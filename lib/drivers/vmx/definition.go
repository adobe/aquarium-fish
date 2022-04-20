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

package vmx

import (
	"encoding/json"
	"errors"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
)

/**
 * Definition example:
 *   image: macos-1015-ci-xcode122
 *   images:
 *     macos-1015: https://artifact-storage/aquarium/image/macos-1015-VERSION/macos-1015-VERSION.tar.xz
 *     macos-1015-ci: https://artifact-storage/aquarium/image/macos-1015-ci-VERSION/macos-1015-ci-VERSION.tar.xz
 *     macos-1015-ci-xcode122: https://artifact-storage/aquarium/image/macos-1015-ci-xcode122-VERSION/macos-1015-ci-xcode122-VERSION.tar.xz
 *   requirements:
 *     cpu: 14
 *     ram: 14
 *     disks:
 *       xcode122_workspace:
 *         type: exfat
 *         size: 100
 *         reuse: true
 *     network: ""
 *   metadata:
 *     JENKINS_AGENT_WORKDIR: /Users/jenkins/workdir
 */
type Definition struct {
	Image        string               `json:"image"`        // Main image to use as reference
	Images       map[string]string    `json:"images"`       // List of image dependencies
	Requirements drivers.Requirements `json:"requirements"` // Required resources to allocate
}

func (d *Definition) Apply(definition string) error {
	if err := json.Unmarshal([]byte(definition), d); err != nil {
		log.Println("VMX: Unable to apply the driver definition", err)
		return err
	}

	return d.Validate()
}

func (d *Definition) Validate() error {
	// Check image
	if d.Image == "" {
		return errors.New("VMX: No image is specified")
	}

	// Check images
	image_exist := false
	for name, url := range d.Images {
		if name == "" {
			return errors.New("VMX: No image name is specified")
		}
		if url == "" {
			return errors.New("VMX: No image url is specified")
		}
		if name == d.Image {
			image_exist = true
		}
	}
	if !image_exist {
		return errors.New("VMX: No image found in the images")
	}

	// Check resources
	if d.Requirements.Validate() != nil {
		return errors.New("VMX: Requirements validation failed")
	}

	return nil
}
