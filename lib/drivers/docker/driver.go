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
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
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

func (d *Driver) IsRemote() bool {
	return false
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

func (d *Driver) DefinitionResources(definition string) drivers.Resources {
	var def Definition
	def.Apply(definition)

	return def.Resources
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage drivers.Resources, definition string) int64 {
	var out_count int64

	var def Definition
	if err := def.Apply(definition); err != nil {
		log.Println("AWS: Unable to apply definition:", err)
		return -1
	}

	return out_count
}

/**
 * Allocate container out of the images
 *
 * It automatically download the required images, unpack them and runs the container.
 * Using metadata to create env file and pass it to the container.
 */
func (d *Driver) Allocate(definition string, metadata map[string]interface{}) (string, string, error) {
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
	c_network := def.Resources.Network
	if c_network == "" {
		c_network = "hostonly"
	}
	if !d.isNetworkExists(c_network) {
		net_args := []string{"network", "create", "-d", "bridge"}
		if c_network == "hostonly" {
			net_args = append(net_args, "--internal")
		}
		net_args = append(net_args, "aquarium-"+c_network)
		if _, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, net_args...); err != nil {
			return "", "", err
		}
	}

	// Load the images
	img_name_version, err := d.loadImages(&def)
	if err != nil {
		return "", "", err
	}

	// Set the arguments to run the container
	run_args := []string{"run", "--detach",
		"--name", c_name,
		"--mac-address", c_hwaddr,
		"--network", "aquarium-" + c_network,
		"--cpus", fmt.Sprintf("%d", def.Resources.Cpu),
		"--memory", fmt.Sprintf("%dg", def.Resources.Ram),
		"--pull", "never",
	}

	// Create and connect volumes to container
	if err := d.disksCreate(c_name, &run_args, def.Resources.Disks); err != nil {
		log.Println("DOCKER: Unable to create the required disks")
		return c_hwaddr, "", err
	}

	// Create env file
	env_path, err := d.envCreate(c_name, metadata)
	if err != nil {
		log.Println("DOCKER: Unable to create the required disks")
		return c_hwaddr, "", err
	}
	// Add env-file to run args
	run_args = append(run_args, "--env-file", env_path)
	// Deleting the env file when container is running to keep secrets
	defer os.Remove(env_path)

	// Run the container
	run_args = append(run_args, img_name_version)
	if _, _, err := runAndLog(30*time.Second, d.cfg.DockerPath, run_args...); err != nil {
		log.Println("DOCKER: Unable to run container", c_name, err)
		return c_hwaddr, "", err
	}

	log.Println("DOCKER: Allocate of VM", c_hwaddr, c_name, "completed")

	return c_hwaddr, "", nil
}

func (d *Driver) Status(hwaddr string) string {
	if len(d.getAllocatedContainer(hwaddr)) > 0 {
		return drivers.StatusAllocated
	}
	return drivers.StatusNone
}

func (d *Driver) Snapshot(hwaddr string, full bool) (string, error) {
	return "", fmt.Errorf("DOCKER: Snapshot not implemented")
}

func (d *Driver) Deallocate(hwaddr string) error {
	c_name := d.getContainerName(hwaddr)
	c_id := d.getAllocatedContainer(hwaddr)
	if len(c_id) == 0 {
		log.Println("DOCKER: Unable to find container with HW ADDR:", hwaddr)
		return fmt.Errorf("DOCKER: No container found with HW ADDR: %s", hwaddr)
	}

	// Getting the mounted volumes
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "inspect",
		"--format", "{{ range .Mounts }}{{ printf \"%s\\n\" .Source }}{{ end }}", c_id,
	)
	if err != nil {
		log.Println("DOCKER: Unable to inspect the container:", c_name)
		return err
	}
	c_volumes := strings.Split(strings.TrimSpace(stdout), "\n")

	// Stop the container
	if _, _, err := runAndLogRetry(3, 10*time.Second, d.cfg.DockerPath, "stop", c_id); err != nil {
		log.Println("DOCKER: Unable to stop the container:", c_name)
		return err
	}
	// Remove the container
	if _, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "rm", c_id); err != nil {
		log.Println("DOCKER: Unable to remove the container:", c_name)
		return err
	}

	// Umount the disk volumes if needed
	mounts, _, err := runAndLog(3*time.Second, "/sbin/mount")
	if err != nil {
		log.Println("DOCKER: Unable to list the mount points:", c_name)
		return err
	}
	for _, vol_path := range c_volumes {
		if strings.Contains(mounts, vol_path) {
			if _, _, err := runAndLog(5*time.Second, "/usr/bin/hdiutil", "detach", vol_path); err != nil {
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
