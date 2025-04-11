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

package proxyssh

import (
	"encoding/json"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
)

// Config - node driver configuration
type Config struct {
	// Where to listen for incoming SSH requests
	BindAddress string `json:"bind_address"`
	// Path to store the SSH key in pem format, if relative - will be stored in node workdir
	SSHKey string `json:"ssh_key"`
}

// Apply takes json and applies it to the config structure
func (c *Config) Apply(config []byte, db *database.Database) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("PROXYSSH: Unable to apply the gate config:", err)
		}
	}

	if c.BindAddress == "" {
		c.BindAddress = "0.0.0.0:1022"
	}

	if c.SSHKey == "" {
		c.SSHKey = db.GetNodeName() + "_id_ecdsa"
	}

	return nil
}

// Validate makes sure the config have the required defaults & that the required fields are set
func (*Config) Validate() (err error) {
	return nil
}
