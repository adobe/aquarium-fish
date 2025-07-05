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

// Package native implements driver
package native

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Config - node driver configuration
type Config struct {
	//TODO: Users []string `json:"users"` // List of precreated OS user names in format "user[:password]" to run the workload

	SuPath      string `json:"su_path"`      // Path to the su (login as user) binary
	SudoPath    string `json:"sudo_path"`    // Path to the sudo (privilege escalation) binary
	ShPath      string `json:"sh_path"`      // Path to the sh (simple user shell) binary
	TarPath     string `json:"tar_path"`     // Path to the tar (unpacking images) binary
	MountPath   string `json:"mount_path"`   // Path to the mount (list of mounted volumes) binary
	ChownPath   string `json:"chown_path"`   // Path to the chown (change file/dir ownership) binary
	ChmodPath   string `json:"chmod_path"`   // Path to the chmod (change file/dir access) binary
	KillallPath string `json:"killall_path"` // Path to the killall (send signals to multiple processes) binary
	RmPath      string `json:"rm_path"`      // Path to the rm (cleanup after execution) binary

	ImagesPath    string `json:"images_path"`    // Where to store/look the environment images
	WorkspacePath string `json:"workspace_path"` // Where to place the env disks

	DsclPath          string `json:"dscl_path"`          // Path to the dscl (macos user control) binary
	HdiutilPath       string `json:"hdiutil_path"`       // Path to the hdiutil (macos images create/mount/umount) binary
	MdutilPath        string `json:"mdutil_path"`        // Path to the mdutil (macos disable indexing for disks) binary
	CreatehomedirPath string `json:"createhomedir_path"` // Path to the createhomedir (macos create/prefill user directory) binary

	// Alter allows you to control how much resources will be used:
	// * Negative (<0) value will alter the total resource count before provisioning so you will be
	//   able to save some resources for the host system (recommended -2 for CPU and -10 for RAM
	//   for disk caching)
	// * Positive (>0) is also available, but you're going to put more load on the scheduler
	//   Please be careful here - noone wants the workload to fail allocation because of that...
	CPUAlter int32 `json:"cpu_alter"` // 0 do nothing, <0 reduces number available CPUs, >0 increases it (dangerous)
	RAMAlter int32 `json:"ram_alter"` // 0 do nothing, <0 reduces amount of available RAM (GB), >0 increases it (dangerous)

	// Overbook options allows tenants to reuse the resources
	// It will be used only when overbook is allowed by the tenants. It works by just adding those
	// amounts to the existing total before checking availability. For example if you have 16CPU
	// and want to run 2 tenants with requirement of 14 CPUs each - you can put 12 in CPUOverbook -
	// to have virtually 28 CPUs. 3rd will not be running because 2 tenants will eat all 28 virtual
	// CPUs. Same applies to the RamOverbook.
	CPUOverbook uint32 `json:"cpu_overbook"` // How much CPUs could be reused by multiple tenants
	RAMOverbook uint32 `json:"ram_overbook"` // How much RAM (GB) could be reused by multiple tenants

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

	log.Debug().Msgf("Native: Creating working directories: %s %s", c.ImagesPath, c.WorkspacePath)
	if err = os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}

	err = os.MkdirAll(c.WorkspacePath, 0o750)

	return err
}

