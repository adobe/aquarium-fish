/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package vmx

// VMWare VMX (Fusion/Workstation) driver to manage VMs & images

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hpcloud/tail"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "vmx"
}

func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}
	// TODO: Cleanup the image directory in case the images are not good
	return nil
}

func (d *Driver) ValidateDefinition(definition string) error {
	var def Definition
	return def.Apply(definition)
}

/**
 * Allocate VM with provided images
 *
 * It automatically download the required images, unpack them and runs the VM.
 * Not using metadata because there is no good interfaces to pass it to VM.
 */
func (d *Driver) Allocate(definition string, metadata map[string]interface{}) (string, error) {
	var def Definition
	def.Apply(definition)

	// Generate unique id from the hw address and required directories
	buf := crypt.RandBytes(6)
	buf[0] = (buf[0] | 2) & 0xfe // Set local bit, ensure unicast address
	vm_id := fmt.Sprintf("%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
	vm_hwaddr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

	vm_network := def.Requirements.Network
	if vm_network == "" {
		vm_network = "hostonly"
	}

	vm_dir := filepath.Join(d.cfg.WorkspacePath, vm_id)
	vm_images_dir := filepath.Join(vm_dir, "images")

	// Load the required images
	img_path, err := d.loadImages(&def, vm_images_dir)
	if err != nil {
		return vm_hwaddr, err
	}

	// Clone VM from the image
	vmx_path := filepath.Join(vm_dir, vm_id+".vmx")
	args := []string{"-T", "fusion", "clone",
		img_path, vmx_path,
		"linked", "-snapshot", "original",
		"-cloneName", vm_id,
	}
	cmd := exec.Command(d.cfg.VmrunPath, args...)

	if _, _, err := runAndLog(cmd); err != nil {
		return vm_hwaddr, err
	}

	// Change cloned vm configuration
	if err := util.FileReplaceToken(vmx_path,
		true, true, true,
		"ethernet0.addressType =", `ethernet0.addressType = "static"`,
		"ethernet0.address =", fmt.Sprintf("ethernet0.address = %q", vm_hwaddr),
		"ethernet0.connectiontype =", fmt.Sprintf("ethernet0.connectiontype = %q", vm_network),
		"numvcpus =", fmt.Sprintf(`numvcpus = "%d"`, def.Requirements.Cpu),
		"cpuid.corespersocket =", fmt.Sprintf(`cpuid.corespersocket = "%d"`, def.Requirements.Cpu),
		"memsize =", fmt.Sprintf(`memsize = "%d"`, def.Requirements.Ram*1024),
	); err != nil {
		log.Println("VMX: Unable to change cloned VM configuration", vmx_path)
		return vm_hwaddr, err
	}

	// Create and connect disks to vmx
	if err := d.disksCreate(vmx_path, def.Requirements.Disks); err != nil {
		log.Println("VMX: Unable create disks for VM", vmx_path)
		return vm_hwaddr, err
	}

	// Run the background monitoring of the vmware log
	if d.cfg.LogMonitor {
		go d.logMonitor(vmx_path)
	}

	// Run
	cmd = exec.Command(d.cfg.VmrunPath, "start", vmx_path, "nogui")
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("VMX: Unable to run VM", vmx_path, err)
		log.Println("VMX: Check the log info:", filepath.Join(filepath.Dir(vmx_path), "vmware.log"),
			"and directory ~/Library/Logs/VMware/ for additional logs")
		return vm_hwaddr, err
	}

	log.Println("VMX: Allocate of VM", vm_hwaddr, vmx_path, "completed")

	return vm_hwaddr, nil
}

