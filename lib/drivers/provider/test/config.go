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

// Package test implements mock driver
package test

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Config - node driver configuration
type Config struct {
	IsRemote bool `json:"is_remote"` // Pretend to be remote or not to check the local node limits

	WorkspacePath string `json:"workspace_path"` // Where to place the files of running resources

	CPULimit uint `json:"cpu_limit"` // Number of available virtual CPUs, 0 - unlimited
	RAMLimit uint `json:"ram_limit"` // Amount of available virtual RAM (GB), 0 - unlimited

	CPUOverbook uint `json:"cpu_overbook"` // How many CPUs available for overbook
	RAMOverbook uint `json:"ram_overbook"` // How much RAM (GB) available for overbook

	FailConfigApply    uint8 `json:"fail_config_apply"`    // Fail on config Apply (0 - not, 1-254 random, 255-yes)
	FailConfigValidate uint8 `json:"fail_config_validate"` // Fail on config Validation (0 - not, 1-254 random, 255-yes)
	FailStatus         uint8 `json:"fail_status"`          // Fail on Status (0 - not, 1-254 random, 255-yes)
	FailSnapshot       uint8 `json:"fail_snapshot"`        // Fail on Snapshot (0 - not, 1-254 random, 255-yes)
	FailDeallocate     uint8 `json:"fail_deallocate"`      // Fail on Deallocate (0 - not, 1-254 random, 255-yes)
}

// Apply takes json and applies it to the config structure
func (c *Config) Apply(config []byte) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("TEST: Unable to apply the driver config:", err)
		}
	}

	return randomFail("ConfigApply", c.FailConfigApply)
}

// Validate makes sure the config have the required defaults & that the required fields are set
func (c *Config) Validate() (err error) {
	if c.WorkspacePath == "" {
		c.WorkspacePath = "fish_test_workspace"
	}
	if c.WorkspacePath, err = filepath.Abs(c.WorkspacePath); err != nil {
		return err
	}
	log.Debug("TEST: Creating working directory:", c.WorkspacePath)
	if err := os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}
	return randomFail("ConfigValidate", c.FailConfigValidate)
}
