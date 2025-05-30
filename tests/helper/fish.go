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
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

var fishPath = os.Getenv("FISH_PATH") // Full path to the aquarium-fish binary

// AFInstance saves state of the running Aquarium Fish for particular test
type AFInstance struct {
	workspace string
	fishKill  context.CancelFunc
	running   bool
	cmd       *exec.Cmd

	nodeName   string
	adminToken string

	apiEndpoint      string
	proxysshEndpoint string

	waitForLog   map[string]func(string, string) bool
	waitForLogMu sync.RWMutex
}

// NewAquariumFish simple creates and run the fish node
func NewAquariumFish(tb testing.TB, name, cfg string, args ...string) *AFInstance {
	tb.Helper()
	afi := NewAfInstance(tb, name, cfg)
	afi.Start(tb, args...)

	return afi
}

// NewAfInstance helpful if you need to create instance without starting it up right away
func NewAfInstance(tb testing.TB, name, cfg string) *AFInstance {
	tb.Helper()
	tb.Log("INFO: Creating new node:", name)
	afi := &AFInstance{
		nodeName:   name,
		waitForLog: make(map[string]func(string, string) bool),
	}

	// Not using here tb.TempDir to have an ability to save on cleanup for investigation
	var err error
	if afi.workspace, err = os.MkdirTemp("", "fish"); err != nil {
		tb.Fatal("INFO: Unable to create workspace:", afi.nodeName, err)
		return nil
	}
	tb.Log("INFO: Created workspace:", afi.nodeName, afi.workspace)

	cfg += fmt.Sprintf("\nnode_name: %q", afi.nodeName)
	os.WriteFile(filepath.Join(afi.workspace, "config.yml"), []byte(cfg), 0o600)
	tb.Log("INFO: Stored config:", cfg)

	return afi
}

// NewClusterNode starts another node of cluster
// It will automatically add cluster_join parameter to the config
func (afi *AFInstance) NewClusterNode(tb testing.TB, name, cfg string, args ...string) *AFInstance {
	tb.Helper()
	afi2 := afi.NewAfInstanceCluster(tb, name, cfg)
	afi2.Start(tb, args...)

	return afi2
}

// NewAfInstanceCluster just creates the node based on the existing cluster node
func (afi *AFInstance) NewAfInstanceCluster(tb testing.TB, name, cfg string) *AFInstance {
	tb.Helper()
	tb.Log("INFO: Creating new cluster node with seed node:", afi.nodeName)
	cfg += fmt.Sprintf("\ncluster_join: [%q]", afi.apiEndpoint)
	afi2 := NewAfInstance(tb, name, cfg)

	// Copy seed node CA to generate valid cluster node cert
	if err := CopyFile(filepath.Join(afi.workspace, "fish_data", "ca.key"), filepath.Join(afi2.workspace, "fish_data", "ca.key")); err != nil {
		tb.Fatalf("ERROR: Unable to copy CA key: %v", err)
	}
	if err := CopyFile(filepath.Join(afi.workspace, "fish_data", "ca.crt"), filepath.Join(afi2.workspace, "fish_data", "ca.crt")); err != nil {
		tb.Fatalf("ERROR: Unable to copy CA crt: %v", err)
	}

	return afi2
}

// APIEndpoint will return IP:PORT
func (afi *AFInstance) APIEndpoint() string {
	return afi.apiEndpoint
}

// ProxySSHEndpoint will return IP:PORT
func (afi *AFInstance) ProxySSHEndpoint() string {
	return afi.proxysshEndpoint
}

// APIAddress will return url to access API of AquariumFish
func (afi *AFInstance) APIAddress(path string) string {
	return fmt.Sprintf("https://%s/%s", afi.apiEndpoint, path)
}

// Workspace will return workspace of the AquariumFish
func (afi *AFInstance) Workspace() string {
	return afi.workspace
}

// AdminToken returns admin token
func (afi *AFInstance) AdminToken() string {
	return afi.adminToken
}

// IsRunning checks the fish instance is running
func (afi *AFInstance) IsRunning() bool {
	return afi.running
}

// Restart the application
func (afi *AFInstance) Restart(tb testing.TB, args ...string) {
	tb.Helper()
	tb.Log("INFO: Restarting:", afi.nodeName, afi.workspace)
	afi.Stop(tb)
	afi.Start(tb, args...)
}

// Cleanup after the test execution
func (afi *AFInstance) Cleanup(tb testing.TB) {
	tb.Helper()
	tb.Log("INFO: Cleaning up:", afi.nodeName, afi.workspace)
	afi.Stop(tb)

	if tb.Failed() {
		tb.Log("INFO: Keeping workspace for checking:", afi.workspace)
		return
	}
	os.RemoveAll(afi.workspace)
}

