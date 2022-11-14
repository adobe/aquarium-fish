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
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (d *Driver) getContainersResources(container_ids []string) (drivers.Resources, error) {
	var out drivers.Resources

	// Getting current running containers info - will return "<ncpu>,<mem_bytes>\n..." for each one
	docker_args := []string{"inspect", "--format", "{{ .HostConfig.NanoCpus }},{{ .HostConfig.Memory }}"}
	docker_args = append(docker_args, container_ids...)
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, docker_args...)
	if err != nil {
		return out, fmt.Errorf("DOCKER: Unable to inspect the containers to get used resources: %v", err)
	}

	res_list := strings.Split(strings.TrimSpace(stdout), "\n")
	for _, res := range res_list {
		cpu_mem := strings.Split(res, ",")
		if len(cpu_mem) < 2 {
			return out, fmt.Errorf("DOCKER: Not enough info values in return: %q", res_list)
		}
		res_cpu, err := strconv.ParseUint(cpu_mem[0], 10, 64)
		if err != nil {
			return out, fmt.Errorf("DOCKER: Unable to parse CPU uint: %v (%q)", err, cpu_mem[0])
		}
		res_ram, err := strconv.ParseUint(cpu_mem[1], 10, 64)
		if err != nil {
			return out, fmt.Errorf("DOCKER: Unable to parse RAM uint: %v (%q)", err, cpu_mem[1])
		}
		if res_cpu == 0 || res_ram == 0 {
			return out, fmt.Errorf("DOCKER: The container is non-Fish controlled zero-cpu/ram ones: %q", container_ids)
		}
		out.Cpu += uint(res_cpu / 1000000000) // Originallly in NCPU
		out.Ram += uint(res_ram / 1073741824) // Get in GB
		// TODO: Add disks too here
	}

	return out, nil
}

// In order to recover after restart we need to find the current docker usage
// There is some evristics to find the modifiers like Multitenancy and the others
func (d *Driver) getInitialUsage() (drivers.Resources, error) {
	var out drivers.Resources
	// The driver is configured as remote so collecting the current remote docker usage
	// Listing the existing containers ID's to use in inpect command later
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "ps", "--format", "{{ .ID }}")
	if err != nil {
		return out, fmt.Errorf("DOCKER: Unable to list the running containers: %v", err)
	}

	ids_list := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(ids_list) == 1 && ids_list[0] == "" {
		// It's actually empty so skip it
		return out, nil
	}

	out, err = d.getContainersResources(ids_list)
	if err != nil {
		return out, err
	}

	if out.IsEmpty() || len(ids_list) == 1 {
		// There is no or one container is allocated - so for safety use false for modifiers
		return out, nil
	}

	// Let's try to find the modificators that is used
	if len(ids_list) > 1 {
		// There is more than one container is running so multitenancy is true
		out.Multitenancy = true
	}
	if out.Cpu > d.total_cpu {
		out.CpuOverbook = true
	}
	if out.Ram > d.total_ram {
		out.RamOverbook = true
	}

	return out, nil
}

// Collects the available resource with alteration
func (d *Driver) getAvailResources() (avail_cpu, avail_ram uint) {
	if d.cfg.CpuAlter < 0 {
		avail_cpu = d.total_cpu - uint(-d.cfg.CpuAlter)
	} else {
		avail_cpu = d.total_cpu + uint(d.cfg.CpuAlter)
	}

	if d.cfg.RamAlter < 0 {
		avail_ram = d.total_ram - uint(-d.cfg.RamAlter)
	} else {
		avail_ram = d.total_ram + uint(d.cfg.RamAlter)
	}

	return
}

// Returns the standartized container name
func (d *Driver) getContainerName(hwaddr string) string {
	return fmt.Sprintf("fish-%s", strings.ReplaceAll(hwaddr, ":", ""))
}

