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

package vmx

// VMWare VMX (Fusion/Workstation) driver to manage VMs & images

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"

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
	return "vmx"
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
func (*Driver) IsRemote() bool {
	return false
}

// Prepare initializes the driver
func (d *Driver) Prepare(config []byte) error {
	if err := d.cfg.Apply(config); err != nil {
		return err
	}
	if err := d.cfg.Validate(); err != nil {
		return err
	}

	// Collect node resources status
	cpuStat, err := cpu.Counts(true)
	if err != nil {
		return err
	}
	d.totalCPU = uint32(cpuStat)

	memStat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	d.totalRAM = uint32(memStat.Total / 1073741824) // Getting GB from Bytes

	// TODO: Cleanup the image directory in case the images are not good

	return nil
}

// ValidateDefinition checks LabelDefinition is ok
func (d *Driver) ValidateDefinition(def typesv2.LabelDefinition) error {
	// Check resources
	if err := def.Resources.Validate([]string{"hfs+", "exfat", "fat32"}, true); err != nil {
		log.Error().Msgf("VMX: %s: Resources validation failed: %v", d.name, err)
		return fmt.Errorf("VMX: %s: Resources validation failed: %v", d.name, err)
	}

	// Check options
	var opts Options
	return opts.Apply(def.Options)
}

// AvailableCapacity allows Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(nodeUsage typesv2.Resources, req typesv2.LabelDefinition) int64 {
	var outCount int64

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

// Allocate VM with provided images
//
// It automatically download the required images, unpack them and runs the VM.
// Not using metadata because there is no good interfaces to pass it to VM.
func (d *Driver) Allocate(def typesv2.LabelDefinition, _ /*metadata*/ map[string]any) (*typesv2.ApplicationResource, error) {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		log.Error().Msgf("VMX: %s: Unable to apply options: %v", d.name, err)
		return nil, fmt.Errorf("VMX: %s: Unable to apply options: %v", d.name, err)
	}

	// Generate unique id from the hw address and required directories
	buf := crypt.RandBytes(6)
	buf[0] = (buf[0] | 2) & 0xfe // Set local bit, ensure unicast address
	vmID := fmt.Sprintf("%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
	vmHwaddr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

	vmNetwork := def.Resources.Network
	if vmNetwork == "" {
		vmNetwork = "hostonly"
	}

	vmDir := filepath.Join(d.cfg.WorkspacePath, vmID)
	vmImagesDir := filepath.Join(vmDir, "images")

	// Load the required images
	imgPath, err := d.loadImages(&opts, vmImagesDir)
	if err != nil {
		d.cleanupVM(vmDir)
		log.Error().Msgf("VMX: %s: Unable to load the required images: %v", d.name, err)
		return nil, fmt.Errorf("VMX: %s: Unable to load the required images: %v", d.name, err)
	}

	// Clone VM from the image
	vmxPath := filepath.Join(vmDir, vmID+".vmx")
	args := []string{"-T", "fusion", "clone",
		imgPath, vmxPath,
		"linked", "-snapshot", "original",
		"-cloneName", vmID,
	}
	if _, _, err := util.RunAndLog("VMX", 120*time.Second, nil, d.cfg.VmrunPath, args...); err != nil {
		d.cleanupVM(vmDir)
		log.Error().Msgf("VMX: %s: Unable to clone the target image %q: %v", d.name, imgPath, err)
		return nil, fmt.Errorf("VMX: %s: Unable to clone the target image %q: %v", d.name, imgPath, err)
	}

	// Change cloned vm configuration
	if err := util.FileReplaceToken(vmxPath,
		true, true, true,
		"ethernet0.addressType =", `ethernet0.addressType = "static"`,
		"ethernet0.address =", fmt.Sprintf("ethernet0.address = %q", vmHwaddr),
		"ethernet0.connectiontype =", fmt.Sprintf("ethernet0.connectiontype = %q", vmNetwork),
		"numvcpus =", fmt.Sprintf(`numvcpus = "%d"`, def.Resources.Cpu),
		"cpuid.corespersocket =", fmt.Sprintf(`cpuid.corespersocket = "%d"`, def.Resources.Cpu),
		"memsize =", fmt.Sprintf(`memsize = "%d"`, def.Resources.Ram*1024),
	); err != nil {
		d.cleanupVM(vmDir)
		log.Error().Msgf("VMX: %s: Unable to change cloned VM %q configuration: %v", d.name, vmxPath, err)
		return nil, fmt.Errorf("VMX: %s: Unable to change cloned VM %q configuration: %v", d.name, vmxPath, err)
	}

	// Create and connect disks to vmx
	if err := d.disksCreate(vmxPath, def.Resources.Disks); err != nil {
		d.cleanupVM(vmDir)
		log.Error().Msgf("VMX: %s: Unable create disks for VM %q: %v", d.name, vmxPath, err)
		return nil, fmt.Errorf("VMX: %s: Unable create disks for VM %q: %v", d.name, vmxPath, err)
	}

	// Run the background monitoring of the vmware log
	if d.cfg.LogMonitor {
		go d.logMonitor(vmID, vmxPath)
	}

	// Run the VM
	if _, _, err := util.RunAndLog("VMX", 120*time.Second, nil, d.cfg.VmrunPath, "start", vmxPath, "nogui"); err != nil {
		log.Error().Msgf("VMX: %s: Check logs in ~/Library/Logs/VMware/ or enable debug to see vmware.log", d.name)
		d.cleanupVM(vmDir)
		log.Error().Msgf("VMX: %s: Unable to run VM %q: %v", d.name, vmxPath, err)
		return nil, fmt.Errorf("VMX: %s: Unable to run VM %q: %v", d.name, vmxPath, err)
	}

	log.Info().Msgf("VMX: %s: Allocate of VM completed: %s", d.name, vmxPath)
	return &typesv2.ApplicationResource{
		Identifier:     vmxPath,
		HwAddr:         vmHwaddr,
		Authentication: def.Authentication,
	}, nil
}

