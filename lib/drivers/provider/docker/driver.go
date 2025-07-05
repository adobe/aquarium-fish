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
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Factory implements provider.DriverFactory interface
type Factory struct{}

// Name shows name of the driver factory
func (*Factory) Name() string {
	return "docker"
}

// New creates new provider driver
func (f *Factory) New() provider.Driver {
	return &Driver{name: f.Name()}
}

func init() {
	provider.FactoryList = append(provider.FactoryList, &Factory{})
}

// Driver implements provider.Driver interface
type Driver struct {
	name string
	cfg  Config
	// Contains the available tasks of the driver
	tasksList []provider.DriverTask

	totalCPU uint32 // In logical threads
	totalRAM uint32 // In RAM GB

	dockerUsageMutex sync.Mutex
	dockerUsage      *typesv2.Resources // Used when the docker is remote

	// We should not execute some operations at the same
	lockOperationMutex sync.Mutex
}

// Name returns name of the driver
func (d *Driver) Name() string {
	return d.name
}

// Name returns name of the gate
func (d *Driver) SetName(name string) {
	d.name = name
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
	stdout, _, err := util.RunAndLog("DOCKER", 5*time.Second, nil, d.cfg.DockerPath,
		"system", "info", "--format", "{{ .NCPU }},{{ .MemTotal }}",
	)
	if err != nil {
		return fmt.Errorf("DOCKER: %s: Unable to get system info to find the available resources: %v", d.name, err)
	}
	cpuMem := strings.Split(strings.TrimSpace(stdout), ",")
	if len(cpuMem) < 2 {
		return fmt.Errorf("DOCKER: %s: Not enough info values in return: %q", d.name, cpuMem)
	}

	parsedCPU, err := strconv.ParseUint(cpuMem[0], 10, 32)
	if err != nil {
		return fmt.Errorf("DOCKER: %s: Unable to parse CPU uint32: %v (%q)", d.name, err, cpuMem[0])
	}
	d.totalCPU = uint32(parsedCPU)

	parsedRAM, err := strconv.ParseUint(cpuMem[1], 10, 64)
	if err != nil {
		return fmt.Errorf("DOCKER: %s: Unable to parse RAM uint64: %v (%q)", d.name, err, cpuMem[1])
	}
	d.totalRAM = uint32(parsedRAM / 1073741824) // Get in GB

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
func (d *Driver) ValidateDefinition(def typesv2.LabelDefinition) error {
	// Check resources
	if err := def.Resources.Validate([]string{"dir", "hfs+", "exfat", "fat32"}, true); err != nil {
		log.Error().Msgf("DOCKER: %s: Resources validation failed: %v", d.name, err)
		return fmt.Errorf("DOCKER: %s: Resources validation failed: %v", d.name, err)
	}

	// Check options
	var opts Options
	return opts.Apply(def.Options)
}

// AvailableCapacity allows Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(nodeUsage typesv2.Resources, req typesv2.LabelDefinition) int64 {
	var outCount int64

	if d.cfg.IsRemote {
		// It's remote so use the driver-calculated usage
		d.dockerUsageMutex.Lock()
		nodeUsage = *d.dockerUsage
		d.dockerUsageMutex.Unlock()
	}

	availCPU, availRAM := d.getAvailResources()

	// Check if the node has the required resources - otherwise we can't run it anyhow
	if req.Resources.Cpu > availCPU {
		log.Debug().Msgf("DOCKER: %s: Not enough CPU: %d > %d", d.name, req.Resources.Cpu, availCPU)
		return 0
	}
	if req.Resources.Ram > availRAM {
		log.Debug().Msgf("DOCKER: %s: Not enough RAM: %d > %d", d.name, req.Resources.Ram, availRAM)
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
func (d *Driver) Allocate(def typesv2.LabelDefinition, metadata map[string]any) (*typesv2.ApplicationResource, error) {
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
	if err := d.ensureNetwork(cNetwork); err != nil {
		return nil, err
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
		log.Error().Msgf("DOCKER: %s: Unable to create the required disks: %v", d.name, err)
		return nil, fmt.Errorf("DOCKER: %s: Unable to create the required disks: %v", d.name, err)
	}

	// Create env file
	envPath, err := d.envCreate(cName, metadata)
	if err != nil {
		log.Error().Msgf("DOCKER: %s: Unable to create the env file: %v", d.name, err)
		return nil, fmt.Errorf("DOCKER: %s: Unable to create the env file: %v", d.name, err)
	}
	// Add env-file to run args
	runArgs = append(runArgs, "--env-file", envPath)
	// Deleting the env file when container is running to keep secrets
	defer os.Remove(envPath)

	// Run the container
	runArgs = append(runArgs, imgNameVersion)
	if _, _, err := util.RunAndLog("DOCKER", 30*time.Second, nil, d.cfg.DockerPath, runArgs...); err != nil {
		log.Error().Msgf("DOCKER: %s: Unable to run container %q: %v", d.name, cName, err)
		return nil, fmt.Errorf("DOCKER: %s: Unable to run container %q: %v", d.name, cName, err)
	}

	if d.cfg.IsRemote {
		// Locked in the beginning of the function
		d.dockerUsage.Add(&def.Resources)
	}

	log.Info().Msgf("DOCKER: %s: Allocate of Container %q completed: %s", d.name, cName, cHwaddr)

	return &typesv2.ApplicationResource{Identifier: cName, HwAddr: cHwaddr}, nil
}

// Status shows status of the resource
func (d *Driver) Status(res typesv2.ApplicationResource) (string, error) {
	if res.Identifier == "" {
		return "", fmt.Errorf("DOCKER: %s: Invalid resource: %v", d.name, res)
	}
	if len(d.getAllocatedContainerID(res.Identifier)) > 0 {
		return provider.StatusAllocated, nil
	}
	return provider.StatusNone, nil
}

// GetTask returns task struct by name
func (d *Driver) GetTask(name, options string) provider.DriverTask {
	// Look for the specified task name
	var t provider.DriverTask
	for _, task := range d.tasksList {
		if task.Name() == name {
			t = task.Clone()
		}
	}

	// Parse options json into task structure
	if len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.Error().Msgf("DOCKER: %s: Unable to apply the task options: %v", d.name, err)
			return nil
		}
	}

	return t
}

