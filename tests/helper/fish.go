/**
 * Copyright 2023-2025 Adobe. All rights reserved.
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

package helper

import (
	"bufio"
	"context"
	"crypto/x509"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// AFInstance saves state of the running Aquarium Fish for particular test
type AFInstance struct {
	workspace string
	fishKill  context.CancelFunc
	running   bool
	cmd       *exec.Cmd

	nodeName   string
	adminToken string
	caPool     *x509.CertPool

	apiEndpoint string

	waitForLog   map[string]func(string, string) bool
	waitForLogMu sync.RWMutex

	// Mutex to protect running state and process state access
	processMu sync.RWMutex
	// Store process state after cmd.Wait() completes to avoid race conditions
	processState *os.ProcessState

	// Mutex to protect configuration fields set during initialization
	configMu sync.RWMutex
}

// NewAquariumFish simple creates and run the fish node
func NewAquariumFish(tb testing.TB, name, cfg string, args ...string) *AFInstance {
	tb.Helper()

	afi := NewStoppedAquariumFish(tb, name, cfg)
	afi.Start(tb, args...)

	return afi
}

// NewStoppedAquariumFish creates the fish node
func NewStoppedAquariumFish(tb testing.TB, name, cfg string) *AFInstance {
	tb.Helper()

	afi := NewAfInstance(tb, name, cfg)

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

	// Automatically cleaning up the workspace after the test is complete
	tb.Cleanup(func() {
		afi.Cleanup(tb)
	})

	// Enabling monitoring if env variable FISH_MONITORING is set
	if envAddr, ok := os.LookupEnv("FISH_MONITORING"); ok {
		if strings.Contains(cfg, "monitoring:") {
			tb.Log("WARN: Unable to enable monitoring due to config has monitoring already")
		} else {
			if envAddr != "" {
				tb.Logf("Enabling Fish remote OTLP/Pyroscope monitoring: %s", envAddr)
			} else {
				tb.Logf("Enabling Fish file-based monitoring")
			}
			monitoringCfg := fmt.Sprintf(`
monitoring:
  enabled: true

  otlp_endpoint: %s:4317
  pyroscope_url: http://%s:4040

  sample_rate: 1.0
  metrics_interval: "5s"
  profiling_interval: "10s"`, envAddr, envAddr)
			// Add monitoring configuration to the fish config
			cfg += monitoringCfg
		}
	}
	cfg += fmt.Sprintf("\nnode_name: %q", afi.nodeName)
	os.WriteFile(filepath.Join(afi.workspace, "config.yml"), []byte(cfg), 0o600)
	tb.Log("INFO: Stored config:", cfg)

	return afi
}

// GetCA will return the node CA pool for secure communication
func (afi *AFInstance) GetCA(tb testing.TB) *x509.CertPool {
	tb.Helper()
	afi.configMu.RLock()
	caPool := afi.caPool
	afi.configMu.RUnlock()
	if caPool == nil {
		ca, err := os.ReadFile(filepath.Join(afi.workspace, "fish_data", "ca.crt"))
		if err != nil {
			tb.Errorf("Unable to read Fish node CA: %v", err)
			return nil
		}
		caPool = x509.NewCertPool()
		if ok := caPool.AppendCertsFromPEM(ca); !ok {
			tb.Errorf("Unable to add Fish node CA to pool: %v", err)
			return nil
		}

		afi.configMu.Lock()
		afi.caPool = caPool
		afi.configMu.Unlock()
	}
	return caPool
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
	afi.configMu.RLock()
	defer afi.configMu.RUnlock()
	return afi.apiEndpoint
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
	afi.configMu.RLock()
	defer afi.configMu.RUnlock()
	return afi.adminToken
}

// IsRunning checks the fish instance is running
func (afi *AFInstance) IsRunning() bool {
	afi.processMu.RLock()
	defer afi.processMu.RUnlock()
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
// You don't need to call it if you use NewAfInstance(), NewAquariumFish() or NewStoppedAquariumFish()
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

	// Check if we need to stop at all
	afi.processMu.RLock()
	shouldStop := afi.cmd != nil && afi.running
	afi.processMu.RUnlock()

	if !shouldStop {
		return
	}

	// Cleaning up the node variables
	defer func() {
		// Not cleaning adminToken, because it's created just one time for DB
		afi.apiEndpoint = ""
		afi.caPool = nil
	}()

	// Send interrupt signal
	afi.cmd.Process.Signal(os.Interrupt)

	// Wait 30 seconds for process to stop
	tb.Log("INFO: Wait 30s for fish node to stop:", afi.nodeName, afi.workspace)
	for i := 1; i < 60; i++ {
		afi.processMu.RLock()
		isRunning := afi.running
		processState := afi.processState
		afi.processMu.RUnlock()

		if !isRunning {
			// Process has stopped, safely read process state
			if processState != nil {
				if usage, ok := processState.SysUsage().(*syscall.Rusage); ok {
					tb.Log("INFO: MaxRSS:", usage.Maxrss)
				}
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Hard killing the process
	tb.Errorf("I had to hard-kill Fish after 30s of Interrupt waiting - it's not good...")
	afi.fishKill()
	for i := 1; i < 20; i++ {
		afi.processMu.RLock()
		isRunning := afi.running
		processState := afi.processState
		afi.processMu.RUnlock()

		if !isRunning {
			// Process has stopped, safely read process state
			if processState != nil {
				if usage, ok := processState.SysUsage().(*syscall.Rusage); ok {
					tb.Log("INFO: MaxRSS:", usage.Maxrss)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (afi *AFInstance) PrintMemUsage(tb testing.TB) {
	tb.Helper()

	afi.processMu.RLock()
	cmd := afi.cmd
	isRunning := afi.running
	afi.processMu.RUnlock()

	if !isRunning || cmd == nil || cmd.Process == nil {
		tb.Log("ERROR: Process not running or not available for memory usage check")
		return
	}

	proc, err := process.NewProcess(int32(cmd.Process.Pid))
	if err != nil {
		tb.Log("ERROR: Unable to read process for PID", cmd.Process.Pid, err)
		return
	}
	mem, err := proc.MemoryInfo()
	if err != nil {
		tb.Log("ERROR: Unable to read process memory info for PID", cmd.Process.Pid, err)
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

// WaitForLog stores substring to be looked in the Fish log to execute call function with substring & found line
func (afi *AFInstance) WaitForLogDelete(substring string) {
	afi.waitForLogMu.Lock()
	defer afi.waitForLogMu.Unlock()
	delete(afi.waitForLog, substring)
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

	// Check if already running
	afi.processMu.RLock()
	alreadyRunning := afi.running
	afi.processMu.RUnlock()

	if alreadyRunning {
		tb.Fatalf("ERROR: Fish node %q can't be started since already started", afi.nodeName)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	afi.fishKill = cancel

	cmdArgs := []string{"-v", "debug", "-c", filepath.Join(afi.workspace, "config.yml")}
	cmdArgs = append(cmdArgs, args...)

	fishPath := initFishPath(tb)
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
		afi.configMu.Lock()
		afi.adminToken = val[1]
		afi.configMu.Unlock()
		tb.Logf("Located admin user token: %q", val[1])

		return true
	})

	afi.WaitForLog(` server.addr=`, func(substring, line string) bool {
		data := strings.SplitN(strings.TrimSpace(line), substring, 2)
		addrport, err := netip.ParseAddrPort(data[1])
		if err != nil {
			initDone <- fmt.Sprintf("ERROR: Unable to parse address:port from data %q: %v", data[1], err)
			return false
		}
		afi.configMu.Lock()
		afi.apiEndpoint = addrport.String()
		afi.configMu.Unlock()
		tb.Logf("Located api endpoint: %q", afi.apiEndpoint)

		return true
	})
	afi.WaitForLog(` main.fish_init=completed`, func(substring, line string) bool {
		if !strings.HasSuffix(line, substring) {
			return false
		}

		// Found the needed values and continue to process to print the fish output for
		// test debugging purposes
		initDone <- ""

		return true
	})

	// Detecting race conditions
	afi.WaitForLog("WARNING: DATA RACE", func(_ /*substring*/, _ /*line*/ string) bool {
		tb.Error("ERROR: Race condition detected!")
		return false
	})

	// TODO: Add timeout for waiting of API available
	go func() {
		// Listening for log and scan for token and address
		for scanner.Scan() {
			line := scanner.Text()
			tb.Log(afi.nodeName, line)

			go func(currentLine string) {
				afi.waitForLogMu.RLock()
				var substrings []string
				for key := range afi.waitForLog {
					substrings = append(substrings, key)
				}
				afi.waitForLogMu.RUnlock()

				for _, substring := range substrings {
					if !strings.Contains(currentLine, substring) {
						continue
					}
					afi.callWaitForLog(substring, currentLine)
				}
			}(line)
		}
		tb.Log("INFO: Reading of AquariumFish output is done:", scanner.Err())
	}()

	afi.cmd.Start()

	go func() {
		afi.processMu.Lock()
		afi.running = true
		afi.processMu.Unlock()

		defer func() {
			r.Close()
			afi.processMu.Lock()
			afi.running = false
			// Store process state after cmd.Wait() completes
			afi.processState = afi.cmd.ProcessState
			afi.processMu.Unlock()
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

	// Since waiters are executed in goroutine check for Admin password and API host:port available
	for range 50 {
		afi.configMu.RLock()
		hasAdminToken := afi.adminToken != ""
		hasAPIEndpoint := afi.apiEndpoint != ""
		afi.configMu.RUnlock()

		if hasAdminToken && hasAPIEndpoint {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	afi.configMu.RLock()
	adminToken := afi.adminToken
	apiEndpoint := afi.apiEndpoint
	afi.configMu.RUnlock()

	if adminToken == "" || apiEndpoint == "" {
		tb.Fatalf("ERROR: Failed to get admin token or api endpoint for node %q: %q %q", afi.nodeName, adminToken, apiEndpoint)
	}

	tb.Log("INFO: Fish is ready:", afi.nodeName)

	// Running goroutine to listen on fish node failure just in case afi.cmd.Wait will fail
	go func() {
		failed := <-initDone
		fmt.Println(failed)
	}()
}

// detectProjectRoot finds the project root directory by walking up from the current file
func detectProjectRoot() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("unable to get current file path")
	}

	// Walk up from tests/helper/fish.go to find the project root
	dir := filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))

	// Verify this is the project root by checking for go.mod
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return "", fmt.Errorf("could not find project root (no go.mod found)")
	}

	return dir, nil
}