// Load images and returns the target image path for cloning
func (d *Driver) loadImages(def *Definition, vm_images_dir string) (string, error) {
	if err := os.MkdirAll(vm_images_dir, 0o755); err != nil {
		log.Println("VMX: Unable to create the vm images dir:", vm_images_dir)
		return "", err
	}

	target_path := ""
	var wg sync.WaitGroup
	for name, url := range def.Images {
		archive_name := filepath.Base(url)
		image_unpacked := filepath.Join(d.cfg.ImagesPath, strings.TrimSuffix(archive_name, ".tar.xz"))

		log.Println("VMX: Loading the required image:", name, url)

		// Running the background routine to download, unpack and process the image
		// Success will be checked later by existance of the copied image in the vm directory
		wg.Add(1)
		go func(name, url, unpack_dir, target_image string) {
			defer wg.Done()
			if err := util.DownloadUnpackArchive(url, unpack_dir, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				log.Println("VMX: ERROR: Unable to download and unpack the image:", name, url, err)
				return
			}

			// Getting the image subdir name in the unpacked dir
			subdir := ""
			items, err := ioutil.ReadDir(image_unpacked)
			if err != nil {
				log.Println("VMX: ERROR: Unable to read the unpacked directory:", image_unpacked, err)
				return
			}
			for _, f := range items {
				if strings.HasPrefix(f.Name(), name) {
					if f.Mode()&os.ModeSymlink != 0 {
						// Potentially it can be a symlink (like used in local tests)
						if _, err := os.Stat(filepath.Join(image_unpacked, f.Name())); err != nil {
							log.Println("VMX: WARN: The image symlink is broken:", f.Name(), err)
							continue
						}
					}
					subdir = f.Name()
					break
				}
			}
			if subdir == "" {
				log.Printf("VMX: ERROR: Unpacked image '%s' has no subfolder '%s', only %s:\n", image_unpacked, name, items)
				return
			}

			// Unfortunately the clone operation modifies the image snapshots description
			// so we walk through the image files, link them to the workspace dir and copy
			// the files (except for vmdk bins) with path to the workspace images dir
			root_dir := filepath.Join(image_unpacked, subdir)
			out_dir := filepath.Join(vm_images_dir, subdir)
			if target_image == name {
				target_path = filepath.Join(out_dir, name+".vmx")
			}
			if err := os.MkdirAll(out_dir, 0o755); err != nil {
				log.Println("VMX: ERROR: Unable to create the vm image dir:", out_dir, err)
				return
			}

			tocopy_list, err := os.ReadDir(root_dir)
			if err != nil {
				log.Println("VMX: ERROR: Unable to list the image directory:", root_dir, err)
				os.RemoveAll(out_dir)
				return
			}

			for _, entry := range tocopy_list {
				in_path := filepath.Join(root_dir, entry.Name())
				out_path := filepath.Join(out_dir, entry.Name())

				// Check if the file is the big disk
				if strings.HasSuffix(entry.Name(), ".vmdk") && util.FileStartsWith(in_path, []byte("# Disk DescriptorFile")) != nil {
					// Just link the disk image to the vm image dir - we will not modify it anyway
					if err := os.Symlink(in_path, out_path); err != nil {
						log.Println("VMX: ERROR: Unable to symlink the image file", in_path, out_path, err)
						os.RemoveAll(out_dir)
						return
					}
					continue
				}

				// Copy VM file in order to prevent the image modification
				if err := util.FileCopy(in_path, out_path); err != nil {
					log.Println("VMX: ERROR: Unable to copy the image file", in_path, out_path, err)
					os.RemoveAll(out_dir)
					return
				}

				// Deprecated functionality
				// Since aquarium-bait tag `20220118` the images are using only relative paths
				// TODO: Remove it on release v1.0
				//
				// Modify the vmsd file cloneOf0 to replace token - it requires absolute path
				if strings.HasSuffix(entry.Name(), ".vmsd") {
					if err := util.FileReplaceToken(out_path,
						false, false, false,
						"<REPLACE_PARENT_VM_FULL_PATH>", vm_images_dir,
					); err != nil {
						log.Println("VMX: ERROR: Unable to replace full path token in vmsd", name, err)
						os.RemoveAll(out_dir)
						return
					}
				}
			}
		}(name, url, image_unpacked, def.Image)
	}

	log.Println("VMX: Wait for all the background image processes to be done...")
	wg.Wait()

	log.Println("VMX: The images are processed.")

	// Check all the images are in place just by number of them
	vm_images, _ := ioutil.ReadDir(vm_images_dir)
	if len(def.Images) != len(vm_images) {
		log.Println("VMX: The image processes gone wrong, please check log for the errors")
		return "", errors.New("VMX: The image processes gone wrong, please check log for the errors")
	}

	return target_path, nil
}

func (d *Driver) getAllocatedVMX(hwaddr string) string {
	// Probably it's better to store the current list in the memory and
	// update on fnotify or something like that...
	cmd := exec.Command(d.cfg.VmrunPath, "list")
	stdout, _, err := runAndLog(cmd)
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasSuffix(line, ".vmx") {
			continue
		}

		// Read vmx file and filter by ethernet0.address = "${hwaddr}"
		f, err := os.OpenFile(line, os.O_RDONLY, 0o644)
		if err != nil {
			log.Println("VMX: Unable to open .vmx file to check status:", line)
			continue
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			if !strings.HasPrefix(scanner.Text(), "ethernet0.address =") {
				continue
			}
			if !strings.HasSuffix(strings.ToLower(scanner.Text()), `= "`+hwaddr+`"`) {
				break
			}
			return line
		}

		if err := scanner.Err(); err != nil {
			log.Println("VMX: Unable to scan .vmx file for status:", line)
		}
	}
	return ""
}

