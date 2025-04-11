/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package proxysocks

import (
	"encoding/json"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Config - node driver configuration
type Config struct {
	// Where to listen for incoming requests
	BindAddress string `json:"bind_address"`
}

// Apply takes json and applies it to the config structure
func (c *Config) Apply(config []byte) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("PROXYSOCKS: Unable to apply the gate config:", err)
		}
	}

	if c.BindAddress == "" {
		c.BindAddress = "0.0.0.0:1080"
	}

	return nil
}

// Validate makes sure the config have the required defaults & that the required fields are set
func (c *Config) Validate() (err error) {
	log.Warn("TODO: Verify binding is possible if BindAddress is set")

	return nil
}
