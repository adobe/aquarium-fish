/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package vmx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hpcloud/tail"

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Returns the total resources available for the node after alteration
func (d *Driver) getAvailResources() (availCPU, availRAM uint32) {
	if d.cfg.CPUAlter < 0 {
		availCPU = d.totalCPU - uint32(-d.cfg.CPUAlter)
	} else {
		availCPU = d.totalCPU + uint32(d.cfg.CPUAlter)
	}

	if d.cfg.RAMAlter < 0 {
		availRAM = d.totalRAM - uint32(-d.cfg.RAMAlter)
	} else {
		availRAM = d.totalRAM + uint32(d.cfg.RAMAlter)
	}

	return
}

// Load images and returns the target image path for cloning
func (d *Driver) loadImages(opts *Options, vmImagesDir string) (string, error) {
	if err := os.MkdirAll(vmImagesDir, 0o755); err != nil {
		return "", log.Errorf("VMX: %s: Unable to create the VM images dir %q: %v", d.name, vmImagesDir, err)
	}

	targetPath := ""
	var wg sync.WaitGroup
	for imageIndex, image := range opts.Images {
		log.Infof("VMX: %s: Loading the required image: %s %s: %s", d.name, image.Name, image.Version, image.URL)

		// Running the background routine to download, unpack and process the image
		// Success will be checked later by existence of the copied image in the vm directory
		wg.Add(1)
		go func(image provider.Image, index int) error {
			defer wg.Done()
			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				return log.Errorf("VMX: %s: Unable to download and unpack the image: %s %s: %v", d.name, image.Name, image.URL, err)
			}

			// Getting the image subdir name in the unpacked dir
			subdir := ""
			imageUnpacked := filepath.Join(d.cfg.ImagesPath, image.Name+"-"+image.Version)
			items, err := os.ReadDir(imageUnpacked)
			if err != nil {
				return log.Errorf("VMX: %s: Unable to read the unpacked directory %q: %v", d.name, imageUnpacked, err)
			}
			for _, f := range items {
				if strings.HasPrefix(f.Name(), image.Name) {
					if f.Type()&fs.ModeSymlink != 0 {
						// Potentially it can be a symlink (like used in local tests)
						if _, err := os.Stat(filepath.Join(imageUnpacked, f.Name())); err != nil {
							log.Warnf("VMX: %s: The image symlink %q is broken: %v", d.name, f.Name(), err)
							continue
						}
					}
					subdir = f.Name()
					break
				}
			}
			if subdir == "" {
				return log.Errorf("VMX: %s: Unpacked image '%s' has no subfolder '%s', only: %q", d.name, imageUnpacked, image.Name, items)
			}

			// The VMware clone operation modifies the image snapshots description so
			// we walk through the image files, link them to the workspace dir and copy
			// the files (except for vmdk bins) with path to the workspace images dir
			rootDir := filepath.Join(imageUnpacked, subdir)
			outDir := filepath.Join(vmImagesDir, subdir)
			if index+1 == len(opts.Images) {
				// It's the last image in the list so the target one
				targetPath = filepath.Join(outDir, image.Name+".vmx")
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return log.Errorf("VMX: %s: Unable to create the VM image dir %q: %v", d.name, outDir, err)
			}

			tocopyList, err := os.ReadDir(rootDir)
			if err != nil {
				os.RemoveAll(outDir)
				return log.Errorf("VMX: %s: Unable to list the image directory %q: %v", d.name, rootDir, err)
			}

			for _, entry := range tocopyList {
				inPath := filepath.Join(rootDir, entry.Name())
				outPath := filepath.Join(outDir, entry.Name())

				// Check if the file is the big disk
				if strings.HasSuffix(entry.Name(), ".vmdk") && util.FileStartsWith(inPath, []byte("# Disk DescriptorFile")) != nil {
					// Just link the disk image to the vm image dir - we will not modify it anyway
					if err := os.Symlink(inPath, outPath); err != nil {
						os.RemoveAll(outDir)
						return log.Errorf("VMX: %s: Unable to symlink the image file %q to %q: %v", d.name, inPath, outPath, err)
					}
					continue
				}

				// Copy VM file in order to prevent the image modification
				if err := util.FileCopy(inPath, outPath); err != nil {
					os.RemoveAll(outDir)
					return log.Errorf("VMX: %s: Unable to copy the image file %q to %q: %v", d.name, inPath, outPath, err)
				}
			}
			return nil
		}(image, imageIndex)
	}

	log.Debugf("VMX: %s: Wait for all the background image processes to be done...", d.name)
	wg.Wait()

	log.Infof("VMX: %s: The images are processed.", d.name)

	// Check all the images are in place just by number of them
	vmImages, _ := os.ReadDir(vmImagesDir)
	if len(opts.Images) != len(vmImages) {
		return "", log.Errorf("VMX: %s: The image processes gone wrong, please check log for the errors", d.name)
	}

	return targetPath, nil
}

