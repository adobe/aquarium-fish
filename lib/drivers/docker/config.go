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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/adobe/aquarium-fish/lib/log"
)

type Config struct {
	DockerPath string `json:"docker_path"` // '/Applications/Docker.app/Contents/Resources/bin/docker'

	IsRemote bool `json:"is_remote"` // In case the docker client does not use the local node resources

	ImagesPath    string `json:"images_path"`    // Where to look/store docker file images
	WorkspacePath string `json:"workspace_path"` // Where to place the disks

	// Alter allows you to control how much resources will be used:
	// * Negative (<0) value will alter the total resource count before provisioning so you will be
	//   able to save some resources for the host system (recommended -2 for CPU and -10 for RAM
	//   for disk caching)
	// * Positive (>0) value could also be available (but check it in your docker dist in advance)
	//   Please be careful here - noone wants the container to fail allocation because of that...
	CpuAlter int `json:"cpu_alter"` // 0 do nothing, <0 reduces number available CPUs, >0 increases it (dangerous)
	RamAlter int `json:"ram_alter"` // 0 do nothing, <0 reduces amount of available RAM (GB), >0 increases it (dangerous)

	// Overbook options allows tenants to reuse the resources
	// It will be used only when overbook is allowed by the tenants. It works by just adding those
	// amounts to the existing total before checking availability. For example if you have 16CPU
	// and want to run 2 tenants with requirement of 14 CPUs each - you can put 12 in CpuOverbook -
	// to have virtually 28 CPUs. 3rd will not be running because 2 tenants will eat all 28 virtual
	// CPUs. Same applies to the RamOverbook.
	CpuOverbook uint `json:"cpu_overbook"` // How much CPUs could be reused by multiple tenants
	RamOverbook uint `json:"ram_overbook"` // How much RAM (GB) could be reused by multiple tenants

	DownloadUser     string `json:"download_user"`     // The user will be used in download operations
	DownloadPassword string `json:"download_password"` // The password will be used in download operations
}

func (c *Config) Apply(config []byte) error {
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			return log.Error("DOCKER: Unable to apply the driver config", err)
		}
	}
	return nil
}

func (c *Config) Validate() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.DockerPath == "" {
		// Look in the PATH
		if c.DockerPath, err = exec.LookPath("docker"); err != nil {
			return log.Error("DOCKER: Unable to locate `docker` path", err)
		}
	}

	if c.ImagesPath == "" {
		c.ImagesPath = "fish_docker_images"
	}
	if c.WorkspacePath == "" {
		c.WorkspacePath = "fish_docker_workspace"
	}

	// Making paths absolute
	if c.ImagesPath, err = filepath.Abs(c.ImagesPath); err != nil {
		return err
	}
	if c.WorkspacePath, err = filepath.Abs(c.WorkspacePath); err != nil {
		return err
	}

	log.Info("DOCKER: Creating working directories:", c.ImagesPath, c.WorkspacePath)
	if err := os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}

	return nil
}
