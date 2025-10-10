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

package docker

import (
	"encoding/json"
	"fmt"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Options for label definition
type Options struct {
	// TaskImage options
	TaskImageName string `json:"task_image_name"` // Create new image with defined name and "image-DATE.TIME" version
}

// Apply takes json and applies it to the options structure
func (o *Options) Apply(options util.UnparsedJSON) error {
	if err := json.Unmarshal([]byte(options), o); err != nil {
		log.WithFunc("docker", "Apply").Error("Unable to apply the driver options", "err", err)
		return fmt.Errorf("DOCKER: Unable to apply the driver options: %v", err)
	}

	return o.Validate()
}

// Validate makes sure the options have the required defaults & that the required fields are set
func (o *Options) Validate() error {
	return nil
}