// Returns true if the VM with provided identifier is allocated
func (d *Driver) isAllocated(vmxPath string) bool {
	// Probably it's better to store the current list in the memory and
	// update on fnotify or something like that...
	stdout, _, err := util.RunAndLog("VMX", 10*time.Second, nil, d.cfg.VmrunPath, "list")
	if err != nil {
		return false
	}

	for _, line := range strings.Split(stdout, "\n") {
		if vmxPath == line {
			return true
		}
	}

	return false
}

// Creates VMDK disks described by the disks map
func (d *Driver) disksCreate(vmxPath string, disks map[string]typesv2.ResourcesDisk) error {
	// Create disk files
	var diskPaths []string
	for dName, disk := range disks {
		diskPath := filepath.Join(filepath.Dir(vmxPath), dName)
		if disk.Reuse {
			diskPath = filepath.Join(d.cfg.WorkspacePath, "disk-"+dName, dName)
			if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
				return err
			}
		}

		relPath, err := filepath.Rel(filepath.Dir(vmxPath), diskPath+".vmdk")
		if err != nil {
			log.Warnf("VMX: %s: Unable to get relative path for disk %q: %v", d.name, diskPath+".vmdk", err)
			diskPaths = append(diskPaths, diskPath)
		} else {
			diskPaths = append(diskPaths, relPath)
		}

		if _, err := os.Stat(diskPath + ".vmdk"); !os.IsNotExist(err) {
			continue
		}

		// Create disk
		// TODO: support other operating systems & filesystems
		// TODO: Ensure failures doesn't leave the changes behind (like mounted disks or files)

		// Create virtual disk
		var diskType string
		switch disk.Type {
		case "hfs+":
			diskType = "HFS+"
		case "fat32":
			diskType = "FAT32"
		case "exfat":
			diskType = "ExFAT"
		default:
			diskType = "raw"
		}

		if diskType == "raw" {
			// Create a simple raw vmdk so it could be used by the image to format & mount properly
			_, _, err := util.RunAndLog("VMX", 10*time.Minute, nil, d.cfg.VdiskmanagerPath, "-c", "-s", fmt.Sprintf("%dGB", disk.Size), "-t", "0", diskPath+".vmdk")
			if err != nil {
				return log.Errorf("VMX: %s: Unable to create %s vmdk disk %q: %v", d.name, diskType, diskPath+".vmdk", err)
			}
		} else {
			label := dName
			if disk.Label != "" {
				label = disk.Label
			}
			dmgPath := diskPath + ".dmg"
			args := []string{"create", dmgPath,
				"-fs", diskType,
				"-layout", "NONE",
				"-volname", label,
				"-size", fmt.Sprintf("%dm", disk.Size*1024),
			}
			if _, _, err := util.RunAndLog("VMX", 10*time.Minute, nil, "/usr/bin/hdiutil", args...); err != nil {
				return log.Errorf("VMX: %s: Unable to create dmg disk %q: %v", d.name, dmgPath, err)
			}

			vmName := strings.TrimSuffix(filepath.Base(vmxPath), ".vmx")
			mountPoint := filepath.Join("/Volumes", fmt.Sprintf("%s-%s", vmName, dName))

			// Attach & mount disk
			stdout, _, err := util.RunAndLog("VMX", 10*time.Second, nil, "/usr/bin/hdiutil", "attach", dmgPath, "-mountpoint", mountPoint)
			if err != nil {
				return log.Errorf("VMX: %s: Unable to attach dmg disk %q to %q: %v", d.name, dmgPath, mountPoint, err)
			}

			// Get attached disk device
			devPath := strings.SplitN(stdout, " ", 2)[0]

			// Allow anyone to modify the disk content
			if err := os.Chmod(mountPoint, 0o777); err != nil {
				return log.Errorf("VMX: %s: Unable to change the volume %q access rights: %v", d.name, mountPoint, err)
			}

			// Umount disk (use diskutil to umount for sure)
			_, _, err = util.RunAndLog("VMX", 10*time.Second, nil, "/usr/sbin/diskutil", "umount", mountPoint)
			if err != nil {
				return log.Errorf("VMX: %s: Unable to umount dmg disk %q: %v", d.name, mountPoint, err)
			}

			// Detach disk
			if _, _, err := util.RunAndLog("VMX", 10*time.Second, nil, "/usr/bin/hdiutil", "detach", devPath); err != nil {
				return log.Errorf("VMX: %s: Unable to detach dmg disk %q: %v", d.name, devPath, err)
			}

			// Create vmdk by using the pregenerated vmdk template

			// The rawdiskcreator have an issue on MacOS if 2 image disks are
			// mounted at the same time, so avoiding to use it by using template:
			// `Unable to create the source raw disk: Resource deadlock avoided`
			// To generate template: vmware-rawdiskCreator create /dev/disk2 1 ./disk_name lsilogic
			vmdkTemplate := strings.Join([]string{
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
				fmt.Sprintf(`RW %d FLAT %q 0`, disk.Size*1024*1024*2, dmgPath),
				``,
				`# The Disk Data Base`,
				`#DDB`,
				``,
				`ddb.adapterType = "lsilogic"`,
				// The ddb here was cut because it's not needed - it will
				// be generated by converting later by vdiskmanager
				`ddb.virtualHWVersion = "14"`,
			}, "\n")

			if err := os.WriteFile(diskPath+"_tmp.vmdk", []byte(vmdkTemplate), 0o640); err != nil { //nolint:gosec // G306
				return log.Errorf("VMX: %s: Unable to place the template vmdk file %q: %v", d.name, diskPath+"_tmp.vmdk", err)
			}

			// Convert linked vmdk to standalone vmdk
			if _, _, err := util.RunAndLog("VMX", 10*time.Minute, nil, d.cfg.VdiskmanagerPath, "-r", diskPath+"_tmp.vmdk", "-t", "0", diskPath+".vmdk"); err != nil {
				return log.Errorf("VMX: %s: Unable to create %s vmdk disk %q: %v", d.name, diskType, diskPath+".vmdk", err)
			}

			// Remove temp files
			for _, path := range []string{dmgPath, diskPath + "_tmp.vmdk"} {
				if err := os.Remove(path); err != nil {
					return log.Errorf("VMX: %s: Unable to remove tmp disk file %q: %v", d.name, path, err)
				}
			}
		}
	}

	if len(diskPaths) == 0 {
		return nil
	}

	// Connect disk files to vmx
	var toReplace []string
	// Use SCSI adapter
	toReplace = append(toReplace,
		"sata1.present =", `sata1.present = "TRUE"`,
	)
	for i, diskPath := range diskPaths {
		prefix := fmt.Sprintf("sata1:%d", i)
		toReplace = append(toReplace,
			prefix+".present =", prefix+`.present = "TRUE"`,
			prefix+".fileName =", fmt.Sprintf("%s.fileName = %q", prefix, diskPath),
		)
	}
	if err := util.FileReplaceToken(vmxPath, true, true, true, toReplace...); err != nil {
		return log.Errorf("VMX: %s: Unable to add disks to the VM configuration %q: %v", d.name, vmxPath, err)
	}

	return nil
}

