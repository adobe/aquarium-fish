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
	"fmt"
	"log"
	"math/rand"
	"sync"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
)

// Implements drivers.ResourceDriver interface
type Driver struct {
	cfg Config

	resources      map[string]int
	resources_lock sync.Mutex
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

func (d *Driver) Prepare(config []byte) error {
	d.resources_lock.Lock()
	defer d.resources_lock.Unlock()

	d.resources = make(map[string]int)

	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}
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

	var req Definition
	if err := req.Apply(definition); err != nil {
		log.Println("TEST: Unable to apply definition:", err)
		return -1
	}

	if err := RandomFail("AvailableCapacity", req.FailAvailableCapacity); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
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
func (d *Driver) Allocate(definition string, metadata map[string]interface{}) (string, string, error) {
	var def Definition
	if err := def.Apply(definition); err != nil {
		log.Println("TEST: Unable to apply definition:", err)
		return "", "", err
	}

	if err := RandomFail("Allocate", def.FailAllocate); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
		return "", "", err
	}

	d.resources_lock.Lock()
	defer d.resources_lock.Unlock()

	// Generate random resource id and if exists - regenerate
	var res_id string
	for {
		res_id = "test-" + crypt.RandString(6)
		if _, ok := d.resources[res_id]; !ok {
			break
		}
	}
	d.resources[res_id] = 0

	return res_id, "", nil
}

func (d *Driver) Status(res_id string) string {
	if err := RandomFail(fmt.Sprintf("Status %s", res_id), d.cfg.FailStatus); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
		return drivers.StatusNone
	}

	d.resources_lock.Lock()
	defer d.resources_lock.Unlock()

	if _, ok := d.resources[res_id]; ok {
		return drivers.StatusAllocated
	}
	return drivers.StatusNone
}

func (d *Driver) Snapshot(res_id string, full bool) (string, error) {
	if err := RandomFail(fmt.Sprintf("Snapshot %s", res_id), d.cfg.FailSnapshot); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
		return "", err
	}

	d.resources_lock.Lock()
	defer d.resources_lock.Unlock()

	if _, ok := d.resources[res_id]; !ok {
		return "", fmt.Errorf("TEST: Unable to snapshot unavailable resource '%s'", res_id)
	}

	return "", nil
}

func (d *Driver) Deallocate(res_id string) error {
	if err := RandomFail(fmt.Sprintf("Deallocate %s", res_id), d.cfg.FailDeallocate); err != nil {
		log.Printf("TEST: RandomFail: %v\n", err)
		return err
	}

	d.resources_lock.Lock()
	defer d.resources_lock.Unlock()

	if _, ok := d.resources[res_id]; !ok {
		return fmt.Errorf("TEST: Unable to deallocate unavailable resource '%s'", res_id)
	}

	delete(d.resources, res_id)

	return nil
}

func RandomFail(name string, probability uint8) error {
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
