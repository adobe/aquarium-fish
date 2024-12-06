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

package vmx

// VMWare VMX (Fusion/Workstation) driver to manage VMs & images

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Factory implements drivers.ResourceDriverFactory interface
type Factory struct{}

// Name shows name of the driver factory
func (*Factory) Name() string {
	return "vmx"
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
	totalRAM uint // In RAM GB
}

// Name returns name of the driver
func (*Driver) Name() string {
	return "vmx"
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
	d.totalCPU = uint(cpuStat)

	memStat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	d.totalRAM = uint(memStat.Total / 1073741824) // Getting GB from Bytes

	// TODO: Cleanup the image directory in case the images are not good

	return nil
}

// ValidateDefinition checks LabelDefinition is ok
func (*Driver) ValidateDefinition(def types.LabelDefinition) error {
	// Check resources
	if err := def.Resources.Validate([]string{"hfs+", "exfat", "fat32"}, true); err != nil {
		return log.Error("VMX: Resources validation failed:", err)
	}

	// Check options
	var opts Options
	return opts.Apply(def.Options)
}

// AvailableCapacity allows Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(nodeUsage types.Resources, req types.LabelDefinition) int64 {
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
func (d *Driver) Allocate(def types.LabelDefinition, _ /*metadata*/ map[string]any) (*types.Resource, error) {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, log.Error("VMX: Unable to apply options:", err)
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
		return nil, log.Error("VMX: Unable to load the required images:", err)
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
		return nil, log.Error("VMX: Unable to clone the target image:", imgPath, err)
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
		return nil, log.Error("VMX: Unable to change cloned VM configuration:", vmxPath, err)
	}

	// Create and connect disks to vmx
	if err := d.disksCreate(vmxPath, def.Resources.Disks); err != nil {
		d.cleanupVM(vmDir)
		return nil, log.Error("VMX: Unable create disks for VM:", vmxPath, err)
	}

	// Run the background monitoring of the vmware log
	if d.cfg.LogMonitor {
		go d.logMonitor(vmID, vmxPath)
	}

	// Run the VM
	if _, _, err := util.RunAndLog("VMX", 120*time.Second, nil, d.cfg.VmrunPath, "start", vmxPath, "nogui"); err != nil {
		log.Error("VMX: Check logs in ~/Library/Logs/VMware/ or enable debug to see vmware.log")
		d.cleanupVM(vmDir)
		return nil, log.Error("VMX: Unable to run VM:", vmxPath, err)
	}

	log.Info("VMX: Allocate of VM completed:", vmxPath)
	return &types.Resource{
		Identifier:     vmxPath,
		HwAddr:         vmHwaddr,
		Authentication: def.Authentication,
	}, nil
}

// Status shows status of the resource
func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("VMX: Invalid resource: %v", res)
	}
	if d.isAllocated(res.Identifier) {
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
			log.Error("VMX: Unable to apply the task options:", err)
			return nil
		}
	}

	return t
}

// Deallocate the resource
func (d *Driver) Deallocate(res *types.Resource) error {
	if res == nil || res.Identifier == "" {
		return fmt.Errorf("VMX: Invalid resource: %v", res)
	}
	vmxPath := res.Identifier
	if len(vmxPath) == 0 {
		return log.Error("VMX: Unable to find VM:", vmxPath)
	}

	// Sometimes it's stuck, so try to stop a bit more than usual
	if _, _, err := util.RunAndLogRetry("VMX", 3, 60*time.Second, nil, d.cfg.VmrunPath, "stop", vmxPath); err != nil {
		log.Warn("VMX: Unable to soft stop the VM:", vmxPath, err)
		// Ok, it doesn't want to stop, so stopping it hard
		if _, _, err := util.RunAndLogRetry("VMX", 3, 60*time.Second, nil, d.cfg.VmrunPath, "stop", vmxPath, "hard"); err != nil {
			return log.Error("VMX: Unable to deallocate VM:", vmxPath, err)
		}
	}

	// Delete VM
	if _, _, err := util.RunAndLogRetry("VMX", 3, 30*time.Second, nil, d.cfg.VmrunPath, "deleteVM", vmxPath); err != nil {
		return log.Error("VMX: Unable to delete VM:", vmxPath, err)
	}

	// Cleaning the VM images too
	d.cleanupVM(filepath.Dir(vmxPath))

	log.Info("VMX: Deallocate of VM completed:", vmxPath)

	return nil
}