// Stop the fish node executable
func (afi *AFInstance) Stop(tb testing.TB) {
	tb.Helper()
	if afi.cmd == nil || !afi.running {
		return
	}
	// Send interrupt signal
	afi.cmd.Process.Signal(os.Interrupt)

	// Wait 10 seconds for process to stop
	tb.Log("INFO: Wait 10s for fish node to stop:", afi.nodeName, afi.workspace)
	for i := 1; i < 20; i++ {
		if !afi.running {
			usage, ok := afi.cmd.ProcessState.SysUsage().(*syscall.Rusage)
			if ok {
				tb.Log("INFO: MaxRSS:", usage.Maxrss)
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Hard killing the process
	afi.fishKill()
	for i := 1; i < 20; i++ {
		if !afi.running {
			usage, ok := afi.cmd.ProcessState.SysUsage().(*syscall.Rusage)
			if ok {
				tb.Log("INFO: MaxRSS:", usage.Maxrss)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (afi *AFInstance) PrintMemUsage(tb testing.TB) {
	tb.Helper()
	proc, err := process.NewProcess(int32(afi.cmd.Process.Pid))
	if err != nil {
		tb.Log("ERROR: Unable to read process for PID", afi.cmd.Process.Pid, err)
		return
	}
	mem, err := proc.MemoryInfo()
	if err != nil {
		tb.Log("ERROR: Unable to read process memory info for PID", afi.cmd.Process.Pid, err)
		return
	}
	tb.Log("INFO: node", afi.nodeName, "memory usage:", mem.String())
}

// WaitForLog stores substring to be looked in the Fish log to execute call function with substring & found line
func (afi *AFInstance) WaitForLog(substring string, call func(string, string) bool) {
	afi.waitForLogMu.Lock()
	defer afi.waitForLogMu.Unlock()
	afi.waitForLog[substring] = call
}

// callWaitForLog is called by log scanner when substring from waitForLog was found
func (afi *AFInstance) callWaitForLog(substring, line string) {
	afi.waitForLogMu.RLock()
	call := afi.waitForLog[substring]
	afi.waitForLogMu.RUnlock()
	if processed := call(substring, line); processed {
		afi.waitForLogMu.Lock()
		delete(afi.waitForLog, substring)
		afi.waitForLogMu.Unlock()
	}
}

// Start the fish node executable
func (afi *AFInstance) Start(tb testing.TB, args ...string) {
	tb.Helper()
	if afi.running {
		tb.Fatalf("ERROR: Fish node %q can't be started since already started", afi.nodeName)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	afi.fishKill = cancel

	cmdArgs := []string{"-v", "debug", "-c", filepath.Join(afi.workspace, "config.yml")}
	cmdArgs = append(cmdArgs, args...)
	afi.cmd = exec.CommandContext(ctx, fishPath, cmdArgs...)
	afi.cmd.Dir = afi.workspace
	r, _ := afi.cmd.StdoutPipe()
	afi.cmd.Stderr = afi.cmd.Stdout

	initDone := make(chan string)
	scanner := bufio.NewScanner(r)
	// Increasing scanner line buffer from 64KB to 1MB
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	afi.WaitForLog("Admin user pass: ", func(substring, line string) bool {
		if !strings.HasPrefix(line, substring) {
			return false
		}
		val := strings.SplitN(strings.TrimSpace(line), substring, 2)
		if len(val) < 2 {
			initDone <- fmt.Sprintf("ERROR: No token after %q", substring)
			return false
		}
		afi.adminToken = val[1]

		return true
	})

	afi.WaitForLog("API listening on: ", func(substring, line string) bool {
		val := strings.SplitN(strings.TrimSpace(line), substring, 2)
		if len(val) < 2 {
			initDone <- fmt.Sprintf("ERROR: No address after %q", substring)
			return false
		}
		afi.apiEndpoint = val[1]

		return true
	})
	afi.WaitForLog("PROXYSSH listening on: ", func(substring, line string) bool {
		val := strings.SplitN(strings.TrimSpace(line), substring, 2)
		if len(val) < 2 {
			initDone <- fmt.Sprintf("ERROR: No address after %q", substring)
			return false
		}
		afi.proxysshEndpoint = val[1]

		return true
	})
	afi.WaitForLog("Fish initialized", func(substring, line string) bool {
		if !strings.HasSuffix(line, substring) {
			return false
		}

		// Found the needed values and continue to process to print the fish output for
		// test debugging purposes
		initDone <- ""

		return true
	})

	// TODO: Add timeout for waiting of API available
	go func() {
		// Listening for log and scan for token and address
		for scanner.Scan() {
			line := scanner.Text()
			tb.Log(afi.nodeName, line)

			afi.waitForLogMu.RLock()
			var substrings []string
			for key := range afi.waitForLog {
				substrings = append(substrings, key)
			}
			afi.waitForLogMu.RUnlock()

			for _, substring := range substrings {
				if !strings.Contains(line, substring) {
					continue
				}
				afi.callWaitForLog(substring, line)
			}
		}
		tb.Log("INFO: Reading of AquariumFish output is done:", scanner.Err())
	}()

	afi.cmd.Start()

	go func() {
		afi.running = true
		defer func() {
			r.Close()
			afi.running = false
		}()
		if err := afi.cmd.Wait(); err != nil {
			tb.Log("WARN: AquariumFish process was stopped:", err)
			initDone <- fmt.Sprintf("ERROR: Fish was stopped with exit code: %v", err)
		}
	}()

	failed := <-initDone

	if failed != "" {
		tb.Fatalf("ERROR: Failed to init node %q: %s", afi.nodeName, failed)
	}
	tb.Log("INFO: Fish is ready:", afi.nodeName)
}
