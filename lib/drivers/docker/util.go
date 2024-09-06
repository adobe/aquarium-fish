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

package docker

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (d *Driver) getContainersResources(containerIds []string) (types.Resources, error) {
	var out types.Resources

	// Getting current running containers info - will return "<ncpu>,<mem_bytes>\n..." for each one
	dockerArgs := []string{"inspect", "--format", "{{ .HostConfig.NanoCpus }},{{ .HostConfig.Memory }}"}
	dockerArgs = append(dockerArgs, containerIds...)
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, dockerArgs...)
	if err != nil {
		return out, fmt.Errorf("Docker: Unable to inspect the containers to get used resources: %v", err)
	}

	resList := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, res := range resList {
		cpuMem := strings.Split(res, ",")
		if len(cpuMem) < 2 {
			return out, fmt.Errorf("Docker: Not enough info values in return: %q", resList)
		}
		resCpu, err := strconv.ParseUint(cpuMem[0], 10, 64)
		if err != nil {
			return out, fmt.Errorf("Docker: Unable to parse CPU uint: %v (%q)", err, cpuMem[0])
		}
		resRam, err := strconv.ParseUint(cpuMem[1], 10, 64)
		if err != nil {
			return out, fmt.Errorf("Docker: Unable to parse RAM uint: %v (%q)", err, cpuMem[1])
		}
		if resCpu == 0 || resRam == 0 {
			return out, fmt.Errorf("Docker: The container is non-Fish controlled zero-cpu/ram ones: %q", containerIds)
		}
		out.Cpu += uint(resCpu / 1000000000) // Originallly in NCPU
		out.Ram += uint(resRam / 1073741824) // Get in GB
		// TODO: Add disks too here
	}

	return out, nil
}

// In order to recover after restart we need to find the current docker usage
// There is some evristics to find the modifiers like Multitenancy and the others
func (d *Driver) getInitialUsage() (types.Resources, error) {
	var out types.Resources
	// The driver is configured as remote so collecting the current remote docker usage
	// Listing the existing containers ID's to use in inpect command later
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "ps", "--format", "{{ .ID }}")
	if err != nil {
		return out, fmt.Errorf("Docker: Unable to list the running containers: %v", err)
	}

	idsList := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(idsList) == 1 && idsList[0] == "" {
		// It's actually empty so skip it
		return out, nil
	}

	out, err = d.getContainersResources(idsList)
	if err != nil {
		return out, err
	}

	if out.IsEmpty() || len(idsList) == 1 {
		// There is no or one container is allocated - so for safety use false for modifiers
		return out, nil
	}

	// Let's try to find the modificators that is used
	if len(idsList) > 1 {
		// There is more than one container is running so multitenancy is true
		out.Multitenancy = true
	}
	if out.Cpu > d.totalCpu {
		out.CpuOverbook = true
	}
	if out.Ram > d.totalRam {
		out.RamOverbook = true
	}

	return out, nil
}

// Collects the available resource with alteration
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

// Returns the standardized container name
func (d *Driver) getContainerName(hwaddr string) string {
	return fmt.Sprintf("fish-%s", strings.ReplaceAll(hwaddr, ":", ""))
}