// Ensures the VM is not stale by monitoring the log
func (d *Driver) logMonitor(vmID, vmxPath string) {
	// Monitor the vmware.log file
	logPath := filepath.Join(filepath.Dir(vmxPath), "vmware.log")
	t, _ := tail.TailFile(logPath, tail.Config{Follow: true, Poll: true})
	log.Debugf("VMX: %s: Start monitoring of log for VM %q: %s", d.name, vmID, logPath)
	defer log.Debugf("VMX: %s: Done monitoring of VM %q log: %s", d.name, vmID, logPath)
	for line := range t.Lines {
		log.Debugf("VMX: %q: VM %q vmware.log: %s", vmID, "vmware.log:", line)
		// Send reset if the VM is switched to 0 status
		if strings.Contains(line.Text, "Tools: Changing running status: 1 => 0") {
			log.Warnf("VMX: %s: Resetting the stale VM %q: %s", d.name, vmID, vmxPath)
			// We should not spend much time here, because we can miss
			// the file delete so running in a separated thread
			go util.RunAndLog("VMX", 10*time.Second, nil, d.cfg.VmrunPath, "reset", vmxPath)
		}
	}
}

// Removes the entire directory for clean up purposes
func (d *Driver) cleanupVM(vmDir string) error {
	if err := os.RemoveAll(vmDir); err != nil {
		log.Warnf("VMX: %s: Unable to clean up the VM directory %q: %v", d.name, vmDir, err)
		return err
	}

	return nil
}
