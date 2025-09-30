//go:build darwin

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

// validateSpec util for MacOS
func (c *Config) validateSpec() (err error) {
	if c.HdiutilPath == "" {
		// Look in the PATH
		if c.HdiutilPath, err = exec.LookPath("hdiutil"); err != nil {
			return fmt.Errorf("DOCKER: Unable to locate `hdiutil` path: %v", err)
		}
	}
	return nil
}

// diskCreateSpec to create disks on MacOS
func (d *Driver) diskCreateSpec(disk typesv2.ResourcesDisk, diskPath string) (string, error) {
	dmgPath := diskPath + ".dmg"

	// Do not recreate the disk if it is exists
	if _, err := os.Stat(dmgPath); !os.IsNotExist(err) {
		return dmgPath, nil
	}

	var diskType string
	switch disk.Type {
	case "hfs+":
		diskType = "HFS+"
	case "fat32":
		diskType = "FAT32"
	default:
		diskType = "ExFAT"
	}

	args := []string{
		"create", dmgPath,
		"-fs", diskType,
		"-layout", "NONE",
		"-volname", disk.Label,
		"-size", fmt.Sprintf("%dm", disk.Size*1024),
	}
	if _, _, err := util.RunAndLog("docker", 10*time.Minute, nil, d.cfg.HdiutilPath, args...); err != nil {
		return dmgPath, fmt.Errorf("DOCKER: %s: Unable to create dmg disk %q: %v", d.name, dmgPath, err)
	}
	return dmgPath, nil
}

// diskMountSpec to attach & mount disk on MacOS
func (d *Driver) diskMountSpec(diskPath, name string) (string, error) {
	mountPoint := filepath.Join("/Volumes", name)

	// Attach & mount disk
	if _, _, err := util.RunAndLog("docker", 10*time.Second, nil, d.cfg.HdiutilPath, "attach", diskPath, "-mountpoint", mountPoint); err != nil {
		return mountPoint, fmt.Errorf("DOCKER: %s: Unable to attach dmg disk %q to %q: %v", d.name, diskPath, mountPoint, err)
	}

	// Allow anyone to modify the disk content
	if err := os.Chmod(mountPoint, 0o777); err != nil {
		return mountPoint, fmt.Errorf("DOCKER: %s: Unable to change the mount point %q access rights: %v", d.name, mountPoint, err)
	}
	return mountPoint, nil
}
