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

// Factory implements drivers.ResourceDriverFactory interface
type Factory struct{}

// Name shows name of the driver factory
func (*Factory) Name() string {
	return "docker"
}

// NewResourceDriver creates new resource driver
func (*Factory) NewResourceDriver() drivers.ResourceDriver {
	return &Driver{}
}

func init() {
	drivers.FactoryList = append(drivers.FactoryList, &Factory{})
}

// Driver implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
	// Contains the available tasks of the driver
	tasksList []drivers.ResourceDriverTask

	totalCPU uint // In logical threads
	totalRAM uint // In RAM megabytes

	dockerUsageMutex sync.Mutex
	dockerUsage      types.Resources // Used when the docker is remote
}

// Name returns name of the driver
func (*Driver) Name() string {
	return "docker"
}

// IsRemote needed to detect the out-of-node resources managed by this driver
func (d *Driver) IsRemote() bool {
	return d.cfg.IsRemote
}

// Prepare initializes the driver
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
		return fmt.Errorf("Docker: Unable to get system info to find the available resources: %v", err)
	}
	cpuMem := strings.Split(strings.TrimSpace(stdout), ",")
	if len(cpuMem) < 2 {
		return fmt.Errorf("Docker: Not enough info values in return: %q", cpuMem)
	}
	parsedCPU, err := strconv.ParseUint(cpuMem[0], 10, 64)
	if err != nil {
		return fmt.Errorf("Docker: Unable to parse CPU uint: %v (%q)", err, cpuMem[0])
	}
	d.totalCPU = uint(parsedCPU / 1000000000) // Originally in NCPU
	parsedRAM, err := strconv.ParseUint(cpuMem[1], 10, 64)
	if err != nil {
		return fmt.Errorf("Docker: Unable to parse RAM uint: %v (%q)", err, cpuMem[1])
	}
	d.totalRAM = uint(parsedRAM / 1073741824) // Get in GB

	// Collect the current state of docker containers for validation (for example not controlled
	// containers) purposes - it will be actively used if docker driver is remote
	d.dockerUsage, err = d.getInitialUsage()
	if err != nil {
		return err
	}

	// TODO: Cleanup the image directory in case the images are not good
	return nil
}

// ValidateDefinition checks LabelDefinition is ok
func (*Driver) ValidateDefinition(def types.LabelDefinition) error {
	// Check resources
	if err := def.Resources.Validate([]string{"dir", "hfs+", "exfat", "fat32"}, true); err != nil {
		return log.Error("Docker: Resources validation failed:", err)
	}

	// Check options
	var opts Options
	return opts.Apply(def.Options)
}

// AvailableCapacity allows Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(nodeUsage types.Resources, req types.LabelDefinition) int64 {
	var outCount int64

	if d.cfg.IsRemote {
		// It's remote so use the driver-calculated usage
		d.dockerUsageMutex.Lock()
		nodeUsage = d.dockerUsage
		d.dockerUsageMutex.Unlock()
	}

	availCPU, availRAM := d.getAvailResources()

	// Check if the node has the required resources - otherwise we can't run it anyhow
	if req.Resources.Cpu > availCPU {
		return 0
	}
	if req.Resources.Ram > availRAM {
		return 0
	}
	// TODO: Check disk requirements

	// Since we have the required resources - let's check if tenancy allows us to expand them to
	// run more tenants here
	if nodeUsage.IsEmpty() {
		// In case we dealing with the first one - we need to set usage modificators, otherwise
		// those values will mess up the next calculations
		nodeUsage.Multitenancy = req.Resources.Multitenancy
		nodeUsage.CpuOverbook = req.Resources.CpuOverbook
		nodeUsage.RamOverbook = req.Resources.RamOverbook
	}
	if nodeUsage.Multitenancy && req.Resources.Multitenancy {
		// Ok we can run more tenants, let's calculate how much
		if nodeUsage.CpuOverbook && req.Resources.CpuOverbook {
			availCPU += d.cfg.CPUOverbook
		}
		if nodeUsage.RamOverbook && req.Resources.RamOverbook {
			availRAM += d.cfg.RAMOverbook
		}
	}

	// Calculate how much of those definitions we could run
	outCount = int64((availCPU - nodeUsage.Cpu) / req.Resources.Cpu)
	ramCount := int64((availRAM - nodeUsage.Ram) / req.Resources.Ram)
	if outCount > ramCount {
		outCount = ramCount
	}
	// TODO: Add disks into equation

	return outCount
}

