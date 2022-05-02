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

// Docker driver to manage container & images

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
	return "docker"
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

func (d *Driver) getContainerName(hwaddr string) string {
	return fmt.Sprintf("fish-%s", strings.ReplaceAll(hwaddr, ":", ""))
}

/**
 * Allocate container out of the images
 *
 * It automatically download the required images, unpack them and runs the container.
 * Using metadata to create env file and pass it to the container.
 */
func (d *Driver) Allocate(definition string, metadata map[string]interface{}) (string, error) {
	var def Definition
	def.Apply(definition)

	// Generate unique id from the hw address and required directories
	buf := crypt.RandBytes(6)
	buf[0] = (buf[0] | 2) & 0xfe // Set local bit, ensure unicast address
	c_hwaddr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
	c_name := d.getContainerName(c_hwaddr)

	// Create the docker network
	// TODO: For now hostonly is only works properly (allows access to host
	// services) on Linux. VM-based docker implementations (Mac/Win) could
	// have the separated container `hostonly` which allows only
	// host.docker.internal access, but others to drop and to use it as
	// `--net container:hostonly` in other containers in the future.
	c_network := def.Requirements.Network
	if c_network == "" {
		c_network = "hostonly"
	}
	if !d.isNetworkExists(c_network) {
		net_args := []string{"network", "create", "-d", "bridge"}
		if c_network == "hostonly" {
			net_args = append(net_args, "--internal")
		}
		net_args = append(net_args, "aquarium-"+c_network)
		cmd := exec.Command(d.cfg.DockerPath, net_args...)
		if _, _, err := runAndLog(cmd); err != nil {
			return "", err
		}
	}

	// Load the images
	img_name_version, err := d.loadImages(&def)
	if err != nil {
		return "", err
	}

	// Set the arguments to run the container
	run_args := []string{"run", "--detach",
		"--name", c_name,
		"--mac-address", c_hwaddr,
		"--network", "aquarium-" + c_network,
		"--cpus", fmt.Sprintf("%d", def.Requirements.Cpu),
		"--memory", fmt.Sprintf("%dg", def.Requirements.Ram),
		"--pull", "never",
	}

	// Create and connect volumes to container
	if err := d.disksCreate(c_name, &run_args, def.Requirements.Disks); err != nil {
		log.Println("DOCKER: Unable to create the required disks")
		return c_hwaddr, err
	}

	// Create env file
	env_path, err := d.envCreate(c_name, metadata)
	if err != nil {
		log.Println("DOCKER: Unable to create the required disks")
		return c_hwaddr, err
	}
	// Add env-file to run args
	run_args = append(run_args, "--env-file", env_path)
	// Deleting the env file when container is running to keep secrets
	defer os.Remove(env_path)

	// Run the container
	run_args = append(run_args, img_name_version)
	cmd := exec.Command(d.cfg.DockerPath, run_args...)
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("DOCKER: Unable to run container", c_name, err)
		return c_hwaddr, err
	}

	log.Println("DOCKER: Allocate of VM", c_hwaddr, c_name, "completed")

	return c_hwaddr, nil
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
			return "", errors.New("DOCKER: The image was unpacked incorrectly, please check log for the errors")
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
			return "", errors.New("DOCKER: The image was unpacked incorrectly, please check log for the errors")
		}

		// Optimization to check if the image exists and not load it again
		subdir_ver_end := strings.LastIndexByte(subdir, '_')
		if subdir_ver_end > 0 {
			image_found := ""
			// Search the image by image ID prefix and list the image tags
			image_tags, _, err := runAndLog(exec.Command(d.cfg.DockerPath, "image", "inspect",
				fmt.Sprintf("sha256:%s", subdir[subdir_ver_end+1:]),
				"--format", "{{ range .RepoTags }}{{ printf \"%s\\n\" . }}{{ end }}",
			))
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
		cmd := exec.Command(d.cfg.DockerPath, "image", "load", "-q", "-i", image_archive)
		stdout, _, err := runAndLog(cmd)
		if err != nil {
			log.Println("DOCKER: ERROR: Unable to load the image:", image_archive, err)
			return "", errors.New("DOCKER: The image was unpacked incorrectly, please check log for the errors")
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
		return "", errors.New("DOCKER: The image processes gone wrong, please check log for the errors")
	}

	return target_out, nil
}

