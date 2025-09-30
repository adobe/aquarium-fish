//go:build linux

/**
 * Copyright 2025 Adobe. All rights reserved.
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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

// validateSpec util for Linux
func (c *Config) validateSpec() (err error) {
	if c.DdPath == "" {
		// Look in the PATH
		if c.DdPath, err = exec.LookPath("dd"); err != nil {
			return fmt.Errorf("DOCKER: Unable to locate `dd` path: %v", err)
		}
	}
	if c.MkfsExt4Path == "" {
		// Look in the PATH
		if c.MkfsExt4Path, err = exec.LookPath("mkfs.ext4"); err != nil {
			return fmt.Errorf("DOCKER: Unable to locate `mkfs.ext4` path: %v", err)
		}
	}
	return nil
}

// diskCreateSpec to create disks on Linux
func (d *Driver) diskCreateSpec(disk typesv2.ResourcesDisk, diskPath string) (string, error) {
	imgPath := diskPath + ".img"

	// Do not recreate the disk if it is exists
	if _, err := os.Stat(imgPath); !os.IsNotExist(err) {
		return imgPath, nil
	}

	// Creating file for the image
	args := []string{
		"if=/dev/zero",
		fmt.Sprintf("of=%s", imgPath),
		"bs=1G",
		fmt.Sprintf("count=%d", disk.Size),
	}
	if _, _, err := util.RunAndLog("docker", 10*time.Minute, nil, d.cfg.DdPath, args...); err != nil {
		return imgPath, fmt.Errorf("DOCKER: %s: Unable to create disk file %q: %v", d.name, imgPath, err)
	}

	// Formatting filesystem
	if _, _, err := util.RunAndLog("docker", 10*time.Minute, nil, d.cfg.MkfsExt4Path, "-L", disk.Label, imgPath); err != nil {
		return imgPath, fmt.Errorf("DOCKER: %s: Unable to create img disk %q: %v", d.name, imgPath, err)
	}

	return imgPath, nil
}

// diskMountSpec to mount disk on Linux
func (d *Driver) diskMountSpec(diskPath, name string) (string, error) {
	mountPoint := filepath.Join("/mnt", name)

	// Attach & mount disk
	if _, _, err := util.RunAndLog("docker", 10*time.Second, nil, d.cfg.MountPath, diskPath, mountPoint); err != nil {
		return mountPoint, fmt.Errorf("DOCKER: %s: Unable to mount disk %q to %q: %v", d.name, diskPath, mountPoint, err)
	}

	// Allow anyone to modify the disk content
	if err := os.Chmod(mountPoint, 0o777); err != nil {
		return mountPoint, fmt.Errorf("DOCKER: %s: Unable to change the mount point %q access rights: %v", d.name, mountPoint, err)
	}

	return mountPoint, nil
}
