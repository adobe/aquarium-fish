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

	"github.com/adobe/aquarium-fish/lib/util"
)

/**
 * Options example:
 *   image: macos1015-xcode122-ci
 *   images:
 *     macos1015: https://artifact-storage/aquarium/image/vmx/macos1015-VERSION/macos1015-VERSION.tar.xz
 *     macos1015-xcode122: https://artifact-storage/aquarium/image/vmx/macos1015-xcode122-VERSION/macos1015-xcode122-VERSION.tar.xz
 *     macos1015-xcode122-ci: https://artifact-storage/aquarium/image/vmx/macos1015-xcode122-ci-VERSION/macos1015-xcode122-ci-VERSION.tar.xz
 */
type Options struct {
	Image  string            `json:"image"`  // Main image to use as reference
	Images map[string]string `json:"images"` // List of image dependencies
}

func (o *Options) Apply(options util.UnparsedJson) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		log.Println("VMX: Unable to apply the driver options", err)
		return err
	}

	return o.Validate()
}

func (o *Options) Validate() error {
	// Check image
	if o.Image == "" {
		return fmt.Errorf("VMX: No image is specified")
	}

	// Check images
	image_exist := false
	for name, url := range o.Images {
		if name == "" {
			return fmt.Errorf("VMX: No image name is specified")
		}
		if url == "" {
			return fmt.Errorf("VMX: No image url is specified")
		}
		if name == o.Image {
			image_exist = true
		}
	}
	if !image_exist {
		return fmt.Errorf("VMX: No image found in the images")
	}

	return nil
}
