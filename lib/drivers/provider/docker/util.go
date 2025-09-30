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

package docker

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (d *Driver) getContainersResources(containerIDs []string) (*typesv2.Resources, error) {
	logger := log.WithFunc("docker", "Driver.getContainersResources").With("provider.name", d.name)
	out := &typesv2.Resources{}

	// Getting current running containers info - will return "<ncpu>,<mem_bytes>\n..." for each one
	dockerArgs := []string{"inspect", "--format", "{{ .HostConfig.NanoCpus }},{{ .HostConfig.Memory }}"}
	dockerArgs = append(dockerArgs, containerIDs...)
	stdout, _, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, dockerArgs...)
	if err != nil {
		return out, fmt.Errorf("DOCKER: %s: Unable to inspect the containers to get used resources: %v", d.name, err)
	}

	resList := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, res := range resList {
		cpuMem := strings.Split(res, ",")
		if len(cpuMem) < 2 {
			return out, fmt.Errorf("DOCKER: %s: Not enough info values in return: %q", d.name, resList)
		}
		resCPU, err := strconv.ParseUint(cpuMem[0], 10, 64)
		if err != nil {
			return out, fmt.Errorf("DOCKER: %s: Unable to parse CPU uint: %v (%q)", d.name, err, cpuMem[0])
		}
		resRAM, err := strconv.ParseUint(cpuMem[1], 10, 64)
		if err != nil {
			return out, fmt.Errorf("DOCKER: %s: Unable to parse RAM uint: %v (%q)", d.name, err, cpuMem[1])
		}
		if resCPU == 0 || resRAM == 0 {
			if !d.cfg.IgnoreNonControlled {
				return out, fmt.Errorf("DOCKER: %s: The container is non-Fish controlled zero-cpu/ram ones: %q", d.name, containerIDs)
			}
			logger.Warn("Found non-Fish controlled container", "containers", containerIDs)
		} else {
			out.Cpu += uint32(resCPU / 1000000000) // Converting from NanoCPU
			out.Ram += uint32(resRAM / 1073741824) // Get in GB
			// TODO: Add disks too here
		}
	}

	return out, nil
}

// In order to recover after restart we need to find the current docker usage
// There is some evristics to find the modifiers like Multitenancy and the others
func (d *Driver) getInitialUsage() (*typesv2.Resources, error) {
	var out *typesv2.Resources
	// The driver is configured as remote so collecting the current remote docker usage
	// Listing the existing containers ID's to use in inpect command later
	stdout, _, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, "ps", "--format", "{{ .ID }}")
	if err != nil {
		return out, fmt.Errorf("DOCKER: %s: Unable to list the running containers: %v", d.name, err)
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
	if out.Cpu > d.totalCPU {
		out.CpuOverbook = true
	}
	if out.Ram > d.totalRAM {
		out.RamOverbook = true
	}

	return out, nil
}

// Collects the available resource with alteration
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

// Returns the standardized container name
func (*Driver) getContainerName(hwaddr string) string {
	return fmt.Sprintf("fish-%s", strings.ReplaceAll(hwaddr, ":", ""))
}

// Load images and returns the target image name:version to use by container
func (d *Driver) loadImages(cName string, opts *Options) (string, error) {
	logger := log.WithFunc("docker", "loadImages").With("provider.name", d.name, "cont_name", cName)

	var existingImagesMu sync.Mutex
	var existingImages []string

	// Download the images and unpack them
	var wg sync.WaitGroup
	for _, image := range opts.Images {
		logger.Info("Loading the required image", "image_name", image.Name, "image_version", image.Version, "image_url", image.URL)

		// Running the background routine to download, unpack and process the image
		// Success will be checked later by existence of the image in local docker registry
		wg.Add(1)
		go func(image provider.Image) {
			defer wg.Done()
			imageID, _, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, "image", "ls", "-q",
				fmt.Sprintf("%s:%s", image.Name, image.Version),
			)
			if err != nil {
				logger.Error("Unable to check image in the local registry", "image_name", image.Name, "image_version", image.Version, "err", err)
			}
			imageID = strings.TrimSpace(imageID)
			if imageID != "" {
				logger.Debug("Found image in the local docker registry", "image_name", image.Name, "image_version", image.Version, "image_id", imageID)
				existingImagesMu.Lock()
				existingImages = append(existingImages, image.Name+":"+image.Version)
				existingImagesMu.Unlock()
				return
			}

			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				logger.Error("Unable to download and unpack the image", "image_name", image.Name, "image_url", image.URL, "err", err)
			}
		}(image)
	}

	logger.Debug("Wait for all the background image processes to be done")
	wg.Wait()

	// Check if all the images are already in place - no need to unpack them
	if len(existingImages) == len(opts.Images) {
		// Just return the last image name/version
		lastImg := opts.Images[len(opts.Images)-1]
		return lastImg.Name + ":" + lastImg.Version, nil
	}

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
			logger.Error("Unable to read the unpacked directory", "image_unpacked", imageUnpacked, "err", err)
			return "", fmt.Errorf("DOCKER: %s: The image %q was unpacked incorrectly, please check log for the errors", d.name, image.Name)
		}
		for _, f := range items {
			if strings.HasPrefix(f.Name(), image.Name) {
				if f.Type()&fs.ModeSymlink != 0 {
					// Potentially it can be a symlink (like used in local tests)
					if _, err := os.Stat(filepath.Join(imageUnpacked, f.Name())); err != nil {
						logger.Warn("The image symlink is broken", "symlink", f.Name(), "err", err)
						continue
					}
				}
				subdir = f.Name()
				break
			}
		}
		if subdir == "" {
			logger.Error("Unpacked image has no subfolder", "image_unpacked", imageUnpacked, "image_name", image.Name, "items", items)
			return "", fmt.Errorf("DOCKER: %s: The image %q was unpacked incorrectly, please check log for the errors", d.name, image.Name)
		}

		// Optimization to check if the image exists and not load it again
		subdirVerEnd := strings.LastIndexByte(subdir, '_')
		if subdirVerEnd > 0 {
			imageFound := ""
			// Search the image by image ID prefix and list the image tags
			imageTags, _, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, "image", "inspect",
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
				logger.Debug("The image was found in the local docker registry", "image_found", imageFound)
				continue
			}
		}

		// Load the docker image
		// sha256 prefix the same
		imageArchive := filepath.Join(imageUnpacked, subdir, image.Name+".tar")
		stdout, _, err := util.RunAndLog("docker", 5*time.Minute, nil, d.cfg.DockerPath, "image", "load", "-q", "-i", imageArchive)
		if err != nil {
			logger.Error("Unable to load the image", "image_archive", imageArchive, "err", err)
			return "", fmt.Errorf("DOCKER: %s: The image %q was unpacked incorrectly, please check log for the errors", d.name, image.Name)
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

	logger.Info("All the images are processed")

	// Check all the images are in place just by number of them
	if len(opts.Images) != len(loadedImages) {
		logger.Error("Not all the images are ok, please check log for the errors", "loaded_count", len(loadedImages), "total_count", len(opts.Images))
		return "", fmt.Errorf("DOCKER: %s: Not all the images are ok (%d out of %d), please check log for the errors", d.name, len(loadedImages), len(opts.Images))
	}

	return targetOut, nil
}

