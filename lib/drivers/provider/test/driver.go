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

package test

// Test driver for tests - actually doing nothing and just pretend to be a real driver

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// Factory implements provider.DriverFactory interface
type Factory struct{}

// Name shows name of the driver factory
func (*Factory) Name() string {
	return "test"
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

	// Fill up the available tasks
	d.tasksList = append(d.tasksList, &TaskSnapshot{driver: d})

	return nil
}

// ValidateDefinition checks LabelDefinition is ok
func (*Driver) ValidateDefinition(def typesv2.LabelDefinition) error {
	var opts Options
	return opts.Apply(def.Options)
}

// AvailableCapacity allows Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(nodeUsage typesv2.Resources, req typesv2.LabelDefinition) int64 {
	var outCount int64

	var opts Options
	if err := opts.Apply(req.Options); err != nil {
		log.WithFunc("test", "AvailableCapacity").Error("Unable to apply options", "err", err)
		return -1
	}

	if err := randomFail("AvailableCapacity", opts.FailAvailableCapacity); err != nil {
		log.WithFunc("test", "AvailableCapacity").Error("RandomFail", "err", err)
		return -1
	}

	if opts.DelayAvailableCapacity > 0 {
		time.Sleep(time.Duration(math.Floor(float64(opts.DelayAvailableCapacity*1000))) * time.Millisecond)
	}

	totalCPU := d.cfg.CPULimit
	totalRAM := d.cfg.RAMLimit

	if totalCPU == 0 && totalRAM == 0 {
		// Resources are unlimited
		return math.MaxInt64
	}

	// Check if the node has the required resources - otherwise we can't run it anyhow
	if req.Resources.Cpu > totalCPU {
		return 0
	}
	if req.Resources.Ram > totalRAM {
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
			totalCPU += d.cfg.CPUOverbook
		}
		if nodeUsage.RamOverbook && req.Resources.RamOverbook {
			totalRAM += d.cfg.RAMOverbook
		}
	}

	// Calculate how much of those definitions we could run
	outCount = int64((totalCPU - nodeUsage.Cpu) / req.Resources.Cpu)
	ramCount := int64((totalRAM - nodeUsage.Ram) / req.Resources.Ram)
	if outCount > ramCount {
		outCount = ramCount
	}
	// TODO: Add disks into equation

	return outCount
}

// Allocate - pretends to Allocate (actually not) the Resource
func (d *Driver) Allocate(def typesv2.LabelDefinition, _ /*metadata*/ map[string]any) (*typesv2.ApplicationResource, error) {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		log.WithFunc("test", "Allocate").Error("Unable to apply options", "err", err)
		return nil, fmt.Errorf("TEST: Unable to apply options: %v", err)
	}

	if err := randomFail("Allocate", opts.FailAllocate); err != nil {
		log.WithFunc("test", "Allocate").Error("RandomFail", "err", err)
		return nil, fmt.Errorf("TEST: RandomFail: %v", err)
	}

	if opts.DelayAllocate > 0 {
		time.Sleep(time.Duration(math.Floor(float64(opts.DelayAllocate*1000))) * time.Millisecond)
	}

	// Generate random resource id and if exists - regenerate
	res := &typesv2.ApplicationResource{
		IpAddr:         "127.0.0.1",
		Authentication: def.Authentication,
	}
	var resFile string
	for {
		res.Identifier = "test-" + crypt.RandString(6)
		resFile = filepath.Join(d.cfg.WorkspacePath, res.Identifier)
		if _, err := os.Stat(resFile); os.IsNotExist(err) {
			break
		}
	}

	// Write identifier file
	fh, err := os.Create(resFile)
	if err != nil {
		return nil, fmt.Errorf("TEST: Unable to open file %q to store identifier: %v", resFile, err)
	}
	defer fh.Close()

	return res, nil
}

// Status shows status of the resource
func (d *Driver) Status(res typesv2.ApplicationResource) (string, error) {
	if res.Identifier == "" {
		return "", fmt.Errorf("TEST: Invalid resource: %v", res)
	}
	if err := randomFail(fmt.Sprintf("Status %s", res.Identifier), d.cfg.FailStatus); err != nil {
		return "", fmt.Errorf("TEST: RandomFail: %v", err)
	}

	resFile := filepath.Join(d.cfg.WorkspacePath, res.Identifier)
	if _, err := os.Stat(resFile); !os.IsNotExist(err) {
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
	if t != nil && len(options) > 0 {
		if err := json.Unmarshal([]byte(options), t); err != nil {
			log.WithFunc("test", "GetTask").Error("Unable to apply the task options", "err", err)
			return nil
		}
	}

	return t
}

// Deallocate the resource
func (d *Driver) Deallocate(res typesv2.ApplicationResource) error {
	if res.Identifier == "" {
		log.WithFunc("test", "Deallocate").Error("Invalid resource", "res", res)
		return fmt.Errorf("TEST: Invalid resource: %v", res)
	}
	if err := randomFail(fmt.Sprintf("Deallocate %s", res.Identifier), d.cfg.FailDeallocate); err != nil {
		log.WithFunc("test", "Deallocate").Error("RandomFail", "err", err)
		return fmt.Errorf("TEST: RandomFail: %v", err)
	}

	resFile := filepath.Join(d.cfg.WorkspacePath, res.Identifier)
	if _, err := os.Stat(resFile); os.IsNotExist(err) {
		return fmt.Errorf("TEST: Unable to deallocate unavailable resource '%s'", res.Identifier)
	}
	if err := os.Remove(resFile); err != nil {
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
	if uint8(rand.Intn(254)) < probability { //nolint:gosec // G402,G404 -- fine for test driver
		return fmt.Errorf("TEST: %s failed (%d)", name, probability)
	}

	return nil
}