// Validate makes sure the config have the required defaults & that the required fields are set
func (c *Config) Validate(drv *Driver) (err error) {
	// Sudo is used to run commands from superuser and execute a number of
	// administrative actions to create/delete the user and cleanup
	if c.SudoPath == "" {
		if c.SudoPath, err = exec.LookPath("sudo"); err != nil {
			return fmt.Errorf("Native: Unable to locate `sudo` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.SudoPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `sudo` path: %s, %s", c.SudoPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `sudo` binary is not executable: %s", c.SudoPath)
		}
	}

	// Su is used to become the separated unprevileged user and control whom to become in sudoers
	if c.SuPath == "" {
		if c.SuPath, err = exec.LookPath("su"); err != nil {
			return fmt.Errorf("Native: Unable to locate `su` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.SuPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `su` path: %s, %s", c.SuPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `su` binary is not executable: %s", c.SuPath)
		}
	}

	// Sh is needed to set the unprevileged user default executable
	if c.ShPath == "" {
		if c.ShPath, err = exec.LookPath("sh"); err != nil {
			return fmt.Errorf("Native: Unable to locate `su` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.ShPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `sh` path: %s, %s", c.ShPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `sh` binary is not executable: %s", c.ShPath)
		}
	}
	// Tar used to unpack the images
	if c.TarPath == "" {
		if c.TarPath, err = exec.LookPath("tar"); err != nil {
			return fmt.Errorf("Native: Unable to locate `tar` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.TarPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `tar` path: %s, %s", c.TarPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `tar` binary is not executable: %s", c.TarPath)
		}
	}
	// Mount allows to look at the mounted volumes
	if c.MountPath == "" {
		if c.MountPath, err = exec.LookPath("mount"); err != nil {
			return fmt.Errorf("Native: Unable to locate `mount` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.MountPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `mount` path: %s, %s", c.MountPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `mount` binary is not executable: %s", c.MountPath)
		}
	}
	// Chown needed to properly set ownership for the unprevileged user on available resources
	if c.ChownPath == "" {
		if c.ChownPath, err = exec.LookPath("chown"); err != nil {
			return fmt.Errorf("Native: Unable to locate `chown` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.ChownPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `chown` path: %s, %s", c.ChownPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `chown` binary is not executable: %s", c.ChownPath)
		}
	}
	// Chmod needed to set additional read access for the unprevileged user on env metadata file
	if c.ChmodPath == "" {
		if c.ChmodPath, err = exec.LookPath("chmod"); err != nil {
			return fmt.Errorf("Native: Unable to locate `chmod` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.ChmodPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `chmod` path: %s, %s", c.ChmodPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `chmod` binary is not executable: %s", c.ChmodPath)
		}
	}
	// Killall is running to stop all the unprevileged user processes during deallocation
	if c.KillallPath == "" {
		if c.KillallPath, err = exec.LookPath("killall"); err != nil {
			return fmt.Errorf("Native: Unable to locate `killall` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.KillallPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `killall` path: %s, %s", c.KillallPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `killall` binary is not executable: %s", c.KillallPath)
		}
	}
	// Rm allows to clean up the leftowers after the execution
	if c.RmPath == "" {
		if c.RmPath, err = exec.LookPath("rm"); err != nil {
			return fmt.Errorf("Native: Unable to locate `rm` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.RmPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate `rm` path: %s, %s", c.RmPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: `rm` binary is not executable: %s", c.RmPath)
		}
	}

	// MacOS specific ones:
	// Dscl creates/removes the unprevileged user
	if c.DsclPath == "" {
		if c.DsclPath, err = exec.LookPath("dscl"); err != nil {
			return fmt.Errorf("Native: Unable to locate macos `dscl` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.DsclPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate macos `dscl` path: %s, %s", c.DsclPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: macos `dscl` binary is not executable: %s", c.DsclPath)
		}
	}
	// Hdiutil allows to create disk images and mount them to restrict user by disk space
	if c.HdiutilPath == "" {
		if c.HdiutilPath, err = exec.LookPath("hdiutil"); err != nil {
			return fmt.Errorf("Native: Unable to locate macos `hdiutil` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.HdiutilPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate macos `hdiutil` path: %s, %s", c.HdiutilPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: macos `hdiutil` binary is not executable: %s", c.HdiutilPath)
		}
	}
	// Mdutil allows to disable the indexing for mounted volume
	if c.MdutilPath == "" {
		if c.MdutilPath, err = exec.LookPath("mdutil"); err != nil {
			return fmt.Errorf("Native: Unable to locate macos `mdutil` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.MdutilPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate macos `mdutil` path: %s, %s", c.MdutilPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: macos `mdutil` binary is not executable: %s", c.MdutilPath)
		}
	}
	// Createhomedir creates unprevileged user home directory and fulfills with default subdirs
	if c.CreatehomedirPath == "" {
		if c.CreatehomedirPath, err = exec.LookPath("createhomedir"); err != nil {
			return fmt.Errorf("Native: Unable to locate macos `createhomedir` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.CreatehomedirPath); os.IsNotExist(err) {
			return fmt.Errorf("Native: Unable to locate macos `createhomedir` path: %s, %s", c.CreatehomedirPath, err)
		} else if info.Mode()&0o111 == 0 {
			return fmt.Errorf("Native: macos `createhomedir` binary is not executable: %s", c.CreatehomedirPath)
		}
	}

	// Verify the configuration works for this machine
	var opts Options
	opts.Validate()
	// If the users are not set - the user will be created dynamically
	// with "fish-" prefix and it's needed quite a good amount of access:

	// Verify user create
	user, _, err := drv.userCreate(opts.Groups)
	if err != nil {
		drv.userDelete(user)
		return fmt.Errorf("Native: Unable to create new user %q: %v", user, err)
	}

	// Create test init script
	initPath, err := testScriptCreate(user)
	if err != nil {
		drv.userDelete(user)
		return fmt.Errorf("Native: Unable to create test script in %q: %v", initPath, err)
	}

	// Run the test init script
	if err = drv.userRun(nil, user, initPath, map[string]any{}); err != nil {
		drv.userDelete(user)
		return fmt.Errorf("Native: Unable to run test init script %q: %v", initPath, err)
	}

	// Cleaning up the test script
	if err := testScriptDelete(initPath); err != nil {
		drv.userDelete(user)
		return fmt.Errorf("Native: Unable to delete test script in %q: %v", initPath, err)
	}

	// Clean after the run
	if err = drv.userDelete(user); err != nil {
		return fmt.Errorf("Native: Unable to delete user in the end of driver verification %q: %v", user, err)
	}

	// TODO:
	// If precreated users are specified - check the user exists and we're
	// capable to control their home directory to unpack images or clean it.
	//
	// Sudo most probably still will be used to run the init process as
	// the user, but will require much less changes in the system.

	// Validating CpuAlter & RamAlter to not be less then the current cpu/ram count
	cpuStat, err := cpu.Counts(true)
	if err != nil {
		return err
	}

	if c.CPUAlter < 0 && cpuStat <= int(-c.CPUAlter) {
		log.Error().Msgf("Native: |CpuAlter| can't be more or equal the available Host CPUs: |%d| > %d", c.CPUAlter, cpuStat)
		return fmt.Errorf("Native: |CpuAlter| can't be more or equal the available Host CPUs: |%d| > %d", c.CPUAlter, cpuStat)
	}

	memStat, err := mem.VirtualMemory()
	if err != nil {
		return err
	}
	ramStat := memStat.Total / 1073741824 // Getting GB from Bytes

	if c.RAMAlter < 0 && ramStat <= uint64(-c.RAMAlter) {
		log.Error().Msgf("Native: |RamAlter| can't be more or equal the available Host RAM: |%d| > %d", c.RAMAlter, ramStat)
		return fmt.Errorf("Native: |RamAlter| can't be more or equal the available Host RAM: |%d| > %d", c.RAMAlter, ramStat)
	}

	return nil
}

// Will create the config test script to run
func testScriptCreate(user string) (path string, err error) {
	path = filepath.Join("/tmp", user+"-init.sh")

	script := []byte("#!/bin/sh\nid\n")
	return path, os.WriteFile(path, script, 0o755) // #nosec G306
}

// Will delete the config test script
func testScriptDelete(path string) error {
	return os.Remove(path)
}
