//go:build linux

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

package native

import (
	"os"
	"sync"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Common lock to properly acquire unique User ID
var userCreateLock sync.Mutex

func unpackForPlatform(user string, err error, imageArchive string, unpackPath string, d *Driver) (error, bool) {
	// Since the image is under Fish node control and user could have no read access to the file
	// it's a good idea to use stdin of the tar command to unpack properly.
	f, err := os.Open(imageArchive)
	if err != nil {
		return log.Error("Native: Unable to read the image:", imageArchive, err), true
	}
	log.Info("Native: Unpacking image:", user, imageArchive, unpackPath)
	_, _, err = runAndLog(5*time.Minute, f, d.cfg.SudoPath, "-n", d.cfg.TarPath, "-xf", "-", "--uname", user, "-C", unpackPath+"/")
	f.Close()
	if err != nil {
		return log.Error("Native: Unable to unpack the image:", imageArchive, err), true
	}
	return nil, false
}

func isEnvAllocated(user string) bool {
	_, err := os.Stat("/Users/" + user)
	return !os.IsNotExist(err)
}

// Create the new user to run workload from it's name
// Don't forget to deleteUser if operation fails
func userCreate(c *Config, groups []string) (user, homedir string, err error) {

	//TODO: Implement userCreate for linux

	return
}

// Runs the executable as defined user
func userRun(c *Config, envData *EnvData, user, entry string, metadata map[string]any) (err error) {

	//TODO: Implement userRun for linux

	return err
}

// Stop the user processes
func userStop(c *Config, user string) (outErr error) { //nolint:unparam

	//TODO: Implement userStop for linux

	return
}

// Delete user and clean up
func userDelete(c *Config, user string) (outErr error) {

	//TODO: Implement userDelete for linux

	return
}

// Unmount user volumes and delete the disk files
func disksDelete(c *Config, user string) (outErr error) {

	//TODO: Implement disksDelete for linux

	return
}

// Creates disks directories described by the disks map, returns the map of disks to mount paths
func (d *Driver) disksCreate(user string, disks map[string]types.ResourcesDisk) (map[string]string, error) {
	// Create disks
	diskPaths := make(map[string]string, len(disks))

	//TODO: Implement disksCreate for linux

	return diskPaths, nil
}
