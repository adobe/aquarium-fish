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

package aws

import (
	"encoding/json"
	"fmt"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Options for label definition
//
// Example:
//
//	image: ami-abcdef123456
//	instance_type: c6a.4xlarge
//	security_group: sg-abcdef123456
//	tags:
//	  somekey: somevalue
type Options struct {
	Image          string            `json:"image"`           // ID/Name of the image you want to use (name that contains * is usually a bad idea for reproducibility)
	InstanceType   string            `json:"instance_type"`   // Type of the instance from aws available list
	SecurityGroups []string          `json:"security_groups"` // IDs/Names of the security groups to use for the instance
	SecurityGroup  string            `json:"security_group"`  // ID/Name of the security group to use for the instance (DEPRECATED)
	Tags           map[string]string `json:"tags"`            // Tags to add during instance creation
	EncryptKey     string            `json:"encrypt_key"`     // Use specific encryption key for the new disks
	Pool           string            `json:"pool"`            // Use machine from dedicated pool, otherwise will try to use one with auto-placement

	UserDataFormat string `json:"userdata_format"` // If not empty - will store the resource metadata to userdata in defined format
	UserDataPrefix string `json:"userdata_prefix"` // Optional if need to add custom prefix to the metadata key during formatting

	// TaskImage options
	TaskImageName       string `json:"task_image_name"`        // Create new image with defined name + "-DATE.TIME" suffix
	TaskImageEncryptKey string `json:"task_image_encrypt_key"` // KMS Key ID or Alias in format "alias/<name>" if need to re-encrypt the newly created AMI snapshots
}

// Apply takes json and applies it to the options structure
func (o *Options) Apply(options util.UnparsedJSON) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		log.Error().Msgf("AWS: Unable to apply the driver options: %v", err)
		return fmt.Errorf("AWS: Unable to apply the driver options: %v", err)
	}

	return o.Validate()
}

// Validate makes sure the options have the required defaults & that the required fields are set
func (o *Options) Validate() error {
	// Check image
	if o.Image == "" {
		return fmt.Errorf("AWS: No EC2 image is specified")
	}

	// Check instance type
	if o.InstanceType == "" {
		return fmt.Errorf("AWS: No EC2 instance type is specified")
	}

	if !util.Contains([]string{"", "json", "env", "ps1"}, o.UserDataFormat) {
		return fmt.Errorf("AWS: Unsupported userdata format: %s", o.UserDataFormat)
	}

	return nil
}
