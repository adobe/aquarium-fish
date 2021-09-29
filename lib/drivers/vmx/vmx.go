package vmx

// VMWare VMX (Fusion/Workstation) driver to manage VMs & images

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
 */
func (d *Driver) Allocate(definition string) (string, error) {
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
	if err := d.loadImages(&def, vm_images_dir); err != nil {
		return vm_hwaddr, err
	}

	// Clone VM from the image
	vmx_path := filepath.Join(vm_dir, vm_id+".vmx")
	args := []string{"-T", "fusion", "clone",
		filepath.Join(vm_images_dir, def.Image, def.Image+".vmx"), vmx_path,
		"linked", "-snapshot", "original",
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
		log.Println("VMX: Unable to change cloned VM configuration", vmx_path)
		return vm_hwaddr, err
	}

	// Run
	cmd = exec.Command(d.cfg.VmrunPath, "-T", "fusion", "start", vmx_path, "nogui")
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("VMX: Unable to run VM", vmx_path, err)
		log.Println("VMX: Check the log info:", filepath.Join(filepath.Dir(vmx_path), "vmware.log"))
		return vm_hwaddr, err
	}

	log.Println("VMX: Allocate of VM", vm_hwaddr, vmx_path, "completed")

	return vm_hwaddr, nil
}

func (d *Driver) loadImages(def *Definition, vm_images_dir string) error {
	if err := os.MkdirAll(vm_images_dir, 0o755); err != nil {
		log.Println("VMX: Unable to create the vm images dir:", vm_images_dir)
		return err
	}
	for name, url := range def.Images {
		archive_name := filepath.Base(url)
		image_archive := filepath.Join(d.cfg.WorkspacePath, archive_name)
		image_unpacked := filepath.Join(d.cfg.ImagesPath, strings.TrimSuffix(archive_name, ".tar.xz"))
		image_vmx_path := filepath.Join(image_unpacked, name, name+".vmx")

		log.Println("VMX: Loading the required image:", name, image_vmx_path, url)

		// Check the image is unpacked
		if _, err := os.Stat(image_vmx_path); os.IsNotExist(err) {
			// Download archive to workspace
			if err := util.DownloadUrl(url, image_archive); err != nil {
				log.Println("VMX: Unable to download image:", name, url)
				return err
			}
			defer os.Remove(image_archive)

			// Unpack archive
			if err := os.MkdirAll(image_unpacked, 0o755); err != nil {
				log.Println("VMX: Unable to create the image dir:", name, url)
				return err
			}
			if err := util.Unpack(image_archive, image_unpacked); err != nil {
				log.Println("VMX: Unable to unpack image:", name, url)
				return err
			}

			// Remove the archive
			if err := os.Remove(image_archive); err != nil {
				log.Println("VMX: Unable to remove image archive:", image_archive, err)
			}
		}

		// Unfortunately the linked clones requires full path to the parent,
		// so walk through the image files, link them to the workspace dir
		// and copy the vmsd file with path to the workspace images dir
		root_dir := filepath.Join(image_unpacked, name)
		out_dir := filepath.Join(vm_images_dir, name)
		if err := os.MkdirAll(out_dir, 0o755); err != nil {
			log.Println("VMX: Unable to create the vm image dir:", out_dir)
			return err
		}

		tocopy_list, err := os.ReadDir(root_dir)
		if err != nil {
			log.Println("VMX: Unable to list image directory:", root_dir)
			return err
		}

		for _, entry := range tocopy_list {
			in_path := filepath.Join(root_dir, entry.Name())
			out_path := filepath.Join(out_dir, entry.Name())

			// Check if the file is not the disk file
			if strings.HasSuffix(entry.Name(), ".vmdk") {
				// Just link the disk image to the vm image dir
				if err := os.Symlink(in_path, out_path); err != nil {
					return err
				}
				continue
			}

			// Copy VM file in order to prevent the image modification
			if err := util.FileCopy(in_path, out_path); err != nil {
				log.Println("VMX: Unable to copy image file", in_path, out_path)
				return err
			}

			// Modify the vmsd file to replace token - it's require absolute path
			if strings.HasSuffix(entry.Name(), ".vmsd") {
				if err := util.FileReplaceToken(out_path,
					false, false, false,
					"<REPLACE_PARENT_VM_FULL_PATH>", vm_images_dir,
				); err != nil {
					log.Println("VMX: Unable to replace full path token in vmsd", name)
					return err
				}
			}
		}
	}

	return nil
}