// Receives the container ID out of the container name
func (d *Driver) getAllocatedContainerID(cName string) string {
	// Probably it's better to store the current list in the memory
	stdout, _, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, "ps", "-a", "-q", "--filter", "name="+cName)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(stdout)
}

// ensureNetwork makes everything possible to create network
func (d *Driver) ensureNetwork(name string) error {
	d.lockOperationMutex.Lock()
	defer d.lockOperationMutex.Unlock()
	if !d.isNetworkExists(name) {
		netArgs := []string{"network", "create", "-d", "bridge"}
		if name == "hostonly" {
			netArgs = append(netArgs, "--internal")
		}
		netArgs = append(netArgs, "aquarium-"+name)
		if _, _, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, netArgs...); err != nil {
			return err
		}
	}

	return nil
}

// Checks if the network is available
func (d *Driver) isNetworkExists(name string) bool {
	stdout, stderr, err := util.RunAndLog("docker", 5*time.Second, nil, d.cfg.DockerPath, "network", "ls", "-q", "--filter", "name=aquarium-"+name)
	if err != nil {
		logger := log.WithFunc("docker", "isNetworkExists").With("provider.name", d.name)
		logger.Error("Unable to list the docker network", "stdout", stdout, "stderr", stderr, "err", err)
		return false
	}

	return len(stdout) > 0
}

// Creates disks directories described by the disks map
func (d *Driver) disksCreate(cName string, runArgs *[]string, disks map[string]typesv2.ResourcesDisk) error {
	logger := log.WithFunc("docker", "disksCreate").With("provider.name", d.name, "cont_name", cName)

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
		label := disk.Label
		if disk.Label != "" {
			// Label can be used as mount point so cut the path separator out
			disk.Label = strings.ReplaceAll(disk.Label, "/", "")
		} else {
			disk.Label = dName
		}

		var err error
		diskPath, err = d.diskCreateSpec(disk, diskPath)
		if err != nil {
			logger.Error("Failed to create disk", "disk_path", diskPath, "err", err)
			return err
		}

		mountPoint, err := d.diskMountSpec(diskPath, fmt.Sprintf("%s-%s", cName, dName))
		if err != nil {
			logger.Error("Failed to mount disk", "disk_path", diskPath, "mount_point", mountPoint, "err", err)
			return err
		}

		diskPaths[mountPoint] = label
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
	logger := log.WithFunc("docker", "envCreate").With("provider.name", d.name, "cont_name", cName)

	envFilePath := filepath.Join(d.cfg.WorkspacePath, cName, ".env")
	if err := os.MkdirAll(filepath.Dir(envFilePath), 0o755); err != nil {
		logger.Error("Unable to create the container directory", "dir", filepath.Dir(envFilePath), "err", err)
		return "", fmt.Errorf("DOCKER: %s: Unable to create the container directory %q: %v", d.name, filepath.Dir(envFilePath), err)
	}
	fd, err := os.OpenFile(envFilePath, os.O_WRONLY|os.O_CREATE, 0o640)
	if err != nil {
		logger.Error("Unable to create env file", "env_file", envFilePath, "err", err)
		return "", fmt.Errorf("DOCKER: %s: Unable to create env file %q: %v", d.name, envFilePath, err)
	}
	defer fd.Close()

	// Write env file line by line
	for key, value := range metadata {
		data := []byte(fmt.Sprintf("%s=%s\n", key, value))
		if _, err := fd.Write(data); err != nil {
			logger.Error("Unable to write env file data", "env_file", envFilePath, "err", err)
			return "", fmt.Errorf("DOCKER: %s: Unable to write env file data %q: %v", d.name, envFilePath, err)
		}
	}

	return envFilePath, nil
}