func (d *Driver) getAllocatedContainer(hwaddr string) string {
	// Probably it's better to store the current list in the memory
	cmd := exec.Command(d.cfg.DockerPath, "ps", "-a", "-q",
		"--filter", "name="+d.getContainerName(hwaddr),
	)
	stdout, _, err := runAndLog(cmd)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(stdout)
}

func (d *Driver) isNetworkExists(name string) bool {
	cmd := exec.Command(d.cfg.DockerPath, "network", "ls", "-q", "--filter", "name=aquarium-"+name)
	stdout, stderr, err := runAndLog(cmd)
	if err != nil {
		log.Println("DOCKER: Unable to list the docker network:", stdout, stderr, err)
		return false
	}

	return len(stdout) > 0
}

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
			cmd := exec.Command("/usr/bin/hdiutil", "create", dmg_path,
				"-fs", disk_type,
				"-volname", label,
				"-size", fmt.Sprintf("%dm", disk.Size*1024),
			)
			if _, _, err := runAndLog(cmd); err != nil {
				log.Println("DOCKER: Unable to create dmg disk", dmg_path, err)
				return err
			}
		}

		mount_point := filepath.Join("/Volumes", fmt.Sprintf("%s-%s", c_name, d_name))

		// Attach & mount disk
		cmd := exec.Command("/usr/bin/hdiutil", "attach", dmg_path, "-mountpoint", mount_point)
		if _, _, err := runAndLog(cmd); err != nil {
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

func (d *Driver) Status(hwaddr string) string {
	if len(d.getAllocatedContainer(hwaddr)) > 0 {
		return drivers.StatusAllocated
	}
	return drivers.StatusNone
}

func (d *Driver) Deallocate(hwaddr string) error {
	c_name := d.getContainerName(hwaddr)
	c_id := d.getAllocatedContainer(hwaddr)
	if len(c_id) == 0 {
		log.Println("DOCKER: Unable to find container with HW ADDR:", hwaddr)
		return errors.New(fmt.Sprintf("DOCKER: No container found with HW ADDR: %s", hwaddr))
	}

	// Getting the mounted volumes
	cmd := exec.Command(d.cfg.DockerPath, "inspect",
		"--format", "{{ range .Mounts }}{{ printf \"%s\\n\" .Source }}{{ end }}", c_id,
	)
	stdout, _, err := runAndLog(cmd)
	if err != nil {
		log.Println("DOCKER: Unable to inspect the container:", c_name)
		return err
	}
	c_volumes := strings.Split(strings.TrimSpace(stdout), "\n")

	// Stop the container
	cmd = exec.Command(d.cfg.DockerPath, "stop", c_id)
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("DOCKER: Unable to stop the container:", c_name)
		return err
	}
	// Remove the container
	cmd = exec.Command(d.cfg.DockerPath, "rm", c_id)
	if _, _, err := runAndLog(cmd); err != nil {
		log.Println("DOCKER: Unable to remove the container:", c_name)
		return err
	}

	// Umount the disk volumes if needed
	mounts, _, err := runAndLog(exec.Command("/sbin/mount"))
	if err != nil {
		log.Println("DOCKER: Unable to list the mount points:", c_name)
		return err
	}
	for _, vol_path := range c_volumes {
		if strings.Contains(mounts, vol_path) {
			cmd := exec.Command("/usr/bin/hdiutil", "detach", vol_path)
			if _, _, err := runAndLog(cmd); err != nil {
				log.Println("DOCKER: Unable to detach the volume disk", err)
				return err
			}
		}
	}

	// Cleaning the container work directory with non-reuse disks
	c_workspace_path := filepath.Join(d.cfg.WorkspacePath, c_name)
	if _, err := os.Stat(c_workspace_path); !os.IsNotExist(err) {
		if err := os.RemoveAll(c_workspace_path); err != nil {
			return err
		}
	}

	log.Println("DOCKER: Deallocate of container", hwaddr, c_name, "completed")

	return nil
}

// Directly from packer: github.com/hashicorp/packer
func runAndLog(cmd *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer

	log.Printf("DOCKER: Executing: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
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