// Load images and returns the target image name:version to use by container
func (d *Driver) loadImages(def *Definition) (string, error) {
	// Download the images and unpack them
	var wg sync.WaitGroup
	for name, url := range def.Images {
		archive_name := filepath.Base(url)
		image_unpacked := filepath.Join(d.cfg.ImagesPath, strings.TrimSuffix(archive_name, ".tar.xz"))

		log.Println("DOCKER: Loading the required image:", name, url)

		// Running the background routine to download, unpack and process the image
		// Success will be checked later by existance of the image in local docker registry
		wg.Add(1)
		go func(name, url, unpack_dir string) {
			defer wg.Done()
			if err := util.DownloadUnpackArchive(url, unpack_dir, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				log.Println("DOCKER: ERROR: Unable to download and unpack the image:", name, url, err)
				return
			}
		}(name, url, image_unpacked)
	}

	log.Println("DOCKER: Wait for all the background image processes to be done...")
	wg.Wait()

	// Loading the image layers tar archive into the local docker registry
	// They needed to be processed sequentially because the childs does not
	// contains the parent's layers so parents should be loaded first

	// The def.Images is unsorted map, so need to sort the keys first for proper order of loading
	image_names := make([]string, 0, len(def.Images))
	for name, _ := range def.Images {
		image_names = append(image_names, name)
	}
	sort.Strings(image_names)

	target_out := ""
	var loaded_images []string
	for _, name := range image_names {
		url, _ := def.Images[name]
		archive_name := filepath.Base(url)
		image_unpacked := filepath.Join(d.cfg.ImagesPath, strings.TrimSuffix(archive_name, ".tar.xz"))

		// Getting the image subdir name in the unpacked dir
		subdir := ""
		items, err := ioutil.ReadDir(image_unpacked)
		if err != nil {
			log.Println("DOCKER: ERROR: Unable to read the unpacked directory:", image_unpacked, err)
			return "", fmt.Errorf("DOCKER: The image was unpacked incorrectly, please check log for the errors")
		}
		for _, f := range items {
			if strings.HasPrefix(f.Name(), name) {
				if f.Mode()&os.ModeSymlink != 0 {
					// Potentially it can be a symlink (like used in local tests)
					if _, err := os.Stat(filepath.Join(image_unpacked, f.Name())); err != nil {
						log.Println("DOCKER: WARN: The image symlink is broken:", f.Name(), err)
						continue
					}
				}
				subdir = f.Name()
				break
			}
		}
		if subdir == "" {
			log.Printf("DOCKER: ERROR: Unpacked image '%s' has no subfolder '%s', only %s:\n", image_unpacked, name, items)
			return "", fmt.Errorf("DOCKER: The image was unpacked incorrectly, please check log for the errors")
		}

		// Optimization to check if the image exists and not load it again
		subdir_ver_end := strings.LastIndexByte(subdir, '_')
		if subdir_ver_end > 0 {
			image_found := ""
			// Search the image by image ID prefix and list the image tags
			image_tags, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "image", "inspect",
				fmt.Sprintf("sha256:%s", subdir[subdir_ver_end+1:]),
				"--format", "{{ range .RepoTags }}{{ println . }}{{ end }}",
			)
			if err == nil {
				// The image could contain a number of tags so check them all
				found_images := strings.Split(strings.TrimSpace(image_tags), "\n")
				for _, tag := range found_images {
					if strings.HasSuffix(strings.Replace(tag, ":", "-", 1), subdir) {
						image_found = tag
						loaded_images = append(loaded_images, image_found)

						if def.Image == name {
							target_out = image_found
						}
						break
					}
				}
			}

			if image_found != "" {
				log.Println("DOCKER: The image was found in the local docker registry:", image_found)
				continue
			}
		}

		// Load the docker image
		// sha256 prefix the same
		image_archive := filepath.Join(image_unpacked, subdir, name+".tar")
		stdout, _, err := runAndLog(5*time.Minute, d.cfg.DockerPath, "image", "load", "-q", "-i", image_archive)
		if err != nil {
			log.Println("DOCKER: ERROR: Unable to load the image:", image_archive, err)
			return "", fmt.Errorf("DOCKER: The image was unpacked incorrectly, please check log for the errors")
		}
		for _, line := range strings.Split(stdout, "\n") {
			if !strings.HasPrefix(line, "Loaded image: ") {
				continue
			}
			image_name_version := strings.Split(line, ": ")[1]

			loaded_images = append(loaded_images, image_name_version)

			if def.Image == name {
				target_out = image_name_version
			}
			break
		}
	}

	log.Println("DOCKER: All the images are processed.")

	// Check all the images are in place just by number of them
	if len(def.Images) != len(loaded_images) {
		log.Println("DOCKER: The image processes gone wrong, please check log for the errors")
		return "", fmt.Errorf("DOCKER: The image processes gone wrong, please check log for the errors")
	}

	return target_out, nil
}

// Receives the container ID out of the container name
func (d *Driver) getAllocatedContainerId(c_name string) string {
	// Probably it's better to store the current list in the memory
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "ps", "-a", "-q", "--filter", "name="+c_name)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(stdout)
}

// Ensures the network is available
func (d *Driver) isNetworkExists(name string) bool {
	stdout, stderr, err := runAndLog(5*time.Second, d.cfg.DockerPath, "network", "ls", "-q", "--filter", "name=aquarium-"+name)
	if err != nil {
		log.Println("DOCKER: Unable to list the docker network:", stdout, stderr, err)
		return false
	}

	return len(stdout) > 0
}

