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
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type Config struct {
	DockerPath string `json:"docker_path"` // '/Applications/Docker.app/Contents/Resources/bin/docker'

	ImagesPath    string `json:"images_path"`    // Where to look/store docker file images
	WorkspacePath string `json:"workspace_path"` // Where to place the disks

	DownloadUser     string `json:"download_user"`     // The user will be used in download operations
	DownloadPassword string `json:"download_password"` // The password will be used in download operations
}

func (c *Config) Apply(config []byte) error {
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			log.Println("DOCKER: Unable to apply the driver config", err)
			return err
		}
	}
	return nil
}

func (c *Config) Validate() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.DockerPath == "" {
		// Look in the PATH
		if c.DockerPath, err = exec.LookPath("docker"); err != nil {
			log.Println("DOCKER: Unable to locate `docker` path", err)
			return err
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

	log.Println("DOCKER: Creating working directories:", c.ImagesPath, c.WorkspacePath)
	if err := os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}

	return nil
}
