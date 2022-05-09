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
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type Config struct {
	VmrunPath        string `json:"vmrun_path"`        // '/Applications/VMware Fusion.app/Contents/Library/vmrun'
	VdiskmanagerPath string `json:"vdiskmanager_path"` // '/Applications/VMware Fusion.app/Contents/Library/vmware-vdiskmanager'

	ImagesPath    string `json:"images_path"`    // Where to look/store VM images
	WorkspacePath string `json:"workspace_path"` // Where to place the cloned VM and disks

	DownloadUser     string `json:"download_user"`     // The user will be used in download operations
	DownloadPassword string `json:"download_password"` // The password will be used in download operations
}

func (c *Config) Apply(config []byte) error {
	if len(config) > 0 {
		if err := json.Unmarshal(config, c); err != nil {
			log.Println("VMX: Unable to apply the driver config", err)
			return err
		}
	}
	return nil
}

func (c *Config) Validate() (err error) {
	// Check that values of the config is filled at least with defaults
	if c.VmrunPath == "" {
		// Look in the PATH
		if c.VmrunPath, err = exec.LookPath("vmrun"); err != nil {
			log.Println("VMX: Unable to locate `vmrun` path", err)
			return err
		}
	}
	if c.VdiskmanagerPath == "" {
		// Use VmrunPath to get the path
		c.VdiskmanagerPath = filepath.Join(filepath.Dir(filepath.Dir(c.VmrunPath)), "Library", "vmware-vdiskmanager")
		if _, err := os.Stat(c.VdiskmanagerPath); os.IsNotExist(err) {
			log.Println("VMX: Unable to locate `vmware-vdiskmanager` path", err)
			return err
		}
	}
	if c.ImagesPath == "" {
		c.ImagesPath = "fish_vmx_images"
	}
	if c.WorkspacePath == "" {
		c.WorkspacePath = "fish_vmx_workspace"
	}

	// Making paths absolute
	if c.ImagesPath, err = filepath.Abs(c.ImagesPath); err != nil {
		return err
	}
	if c.WorkspacePath, err = filepath.Abs(c.WorkspacePath); err != nil {
		return err
	}

	log.Println("VMX: Creating working directories:", c.ImagesPath, c.WorkspacePath)
	if err := os.MkdirAll(c.ImagesPath, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(c.WorkspacePath, 0o750); err != nil {
		return err
	}

	return nil
}