// Load images and returns the target image name:version to use by container
func (d *Driver) loadImages(opts *Options) (string, error) {
	// Download the images and unpack them
	var wg sync.WaitGroup
	for _, image := range opts.Images {
		log.Info("Docker: Loading the required image:", image.Name, image.Version, image.Url)

		// Running the background routine to download, unpack and process the image
		// Success will be checked later by existence of the image in local docker registry
		wg.Add(1)
		go func(image drivers.Image) {
			defer wg.Done()
			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				log.Error("Docker: Unable to download and unpack the image:", image.Name, image.Url, err)
			}
		}(image)
	}

	log.Debug("Docker: Wait for all the background image processes to be done...")
	wg.Wait()

	// Loading the image layers tar archive into the local docker registry
	// They needed to be processed sequentially because the childs does not
	// contains the parent's layers so parents should be loaded first

	targetOut := ""
	var loadedImages []string
	for imageIndex, image := range opts.Images {
		imageUnpacked := filepath.Join(d.cfg.ImagesPath, image.Name+"-"+image.Version)

		// Getting the image subdir name in the unpacked dir
		subdir := ""
		items, err := os.ReadDir(imageUnpacked)
		if err != nil {
			log.Error("Docker: Unable to read the unpacked directory:", imageUnpacked, err)
			return "", fmt.Errorf("Docker: The image was unpacked incorrectly, please check log for the errors")
		}
		for _, f := range items {
			if strings.HasPrefix(f.Name(), image.Name) {
				if f.Type()&fs.ModeSymlink != 0 {
					// Potentially it can be a symlink (like used in local tests)
					if _, err := os.Stat(filepath.Join(imageUnpacked, f.Name())); err != nil {
						log.Warn("Docker: The image symlink is broken:", f.Name(), err)
						continue
					}
				}
				subdir = f.Name()
				break
			}
		}
		if subdir == "" {
			log.Errorf("Docker: Unpacked image '%s' has no subfolder '%s', only: %q", imageUnpacked, image.Name, items)
			return "", fmt.Errorf("Docker: The image was unpacked incorrectly, please check log for the errors")
		}

		// Optimization to check if the image exists and not load it again
		subdirVerEnd := strings.LastIndexByte(subdir, '_')
		if subdirVerEnd > 0 {
			imageFound := ""
			// Search the image by image ID prefix and list the image tags
			imageTags, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "image", "inspect",
				fmt.Sprintf("sha256:%s", subdir[subdirVerEnd+1:]),
				"--format", "{{ range .RepoTags }}{{ println . }}{{ end }}",
			)
			if err == nil {
				// The image could contain a number of tags so check them all
				foundImages := strings.Split(strings.TrimSpace(imageTags), "\n")
				for _, tag := range foundImages {
					if strings.HasSuffix(strings.Replace(tag, ":", "-", 1), subdir) {
						imageFound = tag
						loadedImages = append(loadedImages, imageFound)

						// If it's the last image then it's the target one
						if imageIndex+1 == len(opts.Images) {
							targetOut = imageFound
						}
						break
					}
				}
			}

			if imageFound != "" {
				log.Debug("Docker: The image was found in the local docker registry:", imageFound)
				continue
			}
		}

		// Load the docker image
		// sha256 prefix the same
		imageArchive := filepath.Join(imageUnpacked, subdir, image.Name+".tar")
		stdout, _, err := runAndLog(5*time.Minute, d.cfg.DockerPath, "image", "load", "-q", "-i", imageArchive)
		if err != nil {
			log.Error("Docker: Unable to load the image:", imageArchive, err)
			return "", fmt.Errorf("Docker: The image was unpacked incorrectly, please check log for the errors")
		}
		for _, line := range strings.Split(stdout, "\n") {
			if !strings.HasPrefix(line, "Loaded image: ") {
				continue
			}
			imageNameVersion := strings.Split(line, ": ")[1]

			loadedImages = append(loadedImages, imageNameVersion)

			// If it's the last image then it's the target one
			if imageIndex+1 == len(opts.Images) {
				targetOut = imageNameVersion
			}
			break
		}
	}

	log.Info("Docker: All the images are processed.")

	// Check all the images are in place just by number of them
	if len(opts.Images) != len(loadedImages) {
		return "", log.Errorf("Docker: Not all the images are ok (%d out of %d), please check log for the errors", len(loadedImages), len(opts.Images))
	}

	return targetOut, nil
}

// Receives the container ID out of the container name
func (d *Driver) getAllocatedContainerId(cName string) string {
	// Probably it's better to store the current list in the memory
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "ps", "-a", "-q", "--filter", "name="+cName)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(stdout)
}

// Ensures the network is available
func (d *Driver) isNetworkExists(name string) bool {
	stdout, stderr, err := runAndLog(5*time.Second, d.cfg.DockerPath, "network", "ls", "-q", "--filter", "name=aquarium-"+name)
	if err != nil {
		log.Error("Docker: Unable to list the docker network:", stdout, stderr, err)
		return false
	}

	return len(stdout) > 0
}

