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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
	// Contains the available tasks of the driver
	tasks_list []drivers.ResourceDriverTask

	total_cpu uint // In logical threads
	total_ram uint // In RAM megabytes

	docker_usage_mutex sync.Mutex
	docker_usage       types.Resources // Used when the docker is remote
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "docker"
}

func (d *Driver) IsRemote() bool {
	return d.cfg.IsRemote
}

func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	// Getting info about the docker system - will return "<ncpu>,<mem_bytes>"
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath,
		"system", "info", "--format", "{{ .NCPU }},{{ .MemTotal }}",
	)
	if err != nil {
		return fmt.Errorf("DOCKER: Unable to get system info to find the available resources: %v", err)
	}
	cpu_mem := strings.Split(strings.TrimSpace(stdout), ",")
	if len(cpu_mem) < 2 {
		return fmt.Errorf("DOCKER: Not enough info values in return: %q", cpu_mem)
	}
	parsed_cpu, err := strconv.ParseUint(cpu_mem[0], 10, 64)
	if err != nil {
		return fmt.Errorf("DOCKER: Unable to parse CPU uint: %v (%q)", err, cpu_mem[0])
	}
	d.total_cpu = uint(parsed_cpu / 1000000000) // Originally in NCPU
	parsed_ram, err := strconv.ParseUint(cpu_mem[1], 10, 64)
	if err != nil {
		return fmt.Errorf("DOCKER: Unable to parse RAM uint: %v (%q)", err, cpu_mem[1])
	}
	d.total_ram = uint(parsed_ram / 1073741824) // Get in GB

	// Collect the current state of docker containers for validation (for example not controlled
	// containers) purposes - it will be actively used if docker driver is remote
	d.docker_usage, err = d.getInitialUsage()
	if err != nil {
		return err
	}

	// TODO: Cleanup the image directory in case the images are not good
	return nil
}

func (d *Driver) ValidateDefinition(def types.LabelDefinition) error {
	// Check resources
	if err := def.Resources.Validate([]string{"dir", "hfs+", "exfat", "fat32"}, true); err != nil {
		return fmt.Errorf("DOCKER: Resources validation failed: %s", err)
	}

	// Check options
	var opts Options
	return opts.Apply(def.Options)
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage types.Resources, req types.LabelDefinition) int64 {
	var out_count int64

	if d.cfg.IsRemote {
		// It's remote so use the driver-calculated usage
		d.docker_usage_mutex.Lock()
		node_usage = d.docker_usage
		d.docker_usage_mutex.Unlock()
	}

	avail_cpu, avail_ram := d.getAvailResources()

	// Check if the node has the required resources - otherwise we can't run it anyhow
	if req.Resources.Cpu > avail_cpu {
		return 0
	}
	if req.Resources.Ram > avail_ram {
		return 0
	}
	// TODO: Check disk requirements

	// Since we have the required resources - let's check if tenancy allows us to expand them to
	// run more tenants here
	if node_usage.IsEmpty() {
		// In case we dealing with the first one - we need to set usage modificators, otherwise
		// those values will mess up the next calculations
		node_usage.Multitenancy = req.Resources.Multitenancy
		node_usage.CpuOverbook = req.Resources.CpuOverbook
		node_usage.RamOverbook = req.Resources.RamOverbook
	}
	if node_usage.Multitenancy && req.Resources.Multitenancy {
		// Ok we can run more tenants, let's calculate how much
		if node_usage.CpuOverbook && req.Resources.CpuOverbook {
			avail_cpu += d.cfg.CpuOverbook
		}
		if node_usage.RamOverbook && req.Resources.RamOverbook {
			avail_ram += d.cfg.RamOverbook
		}
	}

	// Calculate how much of those definitions we could run
	out_count = int64((avail_cpu - node_usage.Cpu) / req.Resources.Cpu)
	ram_count := int64((avail_ram - node_usage.Ram) / req.Resources.Ram)
	if out_count > ram_count {
		out_count = ram_count
	}
	// TODO: Add disks into equation

	return out_count
}

