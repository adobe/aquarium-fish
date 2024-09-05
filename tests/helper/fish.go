/**
 * Copyright 2023 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package helper

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fish_path = os.Getenv("FISH_PATH") // Full path to the aquarium-fish binary

// Saves state of the running Aquarium Fish for particular test
type AFInstance struct {
	workspace string
	fishKill  context.CancelFunc
	running   bool
	cmd       *exec.Cmd

	node_name   string
	endpoint    string
	admin_token string
}

// Simple creates and run the fish node
func NewAquariumFish(tb testing.TB, name, cfg string, args ...string) *AFInstance {
	tb.Helper()
	afi := NewAfInstance(tb, name, cfg)
	afi.Start(tb, args...)

	return afi
}

// If you need to create instance without starting it up right away
func NewAfInstance(tb testing.TB, name, cfg string) *AFInstance {
	tb.Helper()
	tb.Log("INFO: Creating new node:", name)
	afi := &AFInstance{
		node_name: name,
	}

	afi.workspace = tb.TempDir()
	tb.Log("INFO: Created workspace:", afi.node_name, afi.workspace)

	cfg += fmt.Sprintf("\nnode_name: %q", afi.node_name)
	os.WriteFile(filepath.Join(afi.workspace, "config.yml"), []byte(cfg), 0o600)
	tb.Log("INFO: Stored config:", cfg)

	return afi
}

// Start another node of cluster
// It will automatically add cluster_join parameter to the config
func (afi1 *AFInstance) NewClusterNode(tb testing.TB, name, cfg string, args ...string) *AFInstance {
	tb.Helper()
	afi2 := afi1.NewAfInstanceCluster(tb, name, cfg)
	afi2.Start(tb, args...)

	return afi2
}

// Just create the node based on the existing cluster node
func (afi1 *AFInstance) NewAfInstanceCluster(tb testing.TB, name, cfg string) *AFInstance {
	tb.Helper()
	tb.Log("INFO: Creating new cluster node with seed node:", afi1.node_name)
	cfg += fmt.Sprintf("\ncluster_join: [%q]", afi1.endpoint)
	afi2 := NewAfInstance(tb, name, cfg)

	// Copy seed node CA to generate valid cluster node cert
	if err := CopyFile(filepath.Join(afi1.workspace, "fish_data", "ca.key"), filepath.Join(afi2.workspace, "fish_data", "ca.key")); err != nil {
		tb.Fatalf("ERROR: Unable to copy CA key: %v", err)
	}
	if err := CopyFile(filepath.Join(afi1.workspace, "fish_data", "ca.crt"), filepath.Join(afi2.workspace, "fish_data", "ca.crt")); err != nil {
		tb.Fatalf("ERROR: Unable to copy CA crt: %v", err)
	}

	return afi2
}

// Will return just IP:PORT
func (afi *AFInstance) Endpoint() string {
	return afi.endpoint
}

// Will return url to access API of AquariumFish
func (afi *AFInstance) ApiAddress(path string) string {
	return fmt.Sprintf("https://%s/%s", afi.endpoint, path)
}

// Will return workspace of the AquariumFish
func (afi *AFInstance) Workspace() string {
	return afi.workspace
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
func (afi *AFInstance) Restart(tb testing.TB, args ...string) {
	tb.Helper()
	tb.Log("INFO: Restarting:", afi.node_name, afi.workspace)
	afi.Stop(tb)
	afi.Start(tb, args...)
}

// Cleanup after the test execution
func (afi *AFInstance) Cleanup(tb testing.TB) {
	tb.Helper()
	tb.Log("INFO: Cleaning up:", afi.node_name, afi.workspace)
	afi.Stop(tb)
	os.RemoveAll(afi.workspace)
}

// Stops the fish node executable
func (afi *AFInstance) Stop(tb testing.TB) {
	tb.Helper()
	if afi.cmd == nil || !afi.running {
		return
	}
	// Send interrupt signal
	afi.cmd.Process.Signal(os.Interrupt)

	// Wait 10 seconds for process to stop
	tb.Log("INFO: Wait 10s for fish node to stop:", afi.node_name, afi.workspace)
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
func (afi *AFInstance) Start(tb testing.TB, args ...string) {
	tb.Helper()
	if afi.running {
		tb.Fatalf("ERROR: Fish node %q can't be started since already started", afi.node_name)
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
			tb.Log(afi.node_name, line)
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
				afi.endpoint = val[1]
			}
			if strings.HasSuffix(line, "Fish initialized") {
				// Found the needed values and continue to process to print the fish output for
				// test debugging purposes
				init_done <- ""
			}
		}
		tb.Log("INFO: Reading of AquariumFish output is done")
	}()

	afi.cmd.Start()

	go func() {
		afi.running = true
		defer func() {
			afi.running = false
			r.Close()
		}()
		if err := afi.cmd.Wait(); err != nil {
			tb.Log("WARN: AquariumFish process was stopped:", err)
			init_done <- fmt.Sprintf("ERROR: Fish was stopped with exit code: %v", err)
		}
	}()

	failed := <-init_done

	if failed != "" {
		tb.Fatalf("ERROR: Failed to init node %q: %s", afi.node_name, failed)
	}
}
