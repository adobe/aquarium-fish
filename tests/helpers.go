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

package tests

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var fish_path = os.Getenv("FISH_PATH") // Full path to the aquarium-fish binary

// Saves state of the running Aquarium Fish for particular test
type AFInstance struct {
	workspace string
	fishStop  context.CancelFunc

	api_address string
	admin_token string
}

func RunAquariumFish(t *testing.T, cfg string) *AFInstance {
	afi := &AFInstance{}

	afi.workspace = t.TempDir()
	t.Log("INFO: Created workspace:", afi.workspace)

	os.WriteFile(filepath.Join(afi.workspace, "config.yml"), []byte(cfg), 0644)
	t.Log("INFO: Stored config:", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	afi.fishStop = cancel

	cmd := exec.CommandContext(ctx, fish_path, "-c", filepath.Join(afi.workspace, "config.yml"))
	cmd.Dir = afi.workspace
	r, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout

	init_done := make(chan struct{})
	scanner := bufio.NewScanner(r)
	go func() {
		// Listening for log and scan for token and address
		for scanner.Scan() {
			line := scanner.Text()
			t.Log(line)
			if afi.admin_token == "" || afi.api_address == "" {
				if strings.HasPrefix(line, "Admin user pass: ") {
					val := strings.SplitN(strings.TrimSpace(line), "Admin user pass: ", 2)
					if len(val) < 2 {
						panic("ERROR: No token after 'Admin user pass: '")
					}
					afi.admin_token = val[1]
				}
				if strings.HasPrefix(line, "API listening on: ") {
					val := strings.SplitN(strings.TrimSpace(line), "API listening on: ", 2)
					if len(val) < 2 {
						panic("ERROR: No address after 'API listening on: '")
					}
					afi.api_address = val[1]
				}
				if afi.admin_token != "" && afi.api_address != "" {
					init_done <- struct{}{}
				}
			}
		}
	}()

	go func() {
		if err := cmd.Run(); err != nil {
			t.Log("AquariumFish process was stopped:", err)
		}
	}()

	<-init_done

	return afi
}

// Will return url to access API of AquariumFish
func (afi *AFInstance) ApiAddress(path string) string {
	return fmt.Sprintf("https://%s/%s", afi.api_address, path)
}

// Returns admin token
func (afi *AFInstance) AdminToken() string {
	return afi.admin_token
}

// Cleanup after the test execution
func (afi *AFInstance) Cleanup() error {
	log.Println("INFO: Cleaning up:", afi.workspace)
	afi.fishStop()
	os.RemoveAll(afi.workspace)
	return nil
}
