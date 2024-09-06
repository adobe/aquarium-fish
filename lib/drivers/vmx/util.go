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

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hpcloud/tail"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Returns the total resources available for the node after alteration
func (d *Driver) getAvailResources() (availCpu, availRam uint) {
	if d.cfg.CpuAlter < 0 {
		availCpu = d.totalCpu - uint(-d.cfg.CpuAlter)
	} else {
		availCpu = d.totalCpu + uint(d.cfg.CpuAlter)
	}

	if d.cfg.RamAlter < 0 {
		availRam = d.totalRam - uint(-d.cfg.RamAlter)
	} else {
		availRam = d.totalRam + uint(d.cfg.RamAlter)
	}

	return
}

// Load images and returns the target image path for cloning
func (d *Driver) loadImages(opts *Options, vmImagesDir string) (string, error) {
	if err := os.MkdirAll(vmImagesDir, 0o755); err != nil {
		return "", log.Error("VMX: Unable to create the VM images dir:", vmImagesDir, err)
	}

	targetPath := ""
	var wg sync.WaitGroup
	for imageIndex, image := range opts.Images {
		log.Info("VMX: Loading the required image:", image.Name, image.Version, image.Url)

		// Running the background routine to download, unpack and process the image
		// Success will be checked later by existence of the copied image in the vm directory
		wg.Add(1)
		go func(image drivers.Image, index int) error {
			defer wg.Done()
			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				return log.Error("VMX: Unable to download and unpack the image:", image.Name, image.Url, err)
			}

			// Getting the image subdir name in the unpacked dir
			subdir := ""
			imageUnpacked := filepath.Join(d.cfg.ImagesPath, image.Name+"-"+image.Version)
			items, err := os.ReadDir(imageUnpacked)
			if err != nil {
				return log.Error("VMX: Unable to read the unpacked directory:", imageUnpacked, err)
			}
			for _, f := range items {
				if strings.HasPrefix(f.Name(), image.Name) {
					if f.Type()&fs.ModeSymlink != 0 {
						// Potentially it can be a symlink (like used in local tests)
						if _, err := os.Stat(filepath.Join(imageUnpacked, f.Name())); err != nil {
							log.Warn("VMX: The image symlink is broken:", f.Name(), err)
							continue
						}
					}
					subdir = f.Name()
					break
				}
			}
			if subdir == "" {
				return log.Errorf("VMX: Unpacked image '%s' has no subfolder '%s', only: %q", imageUnpacked, image.Name, items)
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
				return log.Error("VMX: Unable to create the vm image dir:", outDir, err)
			}

			tocopyList, err := os.ReadDir(rootDir)
			if err != nil {
				os.RemoveAll(outDir)
				return log.Error("VMX: Unable to list the image directory:", rootDir, err)
			}

			for _, entry := range tocopyList {
				inPath := filepath.Join(rootDir, entry.Name())
				outPath := filepath.Join(outDir, entry.Name())

				// Check if the file is the big disk
				if strings.HasSuffix(entry.Name(), ".vmdk") && util.FileStartsWith(inPath, []byte("# Disk DescriptorFile")) != nil {
					// Just link the disk image to the vm image dir - we will not modify it anyway
					if err := os.Symlink(inPath, outPath); err != nil {
						os.RemoveAll(outDir)
						return log.Error("VMX: Unable to symlink the image file:", inPath, outPath, err)
					}
					continue
				}

				// Copy VM file in order to prevent the image modification
				if err := util.FileCopy(inPath, outPath); err != nil {
					os.RemoveAll(outDir)
					return log.Error("VMX: Unable to copy the image file:", inPath, outPath, err)
				}

				// Deprecated functionality
				// Since aquarium-bait tag `20220118` the images are using only relative paths
				// TODO: Remove it on release v1.0
				//
				// Modify the vmsd file cloneOf0 to replace token - it requires absolute path
				if strings.HasSuffix(entry.Name(), ".vmsd") {
					if err := util.FileReplaceToken(outPath,
						false, false, false,
						"<REPLACE_PARENT_VM_FULL_PATH>", vmImagesDir,
					); err != nil {
						os.RemoveAll(outDir)
						return log.Error("VMX: Unable to replace full path token in vmsd:", image.Name, err)
					}
				}
			}
			return nil
		}(image, imageIndex)
	}

	log.Debug("VMX: Wait for all the background image processes to be done...")
	wg.Wait()

	log.Info("VMX: The images are processed.")

	// Check all the images are in place just by number of them
	vmImages, _ := os.ReadDir(vmImagesDir)
	if len(opts.Images) != len(vmImages) {
		return "", log.Error("VMX: The image processes gone wrong, please check log for the errors")
	}

	return targetPath, nil
}

