//go:build !windows

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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Config - Elements shared by posix drivers
type PosixConfig struct {
	SuPath      string `json:"su_path"`      // Path to the su (login as user) binary
	SudoPath    string `json:"sudo_path"`    // Path to the sudo (privilege escalation) binary
	ShPath      string `json:"sh_path"`      // Path to the sh (simple user shell) binary
	TarPath     string `json:"tar_path"`     // Path to the tar (unpacking images) binary
	MountPath   string `json:"mount_path"`   // Path to the mount (list of mounted volumes) binary
	ChownPath   string `json:"chown_path"`   // Path to the chown (change file/dir ownership) binary
	ChmodPath   string `json:"chmod_path"`   // Path to the chmod (change file/dir access) binary
	KillallPath string `json:"killall_path"` // Path to the killall (send signals to multiple processes) binary
	RmPath      string `json:"rm_path"`      // Path to the rm (cleanup after execution) binary

}

func (c *Config) validatePosix(err error) (error, error, error) {
	// Sudo is used to run commands from superuser and execute a number of
	// administrative actions to create/delete the user and cleanup
	if c.SudoPath == "" {
		if c.SudoPath, err = exec.LookPath("sudo"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `sudo` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.SudoPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `sudo` path: %s, %s", c.SudoPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `sudo` binary is not executable: %s", c.SudoPath)
		}
	}

	// Su is used to become the separated unprivileged user and control whom to become in sudoers
	if c.SuPath == "" {
		if c.SuPath, err = exec.LookPath("su"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `su` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.SuPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `su` path: %s, %s", c.SuPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `su` binary is not executable: %s", c.SuPath)
		}
	}

	// Sh is needed to set the unprivileged user default executable
	if c.ShPath == "" {
		if c.ShPath, err = exec.LookPath("sh"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `su` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.ShPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `sh` path: %s, %s", c.ShPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `sh` binary is not executable: %s", c.ShPath)
		}
	}
	// Tar used to unpack the images
	if c.TarPath == "" {
		if c.TarPath, err = exec.LookPath("tar"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `tar` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.TarPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `tar` path: %s, %s", c.TarPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `tar` binary is not executable: %s", c.TarPath)
		}
	}
	// Mount allows to look at the mounted volumes
	if c.MountPath == "" {
		if c.MountPath, err = exec.LookPath("mount"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `mount` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.MountPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `mount` path: %s, %s", c.MountPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `mount` binary is not executable: %s", c.MountPath)
		}
	}
	// Chown needed to properly set ownership for the unprivileged user on available resources
	if c.ChownPath == "" {
		if c.ChownPath, err = exec.LookPath("chown"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `chown` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.ChownPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `chown` path: %s, %s", c.ChownPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `chown` binary is not executable: %s", c.ChownPath)
		}
	}
	// Chmod needed to set additional read access for the unprivileged user on env metadata file
	if c.ChmodPath == "" {
		if c.ChmodPath, err = exec.LookPath("chmod"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `chmod` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.ChmodPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `chmod` path: %s, %s", c.ChmodPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `chmod` binary is not executable: %s", c.ChmodPath)
		}
	}
	// Killall is running to stop all the unprivileged user processes during de-allocation
	if c.KillallPath == "" {
		if c.KillallPath, err = exec.LookPath("killall"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `killall` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.KillallPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `killall` path: %s, %s", c.KillallPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `killall` binary is not executable: %s", c.KillallPath)
		}
	}
	// Rm allows to clean up the leftovers after the execution
	if c.RmPath == "" {
		if c.RmPath, err = exec.LookPath("rm"); err != nil {
			return nil, nil, fmt.Errorf("Native: Unable to locate `rm` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.RmPath); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("Native: Unable to locate `rm` path: %s, %s", c.RmPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, nil, fmt.Errorf("Native: `rm` binary is not executable: %s", c.RmPath)
		}
	}
	return err, nil, nil
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