/**
 * Allocate container out of the images
 *
 * It automatically download the required images, unpack them and runs the container.
 * Using metadata to create env file and pass it to the container.
 */
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.Resource, error) {
	if d.cfg.IsRemote {
		// It's remote so let's use docker_usage to store modificators properly
		d.docker_usage_mutex.Lock()
		defer d.docker_usage_mutex.Unlock()
	}
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, err
	}

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
			return nil, err
		}
	}

	// Load the images
	img_name_version, err := d.loadImages(&opts)
	if err != nil {
		return nil, err
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
		return nil, log.Error("DOCKER: Unable to create the required disks:", err)
	}

	// Create env file
	env_path, err := d.envCreate(c_name, metadata)
	if err != nil {
		return nil, log.Error("DOCKER: Unable to create the env file:", err)
	}
	// Add env-file to run args
	run_args = append(run_args, "--env-file", env_path)
	// Deleting the env file when container is running to keep secrets
	defer os.Remove(env_path)

	// Run the container
	run_args = append(run_args, img_name_version)
	if _, _, err := runAndLog(30*time.Second, d.cfg.DockerPath, run_args...); err != nil {
		return nil, log.Error("DOCKER: Unable to run container", c_name, err)
	}

	if d.cfg.IsRemote {
		// Locked in the beginning of the function
		d.docker_usage.Add(def.Resources)
	}

	log.Info("DOCKER: Allocate of Container completed:", c_hwaddr, c_name)

	return &types.Resource{Identifier: c_name, HwAddr: c_hwaddr}, nil
}

func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("DOCKER: Invalid resource: %v", res)
	}
	if len(d.getAllocatedContainerId(res.Identifier)) > 0 {
		return drivers.StatusAllocated, nil
	}
	return drivers.StatusNone, nil
}

func (d *Driver) GetTask(name, options string) drivers.ResourceDriverTask {
	// Look for the specified task name
	var t drivers.ResourceDriverTask
	for _, task := range d.tasks_list {
		if task.Name() == name {
			t = task.Clone()
		}
	}

	// Parse options json into task structure
	if len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.Error("DOCKER: Unable to apply the task options:", err)
			return nil
		}
	}

	return t
}

func (d *Driver) Deallocate(res *types.Resource) error {
	if res == nil || res.Identifier == "" {
		return fmt.Errorf("DOCKER: Invalid resource: %v", res)
	}
	if d.cfg.IsRemote {
		// It's remote so let's use docker_usage to store modificators properly
		d.docker_usage_mutex.Lock()
		defer d.docker_usage_mutex.Unlock()
	}
	c_name := d.getContainerName(res.Identifier)
	c_id := d.getAllocatedContainerId(res.Identifier)
	if len(c_id) == 0 {
		return log.Error("DOCKER: Unable to find container with identifier:", res.Identifier)
	}

	// Getting the mounted volumes
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "inspect",
		"--format", "{{ range .Mounts }}{{ println .Source }}{{ end }}", c_id,
	)
	if err != nil {
		return log.Error("DOCKER: Unable to inspect the container:", c_name, err)
	}
	c_volumes := strings.Split(strings.TrimSpace(stdout), "\n")

	if d.cfg.IsRemote {
		// Get the container CPU/RAM to subtract from the docker_usage
		res, err := d.getContainersResources([]string{c_id})
		if err != nil {
			return log.Error("DOCKER: Unable to collect the container resources:", c_name, err)
		}
		// Locked in the beginning of the function
		d.docker_usage.Subtract(res)
	}

	// Stop the container
	if _, _, err := runAndLogRetry(3, 10*time.Second, d.cfg.DockerPath, "stop", c_id); err != nil {
		return log.Error("DOCKER: Unable to stop the container:", c_name, err)
	}
	// Remove the container
	if _, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "rm", c_id); err != nil {
		return log.Error("DOCKER: Unable to remove the container:", c_name, err)
	}

	// Umount the disk volumes if needed
	mounts, _, err := runAndLog(3*time.Second, "/sbin/mount")
	if err != nil {
		return log.Error("DOCKER: Unable to list the mount points:", c_name, err)
	}
	for _, vol_path := range c_volumes {
		if strings.Contains(mounts, vol_path) {
			if _, _, err := runAndLog(5*time.Second, "/usr/bin/hdiutil", "detach", vol_path); err != nil {
				return log.Error("DOCKER: Unable to detach the volume disk:", c_name, vol_path, err)
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

	log.Info("DOCKER: Deallocate of Container completed:", res.Identifier, c_name)

	return nil
}