// Deallocate the resource
func (d *Driver) Deallocate(res typesv2.ApplicationResource) error {
	if res.Identifier == "" {
		return fmt.Errorf("DOCKER: %s: Invalid resource: %v", d.name, res)
	}
	if d.cfg.IsRemote {
		// It's remote so let's use docker_usage to store modificators properly
		d.dockerUsageMutex.Lock()
		defer d.dockerUsageMutex.Unlock()
	}
	cName := res.Identifier
	cID := d.getAllocatedContainerID(cName)
	if len(cID) == 0 {
		log.Error().Msgf("DOCKER: %s: Unable to find container with identifier: %s", d.name, res.Identifier)
		return fmt.Errorf("DOCKER: %s: Unable to find container with identifier: %s", d.name, res.Identifier)
	}

	// Getting the mounted volumes
	stdout, _, err := util.RunAndLog("DOCKER", 5*time.Second, nil, d.cfg.DockerPath, "inspect",
		"--format", "{{ range .Mounts }}{{ println .Source }}{{ end }}", cID,
	)
	if err != nil {
		log.Error().Msgf("DOCKER: %s: Unable to inspect the container %q: %v", d.name, cName, err)
		return fmt.Errorf("DOCKER: %s: Unable to inspect the container %q: %v", d.name, cName, err)
	}
	cVolumes := strings.Split(strings.TrimSpace(stdout), "\n")

	if d.cfg.IsRemote {
		// Get the container CPU/RAM to subtract from the docker_usage
		res, err := d.getContainersResources([]string{cID})
		if err != nil {
			log.Error().Msgf("DOCKER: %s: Unable to collect the container %q resources: %v", d.name, cName, err)
			return fmt.Errorf("DOCKER: %s: Unable to collect the container %q resources: %v", d.name, cName, err)
		}
		// Locked in the beginning of the function
		d.dockerUsage.Subtract(res)
	}

	// Stop the container
	if _, _, err := util.RunAndLogRetry("DOCKER", 3, 10*time.Second, nil, d.cfg.DockerPath, "stop", cID); err != nil {
		log.Error().Msgf("DOCKER: %s: Unable to stop container %q: %v", d.name, cName, err)
		return fmt.Errorf("DOCKER: %s: Unable to stop container %q: %v", d.name, cName, err)
	}
	// Remove the container
	if _, _, err := util.RunAndLog("DOCKER", 5*time.Second, nil, d.cfg.DockerPath, "rm", cID); err != nil {
		log.Error().Msgf("DOCKER: %s: Unable to remove container %q: %v", d.name, cName, err)
		return fmt.Errorf("DOCKER: %s: Unable to remove container %q: %v", d.name, cName, err)
	}

	// Umount the disk volumes if needed
	mounts, _, err := util.RunAndLog("DOCKER", 3*time.Second, nil, "/sbin/mount")
	if err != nil {
		log.Error().Msgf("DOCKER: %s: Unable to list the mount points for container %q: %v", d.name, cName, err)
		return fmt.Errorf("DOCKER: %s: Unable to list the mount points for container %q: %v", d.name, cName, err)
	}
	for _, volPath := range cVolumes {
		if strings.Contains(mounts, volPath) {
			if _, _, err := util.RunAndLog("DOCKER", 5*time.Second, nil, "/usr/bin/hdiutil", "detach", volPath); err != nil {
				log.Error().Msgf("DOCKER: %s: Unable to detach container %q volume disk %q: %v", d.name, cName, volPath, err)
				return fmt.Errorf("DOCKER: %s: Unable to detach container %q volume disk %q: %v", d.name, cName, volPath, err)
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

	log.Info().Msgf("DOCKER: %s: Deallocate of container %q completed: %s", d.name, res.Identifier, cName)

	return nil
}