func (d *Driver) disksCreate(vmx_path string, disks map[string]drivers.Disk) error {
	// Create disk files
	var disk_paths []string
	for d_name, disk := range disks {
		disk_path := filepath.Join(filepath.Dir(vmx_path), d_name)
		if disk.Reuse {
			disk_path = filepath.Join(d.cfg.WorkspacePath, "disk-"+d_name, d_name)
			if err := os.MkdirAll(filepath.Dir(disk_path), 0o755); err != nil {
				return err
			}
		}

		rel_path, err := filepath.Rel(filepath.Dir(vmx_path), disk_path+".vmdk")
		if err == nil {
			disk_paths = append(disk_paths, rel_path)
		} else {
			log.Println("VMX: Unable to get relative path for disk:", disk_path+".vmdk")
			disk_paths = append(disk_paths, disk_path)
		}

		if _, err := os.Stat(disk_path + ".vmdk"); !os.IsNotExist(err) {
			continue
		}

		// Create disk
		// TODO: support other operating systems & filesystems
		// TODO: Ensure failures doesn't leave the changes behind (like mounted disks or files)

		// Create virtual disk
		dmg_path := disk_path + ".dmg"
		disk_type := ""
		switch disk.Type {
		case "hfs+":
			disk_type = "HFS+"
		case "fat32":
			disk_type = "FAT32"
		default:
			disk_type = "ExFAT"
		}
		label := d_name
		if disk.Label != "" {
			label = disk.Label
		}
		cmd := exec.Command("/usr/bin/hdiutil", "create", dmg_path,
			"-fs", disk_type,
			"-layout", "NONE",
			"-volname", label,
			"-size", fmt.Sprintf("%dm", disk.Size*1024),
		)
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to create dmg disk", dmg_path, err)
			return err
		}

		vm_name := strings.TrimSuffix(filepath.Base(vmx_path), ".vmx")
		mount_point := filepath.Join("/Volumes", fmt.Sprintf("%s-%s", vm_name, d_name))

		// Attach & mount disk
		cmd = exec.Command("/usr/bin/hdiutil", "attach", dmg_path, "-mountpoint", mount_point)
		stdout, _, err := runAndLog(cmd)
		if err != nil {
			log.Println("VMX: Unable to attach dmg disk", err)
			return err
		}

		// Get attached disk device
		dev_path := strings.SplitN(stdout, " ", 2)[0]

		// Allow anyone to modify the disk content
		if err := os.Chmod(mount_point, 0o777); err != nil {
			log.Println("VMX: Unable to change the disk access rights", err)
			return err
		}

		// Umount disk (use diskutil to umount for sure)
		cmd = exec.Command("/usr/sbin/diskutil", "umount", mount_point)
		stdout, _, err = runAndLog(cmd)
		if err != nil {
			log.Println("VMX: Unable to umount dmg disk", err)
			return err
		}

		// Detach disk
		cmd = exec.Command("/usr/bin/hdiutil", "detach", dev_path)
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to detach dmg disk", err)
			return err
		}

		// Create vmdk by using the pregenerated vmdk template

		// The rawdiskcreator have an issue on MacOS if 2 image disks are
		// mounted at the same time, so avoiding to use it by using template:
		// `Unable to create the source raw disk: Resource deadlock avoided`
		// To generate template: vmware-rawdiskCreator create /dev/disk2 1 ./disk_name lsilogic
		vmdk_tempalte := strings.Join([]string{
			`# Disk DescriptorFile`,
			`version=1`,
			`encoding="UTF-8"`,
			`CID=fffffffe`,
			`parentCID=ffffffff`,
			`createType="monolithicFlat"`,
			``,
			`# Extent description`,
			// Format: http://sanbarrow.com/vmdk/disktypes.html
			// <access type> <size> <vmdk-type> <path to datachunk> <offset>
			// size, offset - number in amount of sectors
			fmt.Sprintf(`RW %d FLAT %q 0`, disk.Size*1024*1024*2, dmg_path),
			``,
			`# The Disk Data Base`,
			`#DDB`,
			``,
			`ddb.adapterType = "lsilogic"`,
			// The ddb here was cut because it's not needed - it will
			// be generated by converting later by vdiskmanager
			`ddb.virtualHWVersion = "14"`,
		}, "\n")

		if err := os.WriteFile(disk_path+"_tmp.vmdk", []byte(vmdk_tempalte), 0640); err != nil {
			log.Println("VMX: Unable to place the template vmdk file", disk_path+"_tmp.vmdk")
			return err
		}

		// Convert linked vmdk to standalone vmdk
		cmd = exec.Command(d.cfg.VdiskmanagerPath, "-r", disk_path+"_tmp.vmdk", "-t", "0", disk_path+".vmdk")
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to create vmdk disk", err)
			return err
		}

		// Remove temp files
		for _, path := range []string{dmg_path, disk_path + "_tmp.vmdk"} {
			if err := os.Remove(path); err != nil {
				log.Println("VMX: Unable to remove file", path, err)
				return err
			}
		}
	}

	if len(disk_paths) == 0 {
		return nil
	}

	// Connect disk files to vmx
	var to_replace []string
	// Use SCSI adapter
	to_replace = append(to_replace,
		"sata1.present =", `sata1.present = "TRUE"`,
	)
	for i, disk_path := range disk_paths {
		prefix := fmt.Sprintf("sata1:%d", i)
		to_replace = append(to_replace,
			prefix+".present =", prefix+`.present = "TRUE"`,
			prefix+".fileName =", fmt.Sprintf("%s.fileName = %q", prefix, disk_path),
		)
	}
	if err := util.FileReplaceToken(vmx_path, true, true, true, to_replace...); err != nil {
		log.Println("VMX: Unable to add disks to VM configuration", vmx_path)
		return err
	}

	return nil
}

