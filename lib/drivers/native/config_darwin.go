//go:build darwin

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
)

// Config - node driver configuration
type PlatformConfig struct {

	// Embed shared posix items
	PosixConfig

	// macos specific elements
	DsclPath          string `json:"dscl_path"`          // Path to the dscl (macos user control) binary
	HdiutilPath       string `json:"hdiutil_path"`       // Path to the hdiutil (macos images create/mount/umount) binary
	MdutilPath        string `json:"mdutil_path"`        // Path to the mdutil (macos disable indexing for disks) binary
	CreatehomedirPath string `json:"createhomedir_path"` // Path to the createhomedir (macos create/prefill user directory) binary

}

func (c *Config) validateForPlatform(err error) (error, error) {
	err, err2, err3 := c.validatePosix(err)
	if err3 != nil {
		return err2, err3
	}

	// MacOS specific ones:
	// Dscl creates/removes the unprevileged user
	if c.DsclPath == "" {
		if c.DsclPath, err = exec.LookPath("dscl"); err != nil {
			return nil, fmt.Errorf("Native: Unable to locate macos `dscl` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.DsclPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Native: Unable to locate macos `dscl` path: %s, %s", c.DsclPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("Native: macos `dscl` binary is not executable: %s", c.DsclPath)
		}
	}
	// Hdiutil allows to create disk images and mount them to restrict user by disk space
	if c.HdiutilPath == "" {
		if c.HdiutilPath, err = exec.LookPath("hdiutil"); err != nil {
			return nil, fmt.Errorf("Native: Unable to locate macos `hdiutil` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.HdiutilPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Native: Unable to locate macos `hdiutil` path: %s, %s", c.HdiutilPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("Native: macos `hdiutil` binary is not executable: %s", c.HdiutilPath)
		}
	}
	// Mdutil allows to disable the indexing for mounted volume
	if c.MdutilPath == "" {
		if c.MdutilPath, err = exec.LookPath("mdutil"); err != nil {
			return nil, fmt.Errorf("Native: Unable to locate macos `mdutil` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.MdutilPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Native: Unable to locate macos `mdutil` path: %s, %s", c.MdutilPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("Native: macos `mdutil` binary is not executable: %s", c.MdutilPath)
		}
	}
	// Createhomedir creates unprevileged user home directory and fulfills with default subdirs
	if c.CreatehomedirPath == "" {
		if c.CreatehomedirPath, err = exec.LookPath("createhomedir"); err != nil {
			return nil, fmt.Errorf("Native: Unable to locate macos `createhomedir` path: %s", err)
		}
	} else {
		if info, err := os.Stat(c.CreatehomedirPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Native: Unable to locate macos `createhomedir` path: %s, %s", c.CreatehomedirPath, err)
		} else if info.Mode()&0o111 == 0 {
			return nil, fmt.Errorf("Native: macos `createhomedir` binary is not executable: %s", c.CreatehomedirPath)
		}
	}

	return err, nil
}
