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

package none

import (
	"encoding/json"
	"log"
)

type Config struct {
	FailConfigApply    uint8 `json:"fail_config_apply"`    // Fail on config Apply (0 - not, 1-254 random, 255-yes)
	FailConfigValidate uint8 `json:"fail_config_validate"` // Fail on config Validation (0 - not, 1-254 random, 255-yes)
	FailStatus         uint8 `json:"fail_status"`          // Fail on Status (0 - not, 1-254 random, 255-yes)
	FailSnapshot       uint8 `json:"fail_snapshot"`        // Fail on Snapshot (0 - not, 1-254 random, 255-yes)
	FailDeallocate     uint8 `json:"fail_deallocate"`      // Fail on Deallocate (0 - not, 1-254 random, 255-yes)
	ResourcesLimit     int   `json:"resources_limit"`      // How many resources available in the driver (0 unlimited, -1 none)
}

func (c *Config) Apply(config []byte) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			log.Println("NONE: Unable to apply the driver config", err)
			return err
		}
	}

	return RandomFail("ConfigApply", c.FailConfigApply)
}

func (c *Config) Validate() (err error) {
	return RandomFail("ConfigValidate", c.FailConfigValidate)
}