func (d *Driver) logMonitor(vmx_path string) {
	// Monitor the vmware.log file
	log_path := filepath.Join(filepath.Dir(vmx_path), "vmware.log")
	t, _ := tail.TailFile(log_path, tail.Config{Follow: true, Poll: true})
	log.Println("VMX: Start monitoring of log:", log_path)
	for line := range t.Lines {
		// Send reset if the VM is switched to 0 status
		if strings.Contains(line.Text, "Tools: Changing running status: 1 => 0") {
			log.Println("VMX: Resetting the stale VM", vmx_path)
			cmd := exec.Command(d.cfg.VmrunPath, "reset", vmx_path)
			// We should not spend much time here, because we can miss
			// the file delete so running in a separated thread
			go runAndLog(cmd)
		}
	}
	log.Println("VMX: Done monitoring of log:", log_path)
}

func (d *Driver) Status(hwaddr string) string {
	if len(d.getAllocatedVMX(hwaddr)) > 0 {
		return drivers.StatusAllocated
	}
	return drivers.StatusNone
}

func (d *Driver) Deallocate(hwaddr string) error {
	vmx_path := d.getAllocatedVMX(hwaddr)
	if len(vmx_path) == 0 {
		log.Println("VMX: Unable to find VM with HW ADDR:", hwaddr)
		return errors.New(fmt.Sprintf("VMX: No VM found with HW ADDR: %s", hwaddr))
	}

	cmd := exec.Command(d.cfg.VmrunPath, "stop", vmx_path)
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("VMX: Unable to deallocate VM:", vmx_path)
		return err
	}

	// Delete VM
	cmd = exec.Command(d.cfg.VmrunPath, "deleteVM", vmx_path)
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("VMX: Unable to delete VM:", vmx_path)
		return err
	}

	// Cleaning the VM images too
	if err := os.RemoveAll(filepath.Dir(vmx_path)); err != nil {
		return err
	}

	log.Println("VMX: Deallocate of VM", hwaddr, vmx_path, "completed")

	return nil
}

// Directly from packer: github.com/hashicorp/packer
func runAndLog(cmd *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer

	log.Printf("VMX: Executing: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("VMware error: %s", message)
	}

	if len(stdoutString) > 0 {
		log.Printf("VMX: stdout: %s", stdoutString)
	}
	if len(stderrString) > 0 {
		log.Printf("VMX: stderr: %s", stderrString)
	}

	// Replace these for Windows, we only want to deal with Unix style line endings.
	returnStdout := strings.Replace(stdout.String(), "\r\n", "\n", -1)
	returnStderr := strings.Replace(stderr.String(), "\r\n", "\n", -1)

	return returnStdout, returnStderr, err
}
