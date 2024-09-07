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

// Package native implements driver
package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Config - node driver configuration
type Config struct {
	//TODO: Users []string `json:"users"` // List of precreated OS user names in format "user[:password]" to run the workload

	// Embed platform-specific items
	PlatformConfig

	TarPath       string `json:"tar_path"`       // Path to the tar (unpacking images) binary
	ImagesPath    string `json:"images_path"`    // Where to store/look the environment images
	WorkspacePath string `json:"workspace_path"` // Where to place the env disks

	// Alter allows you to control how much resources will be used:
	// * Negative (<0) value will alter the total resource count before provisioning so you will be
	//   able to save some resources for the host system (recommended -2 for CPU and -10 for RAM
	//   for disk caching)
	// * Positive (>0) is also available, but you're going to put more load on the scheduler
	//   Please be careful here - noone wants the workload to fail allocation because of that...
	CPUAlter int `json:"cpu_alter"` // 0 do nothing, <0 reduces number available CPUs, >0 increases it (dangerous)
	RAMAlter int `json:"ram_alter"` // 0 do nothing, <0 reduces amount of available RAM (GB), >0 increases it (dangerous)

	// Overbook options allows tenants to reuse the resources
	// It will be used only when overbook is allowed by the tenants. It works by just adding those
	// amounts to the existing total before checking availability. For example if you have 16CPU
	// and want to run 2 tenants with requirement of 14 CPUs each - you can put 12 in CPUOverbook -
	// to have virtually 28 CPUs. 3rd will not be running because 2 tenants will eat all 28 virtual
	// CPUs. Same applies to the RamOverbook.
	CPUOverbook uint `json:"cpu_overbook"` // How much CPUs could be reused by multiple tenants
	RAMOverbook uint `json:"ram_overbook"` // How much RAM (GB) could be reused by multiple tenants

	DownloadUser     string `json:"download_user"`     // The user will be used to auth in download operations
	DownloadPassword string `json:"download_password"` // The password will be used to auth in download operations
}

// Apply takes json and applies it to the config structure
func (c *Config) Apply(config []byte) (err error) {
	if len(config) > 0 {
		if err = json.Unmarshal(config, c); err != nil {
			return fmt.Errorf("Native: Unable to apply the driver config: %s", err)
		}
	}

	if c.ImagesPath == "" {
		c.ImagesPath = "fish_native_images"
	}
	if c.WorkspacePath == "" {
		c.WorkspacePath = "fish_native_workspace"
	}

	// Making Image path absolute
	if c.ImagesPath, err = filepath.Abs(c.ImagesPath); err != nil {
		return err
	}

	if c.WorkspacePath, err = filepath.Abs(c.WorkspacePath); err != nil {
		return err
	}

	log.Debug("Native: Creating working directories:", c.ImagesPath, c.WorkspacePath)
	if err = os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}

	err = os.MkdirAll(c.WorkspacePath, 0o750)

	return err
}

// Validate makes sure the config have the required defaults & that the required fields are set
func (c *Config) Validate() (err error) {
	err, err2 := c.validateForPlatform(err)
	if err2 != nil {
		return err2
	}

	// Verify the configuration works for this machine
	var opts Options
	opts.Validate()
	// If the users are not set - the user will be created dynamically
	// with "fish-" prefix and it's needed quite a good amount of access:

	// Verify user create
	user, _, err := userCreate(c, opts.Groups)
	if err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to create new user %q: %v", user, err)
	}

	// Create test init script
	initPath, err := testScriptCreate(user)
	if err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to create test script in %q: %v", initPath, err)
	}

	// Run the test init script
	if err = userRun(c, nil, user, initPath, map[string]any{}); err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to run test init script %q: %v", initPath, err)
	}

	// Cleaning up the test script
	if err := testScriptDelete(initPath); err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to delete test script in %q: %v", initPath, err)
	}

	// Clean after the run
	if err = userDelete(c, user); err != nil {
		return fmt.Errorf("Native: Unable to delete user in the end of driver verification %q: %v", user, err)
	}

	// TODO:
	// If pre-created users are specified - check the user exists and we're
	// capable to control their home directory to unpack images or clean it.
	//
	// Sudo most probably still will be used to run the init process as
	// the user, but will require much less changes in the system.

	// Validating CpuAlter & RamAlter to not be less then the current cpu/ram count
	cpuStat, err := cpu.Counts(true)
	if err != nil {
		return err
	}

	if c.CPUAlter < 0 && cpuStat <= -c.CPUAlter {
		return log.Errorf("Native: |CpuAlter| can't be more or equal the available Host CPUs: |%d| > %d", c.CPUAlter, cpuStat)
	}

	memStat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	ramStat := memStat.Total / 1073741824 // Getting GB from Bytes

	if c.RAMAlter < 0 && int(ramStat) <= -c.RAMAlter {
		return log.Errorf("Native: |RamAlter| can't be more or equal the available Host RAM: |%d| > %d", c.RAMAlter, ramStat)
	}

	return nil
}
