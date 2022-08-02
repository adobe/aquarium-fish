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

package aws

import (
	"encoding/json"
	"fmt"
	"log"
)

type Config struct {
	Region    string `json:"region"`     // AWS Region to connect to
	KeyID     string `json:"key_id"`     // AWS AMI Key ID
	SecretKey string `json:"secret_key"` // AWS AMI Secret Key
}

func (c *Config) Apply(config []byte) error {
	// Parse json
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			log.Println("AWS: Unable to apply the driver config", err)
			return err
		}
	}
	return nil
}

func (c *Config) Validate() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.Region == "" {
		return fmt.Errorf("AWS: No EC2 region is specified")
	}

	if c.KeyID == "" {
		return fmt.Errorf("AWS: Credentials Key ID is not set")
	}
	if c.SecretKey == "" {
		return fmt.Errorf("AWS: Credentials SecretKey is not set")
	}

	// TODO: Verify that connection is possible with those creds

	return nil
}