// Returns true if the VM with provided identifier is allocated
func (d *Driver) isAllocated(vmxPath string) bool {
	// Probably it's better to store the current list in the memory and
	// update on fnotify or something like that...
	stdout, _, err := runAndLog(10*time.Second, d.cfg.VmrunPath, "list")
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
func (d *Driver) disksCreate(vmxPath string, disks map[string]types.ResourcesDisk) error {
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
			log.Warn("VMX: Unable to get relative path for disk:", diskPath+".vmdk", err)
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
		dmgPath := diskPath + ".dmg"
		var diskType string
		switch disk.Type {
		case "hfs+":
			diskType = "HFS+"
		case "fat32":
			diskType = "FAT32"
		default:
			diskType = "ExFAT"
		}
		label := dName
		if disk.Label != "" {
			label = disk.Label
		}
		args := []string{"create", dmgPath,
			"-fs", diskType,
			"-layout", "NONE",
			"-volname", label,
			"-size", fmt.Sprintf("%dm", disk.Size*1024),
		}
		if _, _, err := runAndLog(10*time.Minute, "/usr/bin/hdiutil", args...); err != nil {
			return log.Error("VMX: Unable to create dmg disk:", dmgPath, err)
		}

		vmName := strings.TrimSuffix(filepath.Base(vmxPath), ".vmx")
		mountPoint := filepath.Join("/Volumes", fmt.Sprintf("%s-%s", vmName, dName))

		// Attach & mount disk
		stdout, _, err := runAndLog(10*time.Second, "/usr/bin/hdiutil", "attach", dmgPath, "-mountpoint", mountPoint)
		if err != nil {
			return log.Error("VMX: Unable to attach dmg disk:", dmgPath, mountPoint, err)
		}

		// Get attached disk device
		devPath := strings.SplitN(stdout, " ", 2)[0]

		// Allow anyone to modify the disk content
		if err := os.Chmod(mountPoint, 0o777); err != nil {
			return log.Error("VMX: Unable to change the disk access rights:", mountPoint, err)
		}

		// Umount disk (use diskutil to umount for sure)
		_, _, err = runAndLog(10*time.Second, "/usr/sbin/diskutil", "umount", mountPoint)
		if err != nil {
			return log.Error("VMX: Unable to umount dmg disk:", mountPoint, err)
		}

		// Detach disk
		if _, _, err := runAndLog(10*time.Second, "/usr/bin/hdiutil", "detach", devPath); err != nil {
			return log.Error("VMX: Unable to detach dmg disk:", devPath, err)
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
			return log.Error("VMX: Unable to place the template vmdk file:", diskPath+"_tmp.vmdk", err)
		}

		// Convert linked vmdk to standalone vmdk
		if _, _, err := runAndLog(10*time.Minute, d.cfg.VdiskmanagerPath, "-r", diskPath+"_tmp.vmdk", "-t", "0", diskPath+".vmdk"); err != nil {
			return log.Error("VMX: Unable to create vmdk disk:", diskPath+".vmdk", err)
		}

		// Remove temp files
		for _, path := range []string{dmgPath, diskPath + "_tmp.vmdk"} {
			if err := os.Remove(path); err != nil {
				return log.Error("VMX: Unable to remove tmp disk files:", path, err)
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
		return log.Error("VMX: Unable to add disks to the VM configuration:", vmxPath, err)
	}

	return nil
}

// Ensures the VM is not stale by monitoring the log
func (d *Driver) logMonitor(vmId, vmxPath string) {
	// Monitor the vmware.log file
	logPath := filepath.Join(filepath.Dir(vmxPath), "vmware.log")
	t, _ := tail.TailFile(logPath, tail.Config{Follow: true, Poll: true})
	log.Debug("VMX: Start monitoring of log:", vmId, logPath)
	for line := range t.Lines {
		log.Debug("VMX:", vmId, "vmware.log:", line)
		// Send reset if the VM is switched to 0 status
		if strings.Contains(line.Text, "Tools: Changing running status: 1 => 0") {
			log.Warn("VMX: Resetting the stale VM", vmId, vmxPath)
			// We should not spend much time here, because we can miss
			// the file delete so running in a separated thread
			go runAndLog(10*time.Second, d.cfg.VmrunPath, "reset", vmxPath)
		}
	}
	log.Debug("VMX: Done monitoring of log:", vmId, logPath)
}

// Removes the entire directory for clean up purposes
func (d *Driver) cleanupVm(vmDir string) error {
	if err := os.RemoveAll(vmDir); err != nil {
		log.Warn("VMX: Unable to clean up the vm directory:", vmDir, err)
		return err
	}

	return nil
}

// Runs & logs the executable command
func runAndLog(timeout time.Duration, path string, arg ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	// Running command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, arg...)

	log.Debug("VMX: Executing:", cmd.Path, strings.Join(cmd.Args[1:], " "))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	// Check the context error to see if the timeout was executed
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("VMX: Command timed out")
	} else if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("VMX: Command exited with error: %v: %s", err, message)
	}

	if len(stdoutString) > 0 {
		log.Debug("VMX: stdout:", stdoutString)
	}
	if len(stderrString) > 0 {
		log.Debug("VMX: stderr:", stderrString)
	}

	// Replace these for Windows, we only want to deal with Unix style line endings.
	returnStdout := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	returnStderr := strings.ReplaceAll(stderr.String(), "\r\n", "\n")

	return returnStdout, returnStderr, err
}

// Will retry on error and store the retry output and errors to return
func runAndLogRetry(retry int, timeout time.Duration, path string, arg ...string) (stdout string, stderr string, err error) { //nolint:unparam
	counter := 0
	for {
		counter++
		rout, rerr, err := runAndLog(timeout, path, arg...)
		if err != nil {
			stdout += fmt.Sprintf("\n--- Fish: Command execution attempt %d ---\n", counter)
			stdout += rout
			stderr += fmt.Sprintf("\n--- Fish: Command execution attempt %d ---\n", counter)
			stderr += rerr
			if counter <= retry {
				// Give command 5 seconds to rest
				time.Sleep(5 * time.Second)
				continue
			}
		}
		return stdout, stderr, err
	}
}
