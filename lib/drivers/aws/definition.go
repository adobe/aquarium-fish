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

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/util"
)

/**
 * Definition example:
 *   image: ami-abcdef123456
 *   instance_type: c6a.4xlarge
 *   security_group: sg-abcdef123456
 *   tags:
 *     somekey: somevalue
 *   requirements:
 *     cpu: 16
 *     ram: 32
 *     disks:
 *       /dev/sdb:
 *         clone: snap-abcdef123456
 *     network: vpc-abcdef123456
 */
type Definition struct {
	Image         string            `json:"image"`          // ID/Name of the image to use
	InstanceType  string            `json:"instance_type"`  // Type of the instance from aws available list
	SecurityGroup string            `json:"security_group"` // ID/Name of the security group to use for the instance
	Tags          map[string]string `json:"tags"`           // Tags to add during instance creation
	EncryptKey    string            `json:"encrypt_key"`    // Use specific encryption key for the new disks

	UserDataFormat string `json:"userdata_format"` // If not empty - will store the resource metadata to userdata in defined format
	UserDataPrefix string `json:"userdata_prefix"` // Optional if need to add custom prefix to the metadata key during formatting

	Resources drivers.Resources `json:"resources"` // Required resources to allocate, disk clone & net could use tags
}

func (d *Definition) Apply(definition string) error {
	if err := json.Unmarshal([]byte(definition), d); err != nil {
		log.Println("AWS: Unable to apply the driver definition", err)
		return err
	}

	return d.Validate()
}

func (d *Definition) Validate() error {
	// Check image
	if d.Image == "" {
		return fmt.Errorf("AWS: No EC2 image is specified")
	}

	// Check instance type
	if d.InstanceType == "" {
		return fmt.Errorf("AWS: No EC2 instance type is specified")
	}

	if !util.Contains([]string{"", "json", "env", "ps1"}, d.UserDataFormat) {
		return fmt.Errorf("AWS: Unsupported userdata format: %s", d.UserDataFormat)
	}

	// Check resources (no disk types supported and no net check)
	if err := d.Resources.Validate([]string{}, false); err != nil {
		return fmt.Errorf("AWS: Resources validation failed: %s", err)
	}

	return nil
}
