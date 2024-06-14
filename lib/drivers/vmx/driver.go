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

// Implements drivers.ResourceDriverFactory interface
type Factory struct{}

func (f *Factory) Name() string {
	return "vmx"
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

func (d *Driver) Name() string {
	return "vmx"
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
	// Check resources
	if err := def.Resources.Validate([]string{"hfs+", "exfat", "fat32"}, true); err != nil {
		return log.Error("VMX: Resources validation failed:", err)
	}

	// Check options
	var opts Options
	return opts.Apply(def.Options)
}

// Allow Fish to ask the driver about it's capacity (free slots) of a specific definition
func (d *Driver) AvailableCapacity(node_usage types.Resources, req types.LabelDefinition) int64 {
	var out_count int64

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
 * Allocate VM with provided images
 *
 * It automatically download the required images, unpack them and runs the VM.
 * Not using metadata because there is no good interfaces to pass it to VM.
 */
func (d *Driver) Allocate(def types.LabelDefinition, metadata map[string]any) (*types.Resource, error) {
	var opts Options
	if err := opts.Apply(def.Options); err != nil {
		return nil, log.Error("VMX: Unable to apply options:", err)
	}

	// Generate unique id from the hw address and required directories
	buf := crypt.RandBytes(6)
	buf[0] = (buf[0] | 2) & 0xfe // Set local bit, ensure unicast address
	vm_id := fmt.Sprintf("%02x%02x%02x%02x%02x%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])
	vm_hwaddr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

	vm_network := def.Resources.Network
	if vm_network == "" {
		vm_network = "hostonly"
	}

	vm_dir := filepath.Join(d.cfg.WorkspacePath, vm_id)
	vm_images_dir := filepath.Join(vm_dir, "images")

	// Load the required images
	img_path, err := d.loadImages(&opts, vm_images_dir)
	if err != nil {
		d.cleanupVm(vm_dir)
		return nil, log.Error("VMX: Unable to load the required images:", err)
	}

	// Clone VM from the image
	vmx_path := filepath.Join(vm_dir, vm_id+".vmx")
	args := []string{"-T", "fusion", "clone",
		img_path, vmx_path,
		"linked", "-snapshot", "original",
		"-cloneName", vm_id,
	}
	if _, _, err := runAndLog(120*time.Second, d.cfg.VmrunPath, args...); err != nil {
		d.cleanupVm(vm_dir)
		return nil, log.Error("VMX: Unable to clone the target image:", img_path, err)
	}

	// Change cloned vm configuration
	if err := util.FileReplaceToken(vmx_path,
		true, true, true,
		"ethernet0.addressType =", `ethernet0.addressType = "static"`,
		"ethernet0.address =", fmt.Sprintf("ethernet0.address = %q", vm_hwaddr),
		"ethernet0.connectiontype =", fmt.Sprintf("ethernet0.connectiontype = %q", vm_network),
		"numvcpus =", fmt.Sprintf(`numvcpus = "%d"`, def.Resources.Cpu),
		"cpuid.corespersocket =", fmt.Sprintf(`cpuid.corespersocket = "%d"`, def.Resources.Cpu),
		"memsize =", fmt.Sprintf(`memsize = "%d"`, def.Resources.Ram*1024),
	); err != nil {
		d.cleanupVm(vm_dir)
		return nil, log.Error("VMX: Unable to change cloned VM configuration:", vmx_path, err)
	}

	// Create and connect disks to vmx
	if err := d.disksCreate(vmx_path, def.Resources.Disks); err != nil {
		d.cleanupVm(vm_dir)
		return nil, log.Error("VMX: Unable create disks for VM:", vmx_path, err)
	}

	// Run the background monitoring of the vmware log
	if d.cfg.LogMonitor {
		go d.logMonitor(vm_id, vmx_path)
	}

	// Run the VM
	if _, _, err := runAndLog(120*time.Second, d.cfg.VmrunPath, "start", vmx_path, "nogui"); err != nil {
		log.Error("VMX: Check logs in ~/Library/Logs/VMware/ or enable debug to see vmware.log")
		d.cleanupVm(vm_dir)
		return nil, log.Error("VMX: Unable to run VM:", vmx_path, err)
	}

	log.Info("VMX: Allocate of VM completed:", vmx_path)

	return &types.Resource{Identifier: vmx_path, HwAddr: vm_hwaddr}, nil
}

func (d *Driver) Status(res *types.Resource) (string, error) {
	if res == nil || res.Identifier == "" {
		return "", fmt.Errorf("VMX: Invalid resource: %v", res)
	}
	if d.isAllocated(res.Identifier) {
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
		return fmt.Errorf("VMX: Invalid resource: %v", res)
	}
	vmx_path := res.Identifier
	if len(vmx_path) == 0 {
		return log.Error("VMX: Unable to find VM:", vmx_path)
	}

	// Sometimes it's stuck, so try to stop a bit more than usual
	if _, _, err := runAndLogRetry(3, 60*time.Second, d.cfg.VmrunPath, "stop", vmx_path); err != nil {
		log.Warn("VMX: Unable to soft stop the VM:", vmx_path, err)
		// Ok, it doesn't want to stop, so stopping it hard
		if _, _, err := runAndLogRetry(3, 60*time.Second, d.cfg.VmrunPath, "stop", vmx_path, "hard"); err != nil {
			return log.Error("VMX: Unable to deallocate VM:", vmx_path, err)
		}
	}

	// Delete VM
	if _, _, err := runAndLogRetry(3, 30*time.Second, d.cfg.VmrunPath, "deleteVM", vmx_path); err != nil {
		return log.Error("VMX: Unable to delete VM:", vmx_path, err)
	}

	// Cleaning the VM images too
	d.cleanupVm(filepath.Dir(vmx_path))

	log.Info("VMX: Deallocate of VM completed:", vmx_path)

	return nil
}
