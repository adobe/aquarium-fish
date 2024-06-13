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
	running   bool

	api_address string
	admin_token string
}

func RunAquariumFish(t *testing.T, cfg string) *AFInstance {
	afi := &AFInstance{}

	afi.workspace = t.TempDir()
	t.Log("INFO: Created workspace:", afi.workspace)

	os.WriteFile(filepath.Join(afi.workspace, "config.yml"), []byte(cfg), 0644)
	t.Log("INFO: Stored config:", cfg)

	afi.fishStart(t)

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

// Check the fish instance is running
func (afi *AFInstance) IsRunning() bool {
	return afi.running
}

// Restart the application
func (afi *AFInstance) Restart(t *testing.T) error {
	t.Log("INFO: Restarting:", afi.workspace)
	afi.fishStop()
	afi.fishStart(t)
	return nil
}

// Cleanup after the test execution
func (afi *AFInstance) Cleanup(t *testing.T) error {
	t.Log("INFO: Cleaning up:", afi.workspace)
	afi.fishStop()
	os.RemoveAll(afi.workspace)
	return nil
}

func (afi *AFInstance) fishStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	afi.fishStop = cancel

	cmd := exec.CommandContext(ctx, fish_path, "-v", "debug", "-c", filepath.Join(afi.workspace, "config.yml"))
	cmd.Dir = afi.workspace
	r, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout

	init_done := make(chan string)
	scanner := bufio.NewScanner(r)
	go func() {
		// Listening for log and scan for token and address
		for scanner.Scan() {
			line := scanner.Text()
			t.Log(line)
			if strings.HasPrefix(line, "Admin user pass: ") {
				val := strings.SplitN(strings.TrimSpace(line), "Admin user pass: ", 2)
				if len(val) < 2 {
					init_done <- "ERROR: No token after 'Admin user pass: '"
					break
				}
				afi.admin_token = val[1]
			}
			if strings.Contains(line, "API listening on: ") {
				val := strings.SplitN(strings.TrimSpace(line), "API listening on: ", 2)
				if len(val) < 2 {
					init_done <- "ERROR: No address after 'API listening on: '"
					break
				}
				afi.api_address = val[1]
			}
			if strings.HasSuffix(line, "Fish initialized") {
				// Found the needed values and continue to process to print the fish output for
				// test debugging purposes
				init_done <- ""
			}
		}
		t.Log("Reading of AquariumFish output is done")
	}()

	go func() {
		afi.running = true
		if err := cmd.Run(); err != nil {
			t.Log("AquariumFish process was stopped:", err)
			init_done <- fmt.Sprintf("ERROR: Fish was stopped with exit code: %v", err)
		}
		afi.running = false
		r.Close()
	}()

	failed := <-init_done

	if failed != "" {
		t.Fatalf(failed)
	}
}
