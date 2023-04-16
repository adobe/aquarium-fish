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

package docker

import (
	"encoding/json"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

/**
 * Options example:
 *   images:
 *     - url: https://artifact-storage/aquarium/image/docker/ubuntu2004/ubuntu2004-VERSION.tar.xz
 *       sum: sha256:1234567890abcdef1234567890abcdef1
 *     - url: https://artifact-storage/aquarium/image/docker/ubuntu2004-python3/ubuntu2004-python3-VERSION.tar.xz
 *       sum: sha256:1234567890abcdef1234567890abcdef2
 *     - url: https://artifact-storage/aquarium/image/docker/ubuntu2004-python3-ci/ubuntu2004-python3-ci-VERSION.tar.xz
 *       sum: sha256:1234567890abcdef1234567890abcdef3
 */
type Options struct {
	Images []drivers.Image `json:"images"` // List of image dependencies, last one is running one
}

func (o *Options) Apply(options util.UnparsedJson) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		return log.Error("Docker: Unable to apply the driver options:", err)
	}

	return o.Validate()
}

func (o *Options) Validate() error {
	// Check images
	var img_err error
	for index, _ := range o.Images {
		if err := o.Images[index].Validate(); err != nil {
			img_err = log.Error("Docker: Error during image validation:", err)
		}
	}
	if img_err != nil {
		return img_err
	}

	return nil
}