// findLatestAquariumFishBinary finds the most recent aquarium-fish binary in the project root
func findLatestAquariumFishBinary() (string, error) {
	projectRoot, err := detectProjectRoot()
	if err != nil {
		return "", fmt.Errorf("failed to detect project root: %w", err)
	}

	// Pattern: aquarium-fish-*.<GOOS>_<GOARCH>
	pattern := fmt.Sprintf("aquarium-fish-*.%s_%s", runtime.GOOS, runtime.GOARCH)

	matches, err := filepath.Glob(filepath.Join(projectRoot, pattern))
	if err != nil {
		return "", fmt.Errorf("failed to search for binaries: %w", err)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no aquarium-fish binaries found matching pattern: %s", pattern)
	}

	// Sort by modification time (newest first)
	result := ""
	var resultModTime *time.Time
	for _, match := range matches {
		stat, err := os.Stat(match)
		if err != nil {
			continue
		}
		modTime := stat.ModTime()
		if resultModTime == nil || modTime.After(*resultModTime) {
			result = match
			resultModTime = &modTime
		}
	}

	return result, nil
}

// initFishPath initializes the fish binary path, either from FISH_PATH env var or by auto-detection
func initFishPath(tb testing.TB) string {
	tb.Helper()
	// First, try environment variable
	if envPath := os.Getenv("FISH_PATH"); envPath != "" {
		tb.Logf("Using aquarium-fish binary from FISH_PATH: %s", envPath)
		return envPath
	}

	// Auto-detect the binary
	detectedPath, err := findLatestAquariumFishBinary()
	if err != nil || detectedPath == "" {
		tb.Logf("Failed to auto-detect aquarium-fish binary: %v", err)
	}

	tb.Logf("Auto-detected aquarium-fish binary: %s", detectedPath)
	return detectedPath
}
