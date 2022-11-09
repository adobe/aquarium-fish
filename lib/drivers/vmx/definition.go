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
	"fmt"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
)

/**
 * Definition example:
 *   image: macos1015-xcode122-ci
 *   images:
 *     macos1015: https://artifact-storage/aquarium/image/vmx/macos1015-VERSION/macos1015-VERSION.tar.xz
 *     macos1015-xcode122: https://artifact-storage/aquarium/image/vmx/macos1015-xcode122-VERSION/macos1015-xcode122-VERSION.tar.xz
 *     macos1015-xcode122-ci: https://artifact-storage/aquarium/image/vmx/macos1015-xcode122-ci-VERSION/macos1015-xcode122-ci-VERSION.tar.xz
 *   requirements:
 *     cpu: 14
 *     ram: 14
 *     disks:
 *       xcode122_workspace:
 *         type: exfat
 *         size: 100
 *         reuse: true
 *     network: ""
 */
type Definition struct {
	Image  string            `json:"image"`  // Main image to use as reference
	Images map[string]string `json:"images"` // List of image dependencies

	Resources drivers.Resources `json:"resources"` // Required resources to allocate
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
		return fmt.Errorf("VMX: No image is specified")
	}

	// Check images
	image_exist := false
	for name, url := range d.Images {
		if name == "" {
			return fmt.Errorf("VMX: No image name is specified")
		}
		if url == "" {
			return fmt.Errorf("VMX: No image url is specified")
		}
		if name == d.Image {
			image_exist = true
		}
	}
	if !image_exist {
		return fmt.Errorf("VMX: No image found in the images")
	}

	// Check resources
	if err := d.Resources.Validate([]string{"hfs+", "exfat", "fat32"}, true); err != nil {
		return fmt.Errorf("VMX: Resources validation failed: %s", err)
	}

	return nil
}
