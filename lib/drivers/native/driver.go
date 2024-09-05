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

package native

// Native driver to run the workload on the host of the fish node

import (
	"encoding/json"
	"fmt"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Implements drivers.ResourceDriverFactory interface
type Factory struct{}

func (f *Factory) Name() string {
	return "native"
}

func (f *Factory) NewResourceDriver() drivers.ResourceDriver {
	return &Driver{}
}

func init() {
	drivers.FactoryList = append(drivers.FactoryList, &Factory{})
}

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config
	// Contains the available tasks of the driver
	tasks_list []drivers.ResourceDriverTask

	total_cpu uint // In logical threads
	total_ram uint // In RAM GB
}

// Is used to provide some data to the entry/metadata values which could contain templates
type EnvData struct {
	Disks map[string]string // Map with disk_name = mount_path
}

func (d *Driver) Name() string {
	return "native"
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

	// Collect node resources status
	cpu_stat, err := cpu.Counts(true)
	if err != nil {
		return err
	}
	d.total_cpu = uint(cpu_stat)

	mem_stat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	d.total_ram = uint(mem_stat.Total / 1073741824) // Getting GB from Bytes

	// TODO: Cleanup the image directory in case the images are not good

	return nil
}

func (d *Driver) ValidateDefinition(def types.LabelDefinition) error {
	// Check options
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return err
	}
	// Validate image tags are available in the disk names
	for _, img := range opts.Images {
		// Empty name means user home which is always exists
		if img.Tag != "" {
			found := false
			for d_name := range def.Resources.Disks {
				if d_name == img.Tag {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("Unable to find disk with name in the image tag: %q", img.Tag)
			}
		}
	}
	return nil
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage types.Resources, req types.LabelDefinition) int64 {
	var out_count int64

	var opts Options
	if err := opts.Apply(req.Options); err != nil {
		return 0
	}

	// Check if the node has the required resources - otherwise we can't run it anyhow
	avail_cpu, avail_ram := d.getAvailResources()
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
 * Allocate workload environment with the provided images
 *
 * It automatically download the required images, unpack them and runs the workload.
 * Using metadata to pass the env to the entry point of the image.
 */
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.Resource, error) {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, log.Error("Native: Unable to apply options:", err)
	}

	// Create user to execute the workload
	user, homedir, err := userCreate(&d.cfg, opts.Groups)
	if err != nil {
		userDelete(&d.cfg, user)
		return nil, log.Error("Native: Unable to create user:", user, err)
	}
	log.Info("Native: Created user for Application execution:", user)

	// Create and connect volumes to container
	disk_paths, err := d.disksCreate(user, def.Resources.Disks)
	if err != nil {
		disksDelete(&d.cfg, user)
		userDelete(&d.cfg, user)
		return nil, log.Error("Native: Unable to create the required disks:", err)
	}

	// Set default path as homedir
	disk_paths[""] = homedir

	// Loading images and unpack them to home/disks according
	if err := d.loadImages(user, opts.Images, disk_paths); err != nil {
		disksDelete(&d.cfg, user)
		userDelete(&d.cfg, user)
		return nil, log.Error("Native: Unable to load and unpack images:", err)
	}

	// Running workload
	if err := userRun(&d.cfg, &EnvData{Disks: disk_paths}, user, opts.Entry, metadata); err != nil {
		disksDelete(&d.cfg, user)
		userDelete(&d.cfg, user)
		return nil, log.Error("Native: Unable to run the entry workload:", err)
	}

	log.Infof("Native: Started environment for user %q", user)

	return &types.Resource{Identifier: user}, nil
}

func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("Native: Invalid resource: %v", res)
	}
	if isEnvAllocated(res.Identifier) {
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
			log.Error("VMX: Unable to apply the task options:", err)
			return nil
		}
	}

	return t
}

func (d *Driver) Deallocate(res *types.Resource) error {
	if res == nil || res.Identifier == "" {
		return fmt.Errorf("Native: Invalid resource: %v", res)
	}
	if !isEnvAllocated(res.Identifier) {
		return log.Error("Native: Unable to find the environment user:", res.Identifier)
	}

	user := res.Identifier

	// Umounting & delete the user env disks
	err := disksDelete(&d.cfg, user)

	// Umounting & delete the user env disks
	err2 := userDelete(&d.cfg, user)

	log.Info("Docker: Deallocate of user env completed:", user)

	// Processing the errors after the cleanup
	if err != nil {
		return err
	}
	if err2 != nil {
		return err2
	}

	return nil
}
