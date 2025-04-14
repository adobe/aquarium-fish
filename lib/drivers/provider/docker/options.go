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

	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Options for label definition
//
// Example:
//
//	images:
//	  - url: https://artifact-storage/aquarium/image/docker/ubuntu2004/ubuntu2004-VERSION.tar.xz
//	    sum: sha256:1234567890abcdef1234567890abcdef1
//	  - url: https://artifact-storage/aquarium/image/docker/ubuntu2004-python3/ubuntu2004-python3-VERSION.tar.xz
//	    sum: sha256:1234567890abcdef1234567890abcdef2
//	  - url: https://artifact-storage/aquarium/image/docker/ubuntu2004-python3-ci/ubuntu2004-python3-ci-VERSION.tar.xz
//	    sum: sha256:1234567890abcdef1234567890abcdef3
type Options struct {
	Images []provider.Image `json:"images"` // List of image dependencies, last one is running one
}

// Apply takes json and applies it to the options structure
func (o *Options) Apply(options util.UnparsedJSON) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		return log.Error("DOCKER: Unable to apply the driver options:", err)
	}

	return o.Validate()
}

// Validate makes sure the options have the required defaults & that the required fields are set
func (o *Options) Validate() error {
	// Check images
	var imgErr error
	for index := range o.Images {
		if err := o.Images[index].Validate(); err != nil {
			imgErr = log.Error("DOCKER: Error during image validation:", err)
		}
	}
	if imgErr != nil {
		return imgErr
	}

	return nil
}