// Creates disks directories described by the disks map
func (d *Driver) disksCreate(cName string, runArgs *[]string, disks map[string]types.ResourcesDisk) error {
	// Create disks
	diskPaths := make(map[string]string, len(disks))

	for dName, disk := range disks {
		diskPath := filepath.Join(d.cfg.WorkspacePath, cName, "disk-"+dName)
		if disk.Reuse {
			diskPath = filepath.Join(d.cfg.WorkspacePath, "disk-"+dName)
		}
		if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
			return err
		}

		// Create disk
		// TODO: support other operating systems & filesystems
		// TODO: Ensure failures doesn't leave the changes behind (like mounted disks or files)

		if disk.Type == "dir" {
			if err := os.MkdirAll(diskPath, 0o777); err != nil {
				return err
			}
			diskPaths[diskPath] = disk.Label
			// TODO: Validate the available disk space for disk.Size
			continue
		}

		// Create virtual disk in order to restrict the disk space
		dmgPath := diskPath + ".dmg"

		label := dName
		if disk.Label != "" {
			// Label can be used as mount point so cut the path separator out
			label = strings.ReplaceAll(disk.Label, "/", "")
		} else {
			disk.Label = label
		}

		// Do not recreate the disk if it is exists
		if _, err := os.Stat(dmgPath); os.IsNotExist(err) {
			var diskType string
			switch disk.Type {
			case "hfs+":
				diskType = "HFS+"
			case "fat32":
				diskType = "FAT32"
			default:
				diskType = "ExFAT"
			}
			args := []string{"create", dmgPath,
				"-fs", diskType,
				"-layout", "NONE",
				"-volname", label,
				"-size", fmt.Sprintf("%dm", disk.Size*1024),
			}
			if _, _, err := runAndLog(10*time.Minute, "/usr/bin/hdiutil", args...); err != nil {
				return log.Error("Docker: Unable to create dmg disk:", dmgPath, err)
			}
		}

		mountPoint := filepath.Join("/Volumes", fmt.Sprintf("%s-%s", cName, dName))

		// Attach & mount disk
		if _, _, err := runAndLog(10*time.Second, "/usr/bin/hdiutil", "attach", dmgPath, "-mountpoint", mountPoint); err != nil {
			return log.Error("Docker: Unable to attach dmg disk:", dmgPath, mountPoint, err)
		}

		// Allow anyone to modify the disk content
		if err := os.Chmod(mountPoint, 0o777); err != nil {
			return log.Error("Docker: Unable to change the disk access rights:", mountPoint, err)
		}

		diskPaths[mountPoint] = disk.Label
	}

	if len(diskPaths) == 0 {
		return nil
	}

	// Connect disk files to container via cmd
	for mountPath, mountPoint := range diskPaths {
		// If the label is not an absolute path than use mnt dir
		if !strings.HasPrefix(mountPoint, "/") {
			mountPoint = filepath.Join("/mnt", mountPoint)
		}
		*runArgs = append(*runArgs, "-v", fmt.Sprintf("%s:%s", mountPath, mountPoint))
	}

	return nil
}

// Creates the env file for the container out of metadata specification
func (d *Driver) envCreate(cName string, metadata map[string]any) (string, error) {
	envFilePath := filepath.Join(d.cfg.WorkspacePath, cName, ".env")
	if err := os.MkdirAll(filepath.Dir(envFilePath), 0o755); err != nil {
		return "", log.Error("Docker: Unable to create the container directory:", filepath.Dir(envFilePath), err)
	}
	fd, err := os.OpenFile(envFilePath, os.O_WRONLY|os.O_CREATE, 0o640)
	if err != nil {
		return "", log.Error("Docker: Unable to create env file:", envFilePath, err)
	}
	defer fd.Close()

	// Write env file line by line
	for key, value := range metadata {
		data := []byte(fmt.Sprintf("%s=%s\n", key, value))
		if _, err := fd.Write(data); err != nil {
			return "", log.Error("Docker: Unable to write env file data:", envFilePath, err)
		}
	}

	return envFilePath, nil
}

// Runs & logs the executable command
func runAndLog(timeout time.Duration, path string, arg ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	// Running command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, arg...)

	log.Debug("Docker: Executing:", cmd.Path, strings.Join(cmd.Args[1:], " "))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	// Check the context error to see if the timeout was executed
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("Docker: Command timed out")
	} else if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("Docker: Command exited with error: %v: %s", err, message)
	}

	if len(stdoutString) > 0 {
		log.Debug("Docker: stdout:", stdoutString)
	}
	if len(stderrString) > 0 {
		log.Debug("Docker: stderr:", stderrString)
	}

	// Replace these for Windows, we only want to deal with Unix style line endings.
	returnStdout := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	returnStderr := strings.ReplaceAll(stderr.String(), "\r\n", "\n")

	return returnStdout, returnStderr, err
}

// Will retry on error and store the retry output and errors to return
func runAndLogRetry(retry int, timeout time.Duration, path string, arg ...string) (stdout string, stderr string, err error) {
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