// Allocate container out of the images
//
// It automatically download the required images, unpack them and runs the container.
// Using metadata to create env file and pass it to the container.
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.Resource, error) {
	if d.cfg.IsRemote {
		// It's remote so let's use docker_usage to store modificators properly
		d.dockerUsageMutex.Lock()
		defer d.dockerUsageMutex.Unlock()
	}
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, err
	}

	// Generate unique id from the hw address and required directories
	buf := crypt.RandBytes(6)
	buf[0] = (buf[0] | 2) & 0xfe // Set local bit, ensure unicast address
	cHwaddr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
	cName := d.getContainerName(cHwaddr)

	// Create the docker network
	// TODO: For now hostonly is only works properly (allows access to host
	// services) on Linux. VM-based docker implementations (Mac/Win) could
	// have the separated container `hostonly` which allows only
	// host.docker.internal access, but others to drop and to use it as
	// `--net container:hostonly` in other containers in the future.
	cNetwork := def.Resources.Network
	if cNetwork == "" {
		cNetwork = "hostonly"
	}
	if !d.isNetworkExists(cNetwork) {
		netArgs := []string{"network", "create", "-d", "bridge"}
		if cNetwork == "hostonly" {
			netArgs = append(netArgs, "--internal")
		}
		netArgs = append(netArgs, "aquarium-"+cNetwork)
		if _, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, netArgs...); err != nil {
			return nil, err
		}
	}

	// Load the images
	imgNameVersion, err := d.loadImages(&opts)
	if err != nil {
		return nil, err
	}

	// Set the arguments to run the container
	runArgs := []string{"run", "--detach",
		"--name", cName,
		"--mac-address", cHwaddr,
		"--network", "aquarium-" + cNetwork,
		"--cpus", fmt.Sprintf("%d", def.Resources.Cpu),
		"--memory", fmt.Sprintf("%dg", def.Resources.Ram),
		"--pull", "never",
	}

	// Create and connect volumes to container
	if err := d.disksCreate(cName, &runArgs, def.Resources.Disks); err != nil {
		return nil, log.Error("Docker: Unable to create the required disks:", err)
	}

	// Create env file
	envPath, err := d.envCreate(cName, metadata)
	if err != nil {
		return nil, log.Error("Docker: Unable to create the env file:", err)
	}
	// Add env-file to run args
	runArgs = append(runArgs, "--env-file", envPath)
	// Deleting the env file when container is running to keep secrets
	defer os.Remove(envPath)

	// Run the container
	runArgs = append(runArgs, imgNameVersion)
	if _, _, err := runAndLog(30*time.Second, d.cfg.DockerPath, runArgs...); err != nil {
		return nil, log.Error("Docker: Unable to run container", cName, err)
	}

	if d.cfg.IsRemote {
		// Locked in the beginning of the function
		d.dockerUsage.Add(def.Resources)
	}

	log.Info("Docker: Allocate of Container completed:", cHwaddr, cName)

	return &types.Resource{Identifier: cName, HwAddr: cHwaddr}, nil
}

// Status shows status of the resource
func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("Docker: Invalid resource: %v", res)
	}
	if len(d.getAllocatedContainerID(res.Identifier)) > 0 {
		return drivers.StatusAllocated, nil
	}
	return drivers.StatusNone, nil
}

// GetTask returns task struct by name
func (d *Driver) GetTask(name, options string) drivers.ResourceDriverTask {
	// Look for the specified task name
	var t drivers.ResourceDriverTask
	for _, task := range d.tasksList {
		if task.Name() == name {
			t = task.Clone()
		}
	}

	// Parse options json into task structure
	if len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.Error("Docker: Unable to apply the task options:", err)
			return nil
		}
	}

	return t
}

// Deallocate the resource
func (d *Driver) Deallocate(res *types.Resource) error {
	if res == nil || res.Identifier == "" {
		return fmt.Errorf("Docker: Invalid resource: %v", res)
	}
	if d.cfg.IsRemote {
		// It's remote so let's use docker_usage to store modificators properly
		d.dockerUsageMutex.Lock()
		defer d.dockerUsageMutex.Unlock()
	}
	cName := d.getContainerName(res.Identifier)
	cID := d.getAllocatedContainerID(res.Identifier)
	if len(cID) == 0 {
		return log.Error("Docker: Unable to find container with identifier:", res.Identifier)
	}

	// Getting the mounted volumes
	stdout, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "inspect",
		"--format", "{{ range .Mounts }}{{ println .Source }}{{ end }}", cID,
	)
	if err != nil {
		return log.Error("Docker: Unable to inspect the container:", cName, err)
	}
	cVolumes := strings.Split(strings.TrimSpace(stdout), "\n")

	if d.cfg.IsRemote {
		// Get the container CPU/RAM to subtract from the docker_usage
		res, err := d.getContainersResources([]string{cID})
		if err != nil {
			return log.Error("Docker: Unable to collect the container resources:", cName, err)
		}
		// Locked in the beginning of the function
		d.dockerUsage.Subtract(res)
	}

	// Stop the container
	if _, _, err := runAndLogRetry(3, 10*time.Second, d.cfg.DockerPath, "stop", cID); err != nil {
		return log.Error("Docker: Unable to stop the container:", cName, err)
	}
	// Remove the container
	if _, _, err := runAndLog(5*time.Second, d.cfg.DockerPath, "rm", cID); err != nil {
		return log.Error("Docker: Unable to remove the container:", cName, err)
	}

	// Umount the disk volumes if needed
	mounts, _, err := runAndLog(3*time.Second, "/sbin/mount")
	if err != nil {
		return log.Error("Docker: Unable to list the mount points:", cName, err)
	}
	for _, volPath := range cVolumes {
		if strings.Contains(mounts, volPath) {
			if _, _, err := runAndLog(5*time.Second, "/usr/bin/hdiutil", "detach", volPath); err != nil {
				return log.Error("Docker: Unable to detach the volume disk:", cName, volPath, err)
			}
		}
	}

	// Cleaning the container work directory with non-reuse disks
	cWorkspacePath := filepath.Join(d.cfg.WorkspacePath, cName)
	if _, err := os.Stat(cWorkspacePath); !os.IsNotExist(err) {
		if err := os.RemoveAll(cWorkspacePath); err != nil {
			return err
		}
	}

	log.Info("Docker: Deallocate of Container completed:", res.Identifier, cName)

	return nil
}