// Status shows status of the resource
func (d *Driver) Status(res typesv2.ApplicationResource) (string, error) {
	if res.Identifier == "" {
		return "", fmt.Errorf("VMX: %s: Invalid resource: %v", d.name, res)
	}
	if d.isAllocated(res.Identifier) {
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
			log.Error().Msgf("VMX: %s: Unable to apply the task options: %v", d.name, err)
			return nil
		}
	}

	return t
}

// Deallocate the resource
func (d *Driver) Deallocate(res typesv2.ApplicationResource) error {
	if res.Identifier == "" {
		return fmt.Errorf("VMX: %s: Invalid resource: %v", d.name, res)
	}
	vmxPath := res.Identifier
	if len(vmxPath) == 0 {
		log.Error().Msgf("VMX: %s: Unable to find VM: %s", d.name, vmxPath)
		return fmt.Errorf("VMX: %s: Unable to find VM: %s", d.name, vmxPath)
	}

	// Sometimes it's stuck, so try to stop a bit more than usual
	if _, _, err := util.RunAndLogRetry("VMX", 3, 60*time.Second, nil, d.cfg.VmrunPath, "stop", vmxPath); err != nil {
		log.Warn().Msgf("VMX: %s: Unable to soft stop the VM %q: %v", d.name, vmxPath, err)
		// Ok, it doesn't want to stop, so stopping it hard
		if _, _, err := util.RunAndLogRetry("VMX", 3, 60*time.Second, nil, d.cfg.VmrunPath, "stop", vmxPath, "hard"); err != nil {
			log.Error().Msgf("VMX: %s: Unable to deallocate VM %q: %s", d.name, vmxPath, err)
			return fmt.Errorf("VMX: %s: Unable to deallocate VM %q: %s", d.name, vmxPath, err)
		}
	}

	// Delete VM
	if _, _, err := util.RunAndLogRetry("VMX", 3, 30*time.Second, nil, d.cfg.VmrunPath, "deleteVM", vmxPath); err != nil {
		log.Error().Msgf("VMX: %s: Unable to delete VM %q: %v", d.name, vmxPath, err)
		return fmt.Errorf("VMX: %s: Unable to delete VM %q: %v", d.name, vmxPath, err)
	}

	// Cleaning the VM images too
	d.cleanupVM(filepath.Dir(vmxPath))

	log.Info().Msgf("VMX: %s: Deallocate of VM completed: %s", d.name, vmxPath)

	return nil
}
