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
	"fmt"
	"log"

	"github.com/adobe/aquarium-fish/lib/drivers"
)

/**
 * Definition example:
 *   image: ubuntu2004-python3-ci
 *   images:
 *     ubuntu2004: https://artifact-storage/aquarium/image/docker/ubuntu2004/ubuntu2004-VERSION.tar.xz
 *     ubuntu2004-python3: https://artifact-storage/aquarium/image/docker/ubuntu2004-python3/ubuntu2004-python3-VERSION.tar.xz
 *     ubuntu2004-python3-ci: https://artifact-storage/aquarium/image/docker/ubuntu2004-python3-ci/ubuntu2004-python3-ci-VERSION.tar.xz
 *   requirements:
 *     cpu: 14
 *     ram: 14
 *     disks:
 *       python3_workspace:
 *         label: workspace
 *		   type: dir
 *         size: 100
 *         reuse: true
 *     network: ""
 *   metadata:
 *     JENKINS_AGENT_WORKDIR: /mnt/workspace
 */
type Definition struct {
	Image        string               `json:"image"`        // Image name to use
	Images       map[string]string    `json:"images"`       // List of image dependencies
	Requirements drivers.Requirements `json:"requirements"` // Required resources to allocate
}

func (d *Definition) Apply(definition string) error {
	if err := json.Unmarshal([]byte(definition), d); err != nil {
		log.Println("DOCKER: Unable to apply the driver definition", err)
		return err
	}

	return d.Validate()
}

func (d *Definition) Validate() error {
	// Check image
	if d.Image == "" {
		return fmt.Errorf("DOCKER: No image is specified")
	}

	// Check the images
	image_exist := false
	for name, url := range d.Images {
		if name == "" {
			return fmt.Errorf("DOCKER: No image name is specified")
		}
		if url == "" {
			return fmt.Errorf("DOCKER: No image url is specified")
		}
		if name == d.Image {
			image_exist = true
		}
	}
	if !image_exist {
		return fmt.Errorf("DOCKER: No image found in the images")
	}

	// Check resources
	if err := d.Requirements.Validate([]string{"dir", "hfs+", "exfat", "fat32"}, true); err != nil {
		return fmt.Errorf("DOCKER: Requirements validation failed: %s", err)
	}

	return nil
}
