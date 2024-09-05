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

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/adobe/aquarium-fish/lib/log"
)

type Config struct {
	VmrunPath        string `json:"vmrun_path"`        // '/Applications/VMware Fusion.app/Contents/Library/vmrun'
	VdiskmanagerPath string `json:"vdiskmanager_path"` // '/Applications/VMware Fusion.app/Contents/Library/vmware-vdiskmanager'

	ImagesPath    string `json:"images_path"`    // Where to look/store VM images
	WorkspacePath string `json:"workspace_path"` // Where to place the cloned VM and disks

	// Alter allows you to control how much resources will be used:
	// * Negative (<0) value will alter the total resource count before provisioning so you will be
	//   able to save some resources for the host system (recommended -2 for CPU and -10 for RAM
	//   for disk caching)
	// * Positive (>0) value could also be available (but check it in your vmware dist in advance)
	//   Please be careful here - noone wants the VM to fail allocation because of that...
	CpuAlter int `json:"cpu_alter"` // 0 do nothing, <0 reduces number available CPUs, >0 increases it (dangerous)
	RamAlter int `json:"ram_alter"` // 0 do nothing, <0 reduces amount of available RAM (GB), >0 increases it (dangerous)

	// Overbook options allows tenants to reuse the resources
	// It will be used only when overbook is allowed by the tenants. It works by just adding those
	// amounts to the existing total before checking availability. For example if you have 16CPU
	// and want to run 2 tenants with requirement of 14 CPUs each - you can put 12 in CpuOverbook -
	// to have virtually 28 CPUs. 3rd will not be running because 2 tenants will eat all 28 virtual
	// CPUs. Same applies to the RamOverbook.
	CpuOverbook uint `json:"cpu_overbook"` // How much CPUs could be reused by multiple tenants
	RamOverbook uint `json:"ram_overbook"` // How much RAM (GB) could be reused by multiple tenants

	DownloadUser     string `json:"download_user"`     // The user will be used in download operations
	DownloadPassword string `json:"download_password"` // The password will be used in download operations

	LogMonitor bool `json:"log_monitor"` // Actively monitor the vmware.log of VM and reset it on halt
}

func (c *Config) Apply(config []byte) error {
	// Set defaults
	c.LogMonitor = true

	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("VMX: Unable to apply the driver config:", err)
		}
	}
	return nil
}

func (c *Config) Validate() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.VmrunPath == "" {
		// Look in the PATH
		if c.VmrunPath, err = exec.LookPath("vmrun"); err != nil {
			return log.Error("VMX: Unable to locate `vmrun` path:", err)
		}
	}
	if c.VdiskmanagerPath == "" {
		// Use VmrunPath to get the path
		c.VdiskmanagerPath = filepath.Join(filepath.Dir(filepath.Dir(c.VmrunPath)), "Library", "vmware-vdiskmanager")
		if _, err := os.Stat(c.VdiskmanagerPath); os.IsNotExist(err) {
			return log.Error("VMX: Unable to locate `vmware-vdiskmanager` path:", err)
		}
	}
	if c.ImagesPath == "" {
		c.ImagesPath = "fish_vmx_images"
	}
	if c.WorkspacePath == "" {
		c.WorkspacePath = "fish_vmx_workspace"
	}

	// Making paths absolute
	if c.ImagesPath, err = filepath.Abs(c.ImagesPath); err != nil {
		return err
	}
	if c.WorkspacePath, err = filepath.Abs(c.WorkspacePath); err != nil {
		return err
	}

	log.Debug("VMX: Creating working directories:", c.ImagesPath, c.WorkspacePath)
	if err := os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}

	// Validating CpuAlter & RamAlter to not be less then the current cpu/ram count
	cpu_stat, err := cpu.Counts(true)
	if err != nil {
		return err
	}

	if c.CpuAlter < 0 && cpu_stat <= -c.CpuAlter {
		return log.Errorf("VMX: |CpuAlter| can't be more or equal the available Host CPUs: |%d| > %d", c.CpuAlter, cpu_stat)
	}

	mem_stat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	ram_stat := mem_stat.Total / 1073741824 // Getting GB from Bytes

	if c.RamAlter < 0 && int(ram_stat) <= -c.RamAlter {
		return log.Errorf("VMX: |RamAlter| can't be more or equal the available Host RAM: |%d| > %d", c.RamAlter, ram_stat)
	}

	return nil
}