// Creates disks directories described by the disks map
func (d *Driver) disksCreate(c_name string, run_args *[]string, disks map[string]drivers.Disk) error {
	// Create disks
	disk_paths := make(map[string]string, len(disks))

	for d_name, disk := range disks {
		disk_path := filepath.Join(d.cfg.WorkspacePath, c_name, "disk-"+d_name)
		if disk.Reuse {
			disk_path = filepath.Join(d.cfg.WorkspacePath, "disk-"+d_name)
		}
		if err := os.MkdirAll(filepath.Dir(disk_path), 0o755); err != nil {
			return err
		}

		// Create disk
		// TODO: support other operating systems & filesystems
		// TODO: Ensure failures doesn't leave the changes behind (like mounted disks or files)

		if disk.Type == "dir" {
			if err := os.MkdirAll(disk_path, 0o777); err != nil {
				return err
			}
			disk_paths[disk_path] = disk.Label
			// TODO: Validate the available disk space for disk.Size
			continue
		}

		// Create virtual disk in order to restrict the disk space
		dmg_path := disk_path + ".dmg"

		label := d_name
		if disk.Label != "" {
			// Label can be used as mount point so cut the path separator out
			label = strings.ReplaceAll(disk.Label, "/", "")
		} else {
			disk.Label = label
		}

		// Do not recreate the disk if it is exists
		if _, err := os.Stat(dmg_path); os.IsNotExist(err) {
			disk_type := ""
			switch disk.Type {
			case "hfs+":
				disk_type = "HFS+"
			case "fat32":
				disk_type = "FAT32"
			default:
				disk_type = "ExFAT"
			}
			args := []string{"create", dmg_path,
				"-fs", disk_type,
				"-layout", "NONE",
				"-volname", label,
				"-size", fmt.Sprintf("%dm", disk.Size*1024),
			}
			if _, _, err := runAndLog(10*time.Minute, "/usr/bin/hdiutil", args...); err != nil {
				log.Println("DOCKER: Unable to create dmg disk", dmg_path, err)
				return err
			}
		}

		mount_point := filepath.Join("/Volumes", fmt.Sprintf("%s-%s", c_name, d_name))

		// Attach & mount disk
		if _, _, err := runAndLog(10*time.Second, "/usr/bin/hdiutil", "attach", dmg_path, "-mountpoint", mount_point); err != nil {
			log.Println("DOCKER: Unable to attach dmg disk", err)
			return err
		}

		// Allow anyone to modify the disk content
		if err := os.Chmod(mount_point, 0o777); err != nil {
			log.Println("DOCKER: Unable to change the disk access rights", err)
			return err
		}

		disk_paths[mount_point] = disk.Label
	}

	if len(disk_paths) == 0 {
		return nil
	}

	// Connect disk files to container via cmd
	for mount_path, mount_point := range disk_paths {
		// If the label is not an absolute path than use mnt dir
		if !strings.HasPrefix(mount_point, "/") {
			mount_point = filepath.Join("/mnt", mount_point)
		}
		*run_args = append(*run_args, "-v", fmt.Sprintf("%s:%s", mount_path, mount_point))
	}

	return nil
}

// Creates the env file for the container out of metadata specification
func (d *Driver) envCreate(c_name string, metadata map[string]interface{}) (string, error) {
	env_file_path := filepath.Join(d.cfg.WorkspacePath, c_name, ".env")
	if err := os.MkdirAll(filepath.Dir(env_file_path), 0o755); err != nil {
		log.Println("DOCKER: Unable to create the container directory", err)
		return "", err
	}
	fd, err := os.OpenFile(env_file_path, os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		log.Println("DOCKER: Unable to create env file", err)
		return "", err
	}
	defer fd.Close()

	// Write env file line by line
	for key, value := range metadata {
		if _, err := fd.Write([]byte(fmt.Sprintf("%s=%s\n", key, value))); err != nil {
			log.Println("DOCKER: Unable to write env file data", err)
			return "", err
		}
	}

	return env_file_path, nil
}

// Runs & logs the executable command
func runAndLog(timeout time.Duration, path string, arg ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	// Running command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, arg...)

	log.Printf("DOCKER: Executing: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	// Check the context error to see if the timeout was executed
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("Docker error: Command timed out")
	} else if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("Docker error: %s", message)
	}

	if len(stdoutString) > 0 {
		log.Printf("DOCKER: stdout: %s", stdoutString)
	}
	if len(stderrString) > 0 {
		log.Printf("DOCKER: stderr: %s", stderrString)
	}

	// Replace these for Windows, we only want to deal with Unix style line endings.
	returnStdout := strings.Replace(stdout.String(), "\r\n", "\n", -1)
	returnStderr := strings.Replace(stderr.String(), "\r\n", "\n", -1)

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
