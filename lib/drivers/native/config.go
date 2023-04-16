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

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/adobe/aquarium-fish/lib/log"
)

type Config struct {
	//TODO: Users []string `json:"users"` // List of precreated OS user names in format "user[:password]" to run the workload

	SudoPath      string `json:"sudo_path"`      // Path to the sudo (privilege escalation) binary
	ImagesPath    string `json:"images_path"`    // Where to store/look the environment images
	WorkspacePath string `json:"workspace_path"` // Where to place the env disks

	// Alter allows you to control how much resources will be used:
	// * Negative (<0) value will alter the total resource count before provisioning so you will be
	//   able to save some resources for the host system (recommended -2 for CPU and -10 for RAM
	//   for disk caching)
	// * Positive (>0) is also available, but you're going to put more load on the scheduler
	//   Please be careful here - noone wants the workload to fail allocation because of that...
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

	DownloadUser     string `json:"download_user"`     // The user will be used to auth in download operations
	DownloadPassword string `json:"download_password"` // The password will be used to auth in download operations
}

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
	if err = os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}

	return nil
}

func (c *Config) Validate() (err error) {
	// Sudo is used to become the separated unprevileged user which will execute the workload
	// and execute a number of administrative actions to create/delete the user and cleanup
	if c.SudoPath == "" {
		if c.SudoPath, err = exec.LookPath("sudo"); err != nil {
			return fmt.Errorf("Native: Unable to locate `sudo` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.SudoPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `sudo` path: %s, %s", c.SudoPath, err)
		} else {
			if info.Mode()&0111 == 0 {
				return fmt.Errorf("Native: `sudo` binary is not executable: %s", c.SudoPath)
			}
		}
	}

	// Verify the configuration works for this machine
	var opts Options
	// If the users are not set - the user will be created dynamically
	// with "fish-" prefix and it's needed quite a good amount of access:

	// Verify user create
	user, _, err := userCreate(c, opts.Groups)
	if err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to create new user %q: %v", user, err)
	}

	// Create test init script
	init_path, err := testScriptCreate(user)
	if err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to create test script in %q: %v", init_path, err)
	}

	// Run the test init script
	if err = userRun(c, nil, user, init_path, map[string]any{}); err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to run test init script %q: %v", init_path, err)
	}

	// Cleaning up the test script
	if err := testScriptDelete(init_path); err != nil {
		userDelete(c, user)
		return fmt.Errorf("Native: Unable to delete test script in %q: %v", init_path, err)
	}

	// Clean after the run
	if err = userDelete(c, user); err != nil {
		return fmt.Errorf("Native: Unable to delete user %q: %v", user, err)
	}

	// TODO:
	// If precreated users are specified - check the user exists and we're
	// capable to control their home directory to unpack images or clean it.
	//
	// Sudo most probably still will be used to run the init process as
	// the user, but will require much less changes in the system.

	// Validating CpuAlter & RamAlter to not be less then the current cpu/ram count
	cpu_stat, err := cpu.Counts(true)
	if err != nil {
		return err
	}

	if c.CpuAlter < 0 && int(cpu_stat) <= -c.CpuAlter {
		return log.Errorf("Native: |CpuAlter| can't be more or equal the avaialble Host CPUs: |%d| > %d", c.CpuAlter, cpu_stat)
	}

	mem_stat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	ram_stat := mem_stat.Total / 1073741824 // Getting GB from Bytes

	if c.RamAlter < 0 && int(ram_stat) <= -c.RamAlter {
		return log.Errorf("Native: |RamAlter| can't be more or equal the avaialble Host RAM: |%d| > %d", c.RamAlter, ram_stat)
	}

	return nil
}

// Will create the config test script to run
func testScriptCreate(user string) (path string, err error) {
	path = filepath.Join("/tmp", user+"-init.sh")

	script := []byte("#!/bin/sh\nid\n")
	return path, os.WriteFile(path, script, 0755)
}

// Will delete the config test script
func testScriptDelete(path string) error {
	return os.Remove(path)
}
