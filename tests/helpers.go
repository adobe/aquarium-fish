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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fish_path = os.Getenv("FISH_PATH") // Full path to the aquarium-fish binary

// Saves state of the running Aquarium Fish for particular test
type afInstance struct {
	workspace string
	fishKill  context.CancelFunc
	running   bool
	cmd       *exec.Cmd

	node_name   string
	api_address string
	admin_token string
}

// Simple creates and run the fish node
func NewAquariumFish(t *testing.T, name, cfg string, args ...string) *afInstance {
	t.Log("INFO: Creating new node")
	afi := &afInstance{
		node_name: name,
	}

	afi.workspace = t.TempDir()
	t.Log("INFO: Created workspace:", afi.node_name, afi.workspace)

	cfg += fmt.Sprintf("\nnode_name: %q", afi.node_name)
	os.WriteFile(filepath.Join(afi.workspace, "config.yml"), []byte(cfg), 0644)
	t.Log("INFO: Stored config:", cfg)

	afi.Start(t, args...)

	return afi
}

// Will return url to access API of AquariumFish
func (afi *afInstance) ApiAddress(path string) string {
	return fmt.Sprintf("https://%s/%s", afi.api_address, path)
}

// Returns admin token
func (afi *afInstance) AdminToken() string {
	return afi.admin_token
}

// Check the fish instance is running
func (afi *afInstance) IsRunning() bool {
	return afi.running
}

// Restart the application
func (afi *afInstance) Restart(t *testing.T, args ...string) {
	t.Log("INFO: Restarting:", afi.node_name, afi.workspace)
	afi.Stop(t)
	afi.Start(t, args...)
}

// Start another node of cluster
// It will automatically add cluster_join parameter to the config
func (afi1 *afInstance) NewClusterNode(t *testing.T, name, cfg string, args ...string) *afInstance {
	t.Log("INFO: Creating new cluster node with seed:", afi1.api_address)
	afi2 := &afInstance{
		node_name: name,
	}

	afi2.workspace = t.TempDir()
	t.Log("INFO: Created workspace:", afi2.node_name, afi2.workspace)

	cfg += fmt.Sprintf("\nnode_name: %q", afi2.node_name)
	cfg += fmt.Sprintf("\ncluster_join: [%q]", afi1.api_address)
	os.WriteFile(filepath.Join(afi2.workspace, "config.yml"), []byte(cfg), 0644)
	t.Log("INFO: Stored config:", cfg)

	// Copy seed node CA to generate valid cluster node cert
	if err := copyFile(filepath.Join(afi1.workspace, "fish_data", "ca.key"), filepath.Join(afi2.workspace, "fish_data", "ca.key")); err != nil {
		t.Fatalf("Unable to copy CA key: %v", err)
	}
	if err := copyFile(filepath.Join(afi1.workspace, "fish_data", "ca.crt"), filepath.Join(afi2.workspace, "fish_data", "ca.crt")); err != nil {
		t.Fatalf("Unable to copy CA crt: %v", err)
	}

	afi2.Start(t, args...)

	return afi2
}

// Cleanup after the test execution
func (afi *afInstance) Cleanup(t *testing.T) {
	t.Log("INFO: Cleaning up:", afi.node_name, afi.workspace)
	afi.Stop(t)
	os.RemoveAll(afi.workspace)
}

// Stops the fish node executable
func (afi *afInstance) Stop(t *testing.T) {
	if afi.cmd == nil || !afi.running {
		return
	}
	// Send interrupt signal
	afi.cmd.Process.Signal(os.Interrupt)

	// Wait 10 seconds for process to stop
	t.Log("INFO: Wait 10s for fish node to stop:", afi.node_name, afi.workspace)
	for i := 1; i < 20; i++ {
		if !afi.running {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Hard killing the process
	afi.fishKill()
}

// Starts the fish node executable
func (afi *afInstance) Start(t *testing.T, args ...string) {
	if afi.running {
		t.Fatalf("ERROR: Fish node can't be started since already started: %s", afi.node_name)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	afi.fishKill = cancel

	cmd_args := []string{"-v", "debug", "-c", filepath.Join(afi.workspace, "config.yml")}
	cmd_args = append(cmd_args, args...)
	afi.cmd = exec.CommandContext(ctx, fish_path, cmd_args...)
	afi.cmd.Dir = afi.workspace
	r, _ := afi.cmd.StdoutPipe()
	afi.cmd.Stderr = afi.cmd.Stdout

	init_done := make(chan string)
	scanner := bufio.NewScanner(r)
	// TODO: Add timeout for waiting of API available
	go func() {
		// Listening for log and scan for token and address
		for scanner.Scan() {
			line := scanner.Text()
			t.Log(afi.node_name, line)
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

	afi.cmd.Start()

	go func() {
		afi.running = true
		defer func() {
			afi.running = false
			r.Close()
		}()
		if err := afi.cmd.Wait(); err != nil {
			t.Log("AquariumFish process was stopped:", err)
			init_done <- fmt.Sprintf("ERROR: Fish was stopped with exit code: %v", err)
		}
	}()

	failed := <-init_done

	if failed != "" {
		t.Fatalf(failed)
	}
}

// Func to copy files around
func copyFile(src, dst string) error {
	fin, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fin.Close()

	os.MkdirAll(filepath.Dir(dst), 0755)
	fout, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer fout.Close()

	if _, err = io.Copy(fout, fin); err != nil {
		return err
	}

	return nil
}