func (d *Driver) getAllocatedVMX(hwaddr string) string {
	// Probably it's better to store the current list in the memory and
	// update on fnotify or something like that...
	cmd := exec.Command(d.cfg.VmrunPath, "-T", "fusion", "list")
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
	for name, disk := range disks {
		disk_path := filepath.Join(filepath.Dir(vmx_path), name)
		if disk.Reuse {
			disk_path = filepath.Join(d.cfg.WorkspacePath, "disk-"+name, name)
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
		cmd := exec.Command("/usr/bin/hdiutil", "create", dmg_path, "-fs", "HFS+",
			"-volname", name,
			"-size", fmt.Sprintf("%dm", disk.Size*1024),
		)
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to create dmg disk", dmg_path, err)
			return err
		}

		// Attach & mount disk
		cmd = exec.Command("/usr/bin/hdiutil", "attach", dmg_path)
		stdout, _, err := runAndLog(cmd)
		if err != nil {
			log.Println("VMX: Unable to attach dmg disk", err)
			return err
		}

		// Get attached disk device
		dev_path := strings.SplitN(stdout, " ", 2)[0]
		mount_point := filepath.Join("/Volumes", name)

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

		// Create linked to device vmdk
		// It's the simpliest way I found to prepare the automount vmdk on macos
		cmd = exec.Command(d.cfg.RawdiskCreatorPath, "create", dev_path, "1", disk_path+"_tmp", "lsilogic")
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to create vmdk disk", err)
			return err
		}

		// Detach disk
		cmd = exec.Command("/usr/bin/hdiutil", "detach", dev_path)
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to detach dmg disk", err)
			return err
		}

		// Move vmdk from disk to image file
		// Format is here: http://sanbarrow.com/vmdk/disktypes.html
		// <access type> <size> <vmdk-type> <path to datachunk> <offset>
		// size, offset - number in amount of sectors
		if err := util.FileReplaceBlock(disk_path+"_tmp.vmdk",
			`createType="partitionedDevice"`, "# The Disk Data Base",
			`createType="monolithicFlat"`,
			"",
			"# Extent description",
			fmt.Sprintf(`RW %d FLAT %q 0`, disk.Size*1024*1024*2, dmg_path),
			"",
			"# The Disk Data Base",
		); err != nil {
			log.Println("VMX: Unable to replace extent description in vmdk file", disk_path+"_tmp.vmdk")
			return err
		}

		// Convert linked vmdk to standalone vmdk
		cmd = exec.Command(d.cfg.VdiskmanagerPath, "-r", disk_path+"_tmp.vmdk", "-t", "0", disk_path+".vmdk")
		if _, _, err := runAndLog(cmd); err != nil {
			log.Println("VMX: Unable to create vmdk disk", err)
			return err
		}

		// Remove temp files
		for _, path := range []string{dmg_path, disk_path + "_tmp.vmdk", disk_path + "_tmp-pt.vmdk"} {
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

	cmd := exec.Command(d.cfg.VmrunPath, "-T", "fusion", "stop", vmx_path)
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("VMX: Unable to deallocate VM:", vmx_path)
		return err
	}

	// Delete VM
	cmd = exec.Command(d.cfg.VmrunPath, "-T", "fusion", "deleteVM", vmx_path)
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
