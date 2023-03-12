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

package test

// Test driver for tests - actually doing nothing and just pretend to be a real driver

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

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
}

func init() {
	drivers.DriversList = append(drivers.DriversList, &Driver{})
}

func (d *Driver) Name() string {
	return "test"
}

func (d *Driver) IsRemote() bool {
	return d.cfg.IsRemote
}

func (d *Driver) Prepare(config []byte, node *types.Node) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	// Fill up the available tasks
	d.tasks_list = append(d.tasks_list, &TaskSnapshot{driver: d})

	return nil
}

func (d *Driver) ValidateDefinition(def types.LabelDefinition) error {
	var opts Options
	return opts.Apply(def.Options)
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage types.Resources, req types.LabelDefinition) int64 {
	var out_count int64

	var opts Options
	if err := opts.Apply(req.Options); err != nil {
		log.Error("TEST: Unable to apply options:", err)
		return -1
	}

	if err := randomFail("AvailableCapacity", opts.FailAvailableCapacity); err != nil {
		log.Error("TEST: RandomFail:", err)
		return -1
	}

	total_cpu := d.cfg.CpuLimit
	total_ram := d.cfg.RamLimit

	if total_cpu == 0 && total_ram == 0 {
		// Resources are unlimited
		return 99999
	}

	// Check if the node has the required resources - otherwise we can't run it anyhow
	if req.Resources.Cpu > total_cpu {
		return 0
	}
	if req.Resources.Ram > total_ram {
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
			total_cpu += d.cfg.CpuOverbook
		}
		if node_usage.RamOverbook && req.Resources.RamOverbook {
			total_ram += d.cfg.RamOverbook
		}
	}

	// Calculate how much of those definitions we could run
	out_count = int64((total_cpu - node_usage.Cpu) / req.Resources.Cpu)
	ram_count := int64((total_ram - node_usage.Ram) / req.Resources.Ram)
	if out_count > ram_count {
		out_count = ram_count
	}
	// TODO: Add disks into equation

	return out_count
}

/**
 * Pretend to Allocate (actually not) the Resource
 */
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.Resource, error) {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, log.Error("TEST: Unable to apply options:", err)
	}

	if err := randomFail("Allocate", opts.FailAllocate); err != nil {
		return nil, log.Error("TEST: RandomFail:", err)
	}

	// Generate random resource id and if exists - regenerate
	res := &types.Resource{}
	res_file := ""
	for {
		res.Identifier = "test-" + crypt.RandString(6)
		res_file = filepath.Join(d.cfg.WorkspacePath, res.Identifier)
		if _, err := os.Stat(res_file); os.IsNotExist(err) {
			break
		}
	}

	// Write identifier file
	fh, err := os.Create(res_file)
	if err != nil {
		return nil, fmt.Errorf("TEST: Unable to open file %q to store identifier: %v", res_file, err)
	}
	defer fh.Close()

	return res, nil
}

func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("TEST: Invalid resource: %v", res)
	}
	if err := randomFail(fmt.Sprintf("Status %s", res.Identifier), d.cfg.FailStatus); err != nil {
		return "", fmt.Errorf("TEST: RandomFail: %v\n", err)
	}

	res_file := filepath.Join(d.cfg.WorkspacePath, res.Identifier)
	if _, err := os.Stat(res_file); !os.IsNotExist(err) {
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
	if t != nil && len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.Error("TEST: Unable to apply the task options:", err)
			return nil
		}
	}

	return t
}

func (d *Driver) Deallocate(res *types.Resource) error {
	if res == nil || res.Identifier == "" {
		return log.Error("TEST: Invalid resource:", res)
	}
	if err := randomFail(fmt.Sprintf("Deallocate %s", res.Identifier), d.cfg.FailDeallocate); err != nil {
		return log.Error("TEST: RandomFail:", err)
	}

	res_file := filepath.Join(d.cfg.WorkspacePath, res.Identifier)
	if _, err := os.Stat(res_file); os.IsNotExist(err) {
		return fmt.Errorf("TEST: Unable to deallocate unavailable resource '%s'", res.Identifier)
	}
	if err := os.Remove(res_file); err != nil {
		return fmt.Errorf("TEST: Unable to deallocate the resource '%s': %v", res.Identifier, err)
	}

	return nil
}

func randomFail(name string, probability uint8) error {
	// Do not fail on 0
	if probability == 0 {
		return nil
	}

	// Certainly fail on 255
	if probability == 255 {
		return fmt.Errorf("TEST: %s failed (%d)", name, probability)
	}

	// Fail on probability 1 - low, 254 - high (but still can not fail)
	if uint8(rand.Intn(254)) < probability {
		return fmt.Errorf("TEST: %s failed (%d)", name, probability)
	}

	return nil
}
