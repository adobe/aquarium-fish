//go:build windows

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

// Package native implements driver
package native

import (
	"os"
	"path/filepath"
)

// Config - node driver configuration
type PlatformConfig struct {
	//TODO: Add windows specific config items
}

func (c *Config) validateForPlatform(err error) (error, error) {
	//TODO: implement windows validation
	return err, nil

}

// Will create the config test script to run
func testScriptCreate(user string) (tempFile string, err error) {
	tempDir, err := os.MkdirTemp("", "aquarium")
	if err != nil {

	}
	tempFile = filepath.Join(tempDir, user+"-init.ps1")
	script := []byte("whoami")
	return tempFile, os.WriteFile(tempFile, script, 0o644)
}

// Will delete the config test script
func testScriptDelete(path string) error {
	return os.Remove(path)
}
