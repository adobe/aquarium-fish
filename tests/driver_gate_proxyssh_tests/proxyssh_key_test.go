/**
 * Copyright 2024-2025 Adobe. All rights reserved.
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

package tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/crypt"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	"github.com/adobe/aquarium-fish/lib/util"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Checks that proxyssh can establish ssh connection with TTY and execute there a simple command
// Client will use key and proxy will connect to target by password
// WARN: This test requires `ssh` and `sh` binary to be available in PATH
func Test_proxyssh_ssh_key2password_tty_access(t *testing.T) {
	t.Parallel()
	afi := h.NewStoppedAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
    proxyssh:
      bind_address: 127.0.0.1:0
  providers:
    test:`)

	var proxysshEndpoint string
	afi.WaitForLog(" proxyssh.addr=", func(substring, line string) bool {
		data := strings.SplitN(strings.TrimSpace(line), substring, 2)
		addrport, err := netip.ParseAddrPort(data[1])
		if err != nil {
			t.Fatalf("ERROR: Unable to parse address:port from data %q: %v", data[1], err)
			return false
		}
		proxysshEndpoint = addrport.String()
		t.Logf("Located proxyssh endpoint: %q", proxysshEndpoint)

		return true
	})

	afi.Start(t)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	// Note: HTTP client removed as we're using RPC calls now

	// Running SSH Pty server with shell
	_, sshdPort := h.MockSSHPtyServer(t, "testuser", "testpass", "")

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
						Authentication: &aquariumv2.Authentication{
							Username: "testuser",
							Password: "testpass",
							Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	var resUID string
	t.Run("Resource should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	// Now working with the created Application to get access
	var acc *aquariumv2.GateProxySSHAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(
			context.Background(),
			connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
				ApplicationResourceUid: resUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		acc = resp.Msg.Data

		if acc.Key == "" {
			t.Fatalf("Unable to get access to Resource: %v", resUID)
		}
	})

	t.Run("Executing SSH shell through PROXYSSH", func(t *testing.T) {
		// Writing ssh private key to temp file
		proxyKeyFile, err := os.CreateTemp("", "proxykey")
		if err != nil {
			t.Fatalf("Unable to create temp proxykey file: %v", err)
		}
		defer os.Remove(proxyKeyFile.Name())
		_, err = proxyKeyFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp proxykey file: %v", err)
		}
		proxyKeyFile.Close()
		err = os.Chmod(proxyKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp proxykey file mod: %v", err)
		}

		proxyHost, proxyPort, err := net.SplitHostPort(proxysshEndpoint)
		if err != nil {
			t.Fatalf("Unable to parse ProxySSH endpoint: %v", err)
		}

		// In order to emulate terminal input we using pipe to write. This allows us to keep the
		// stdin opened while we working with ssh app, otherwise something like
		// input := bytes.NewBufferString("echo 'Its ALIVE!'\nexit\n") will just close the stream
		// and test will be ok on MacOS (Sonoma 14.5, OpenSSH_9.6p1), but will close the src->dst
		// channel on Linux (Debian 12.8, OpenSSH 9.2p1).
		pipeReader, pipeWriter := io.Pipe()

		go func() {
			// Function to write to the pipe and not to close the channel until we need to. It uses
			// sleep, which is not that great and could be switched to getting response, but meh
			defer pipeWriter.Close()

			// While connection establishing we preparing for the write just like humans do
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'Its ALIVE!'\n"))
			// After we hit enter - expecting some output from there
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			// Not closing pipeWriter, because the other side should close reader
			time.Sleep(100 * time.Millisecond)
		}()

		// Running SSH client and receiving the output
		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt", // We need to request PTY for server
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v (stderr: %s)", err, stderr)
		}

		// SSH output is full of special symbols, so looking just for the desired output
		if !strings.Contains(stdout, "\nIts ALIVE!") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q not in %q (stderr: %s)", "\nIts ALIVE!", stdout, stderr)
			//} else {
			//      t.Log(fmt.Sprintf("Correct response from command through PROXYSSH: %q in %q (stderr: %s)", "\nIts ALIVE!", stdout, stderr))
		}
	})
}

// Checks that proxyssh can establish ssh connection with TTY and execute there a simple command
// Client will use key and proxy will connect to target by key
// WARN: This test requires `ssh` and `sh` binary to be available in PATH
func Test_proxyssh_ssh_key2key_tty_access(t *testing.T) {
	t.Parallel()
	afi := h.NewStoppedAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
    proxyssh:
      bind_address: 127.0.0.1:0
  providers:
    test:`)

	var proxysshEndpoint string
	afi.WaitForLog(" proxyssh.addr=", func(substring, line string) bool {
		data := strings.SplitN(strings.TrimSpace(line), substring, 2)
		addrport, err := netip.ParseAddrPort(data[1])
		if err != nil {
			t.Fatalf("ERROR: Unable to parse address:port from data %q: %v", data[1], err)
			return false
		}
		proxysshEndpoint = addrport.String()
		t.Logf("Located proxyssh endpoint: %q", proxysshEndpoint)

		return true
	})

	afi.Start(t)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	sshdKey, err := crypt.GenerateSSHKey()
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}
	sshdPubKey, err := crypt.GetSSHPubKeyFromPem(sshdKey)
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}

	// Running mock SSH Pty server with shell
	sshdHost, sshdPort := h.MockSSHPtyServer(t, "testuser", "", string(sshdPubKey))

	// First executing a simple one directly over the mock server with a little validation
	// NOTE: Previously we used it to compare with proxyssh output, but multiple variables made it
	// very unstable, so I leave it for now here commented and check just for echo output
	//var sshdTestOutput string
	t.Run("Executing SSH shell directly on mock SSHD", func(t *testing.T) {
		// Writing ssh private key to temp file
		sshdKeyFile, err := os.CreateTemp("", "sshdkey")
		if err != nil {
			t.Fatalf("Unable to create temp sshdkey file: %v", err)
		}
		defer os.Remove(sshdKeyFile.Name())
		_, err = sshdKeyFile.WriteString(string(sshdKey))
		if err != nil {
			t.Fatalf("Unable to write temp sshdkey file: %v", err)
		}
		sshdKeyFile.Close()
		err = os.Chmod(sshdKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp sshdkey file mod: %v", err)
		}

		// In order to emulate terminal input we using pipe to write. This allows us to keep the
		// stdin opened while we working with ssh app, otherwise something like
		// input := bytes.NewBufferString("echo 'Its ALIVE!'\nexit\n") will just close the stream
		// and test will be ok on MacOS (Sonoma 14.5, OpenSSH_9.6p1), but will close the src->dst
		// channel on Linux (Debian 12.8, OpenSSH 9.2p1).
		pipeReader, pipeWriter := io.Pipe()

		go func() {
			// Function to write to the pipe and not to close the channel until we need to. It uses
			// sleep, which is not that great and could be switched to getting response, but meh
			defer pipeWriter.Close()

			// While connection establishing we preparing for the write just like humans do
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'Its ALIVE!'\n"))
			// After we hit enter - expecting some output from there
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			// Not closing pipeWriter, because the other side should close reader
			time.Sleep(100 * time.Millisecond)
		}()

		// Running SSH client and receiving the input
		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-vvv",
			"-i", sshdKeyFile.Name(),
			"-p", sshdPort,
			"-tt", // We need to request PTY for server
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "testuser",
			sshdHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute command directly on mock sshd: %v (stderr: %s)", err, stderr)
		}

		// SSH output is full of special symbols, so looking just for the desired output
		if !strings.Contains(stdout, "Its ALIVE!\n") {
			t.Fatalf("Incorrect response from command on mock sshd: %q not in %q (stderr: %s)", "Its ALIVE!\n", stdout, stderr)
			//} else {
			//	t.Log(fmt.Sprintf("Correct response from command on mock sshd: %q in %q (stderr: %s)", "Its ALIVE!\n", stdout, stderr))
		}
		//sshdTestOutput = stdout
	})

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
						Authentication: &aquariumv2.Authentication{
							Username: "testuser",
							Key:      string(sshdKey),
							Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	var resUID string
	t.Run("Resource should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	// Now working with the created Application to get access
	var acc *aquariumv2.GateProxySSHAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(
			context.Background(),
			connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
				ApplicationResourceUid: resUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		acc = resp.Msg.Data

		if acc.Key == "" {
			t.Fatalf("Unable to get access to Resource: %v", resUID)
		}
	})

	t.Run("Executing SSH shell through PROXYSSH", func(t *testing.T) {
		// Writing ssh private key to temp file
		proxyKeyFile, err := os.CreateTemp("", "proxykey")
		if err != nil {
			t.Fatalf("Unable to create temp proxykey file: %v", err)
		}
		defer os.Remove(proxyKeyFile.Name())
		_, err = proxyKeyFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp proxykey file: %v", err)
		}
		proxyKeyFile.Close()
		err = os.Chmod(proxyKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp proxykey file mod: %v", err)
		}

		proxyHost, proxyPort, err := net.SplitHostPort(proxysshEndpoint)
		if err != nil {
			t.Fatalf("Unable to parse ProxySSH endpoint: %v", err)
		}

		// In order to emulate terminal input we using pipe to write. This allows us to keep the
		// stdin opened while we working with ssh app, otherwise something like
		// input := bytes.NewBufferString("echo 'Its ALIVE!'\nexit\n") will just close the stream
		// and test will be ok on MacOS (Sonoma 14.5, OpenSSH_9.6p1), but will close the src->dst
		// channel on Linux (Debian 12.8, OpenSSH 9.2p1).
		pipeReader, pipeWriter := io.Pipe()

		go func() {
			// Function to write to the pipe and not to close the channel until we need to. It uses
			// sleep, which is not that great and could be switched to getting response, but meh
			defer pipeWriter.Close()

			// While connection establishing we preparing for the write just like humans do
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'Its ALIVE!'\n"))
			// After we hit enter - expecting some output from there
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			// Not closing pipeWriter, because the other side should close reader
			time.Sleep(100 * time.Millisecond)
		}()

		// Running SSH client and receiving the input
		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt", // We need to request PTY for server
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v (stderr: %s)", err, stderr)
		}

		// SSH output is full of special symbols, so looking just for the desired output
		//if stdout != sshdTestOutput {
		if !strings.Contains(stdout, "\nIts ALIVE!") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q != %q (stderr: %s)", "\nIts ALIVE!", stdout, stderr)
			//} else {
			//	t.Log(fmt.Printf("Correct response from command through PROXYSSH: %q == %q (stderr: %s)", "\nIts ALIVE!", stdout, stderr))
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}

// Test ProxySSH SCP functionality with key authentication for client and password for target
func Test_proxyssh_scp_key2password_copy(t *testing.T) {
	t.Parallel()
	afi := h.NewStoppedAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
    proxyssh:
      bind_address: 127.0.0.1:0
  providers:
    test:`)

	var proxysshEndpoint string
	afi.WaitForLog(" proxyssh.addr=", func(substring, line string) bool {
		data := strings.SplitN(strings.TrimSpace(line), substring, 2)
		addrport, err := netip.ParseAddrPort(data[1])
		if err != nil {
			t.Fatalf("ERROR: Unable to parse address:port from data %q: %v", data[1], err)
			return false
		}
		proxysshEndpoint = addrport.String()
		t.Logf("Located proxyssh endpoint: %q", proxysshEndpoint)

		return true
	})

	afi.Start(t)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	// Running SSH Sftp server with shell
	_, sshdPort := h.MockSSHSftpServer(t, "testuser", "testpass", "")

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
						Authentication: &aquariumv2.Authentication{
							Username: "testuser",
							Password: "testpass",
							Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == "" || labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	var resUID string
	t.Run("Resource should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	var accUsername, accKey string
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(
			context.Background(),
			connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
				ApplicationResourceUid: resUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		accUsername = resp.Msg.Data.Username
		accKey = resp.Msg.Data.Key

		if accUsername == "" {
			t.Fatalf("Unable to get access to Resource: %v", resp.Msg.Data)
		}
	})

	t.Run("Downloading files by SCP SFTP through PROXYSSH", func(t *testing.T) {
		// Create temp dirs for input and output
		srcdir, err := os.MkdirTemp("", "srcdir")
		if err != nil {
			t.Fatalf("Unable to create srcdir: %v", err)
		}
		defer os.RemoveAll(srcdir)
		dstdir, err := os.MkdirTemp("", "dstdir")
		if err != nil {
			t.Fatalf("Unable to create dstdir: %v", err)
		}
		defer os.RemoveAll(dstdir)

		// Create a few random files
		if _, err = h.CreateRandomFiles(srcdir, 5); err != nil {
			t.Fatalf("Unable to generate random files: %v", err)
		}

		// Writing ssh private key to temp file
		proxyKeyFile, err := os.CreateTemp("", "proxykey")
		if err != nil {
			t.Fatalf("Unable to create temp proxykey file: %v", err)
		}
		defer os.Remove(proxyKeyFile.Name())
		_, err = proxyKeyFile.WriteString(accKey)
		if err != nil {
			t.Fatalf("Unable to write temp proxykey file: %v", err)
		}
		proxyKeyFile.Close()
		err = os.Chmod(proxyKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp proxykey file mod: %v", err)
		}

		proxyHost, proxyPort, err := net.SplitHostPort(proxysshEndpoint)

		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, nil, "scp", "-v",
			"-s", // Forcing SFTP for the scp < v9.0
			"-i", proxyKeyFile.Name(),
			"-P", proxyPort,
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"admin@"+proxyHost+":"+srcdir+"/*",
			dstdir,
		)
		if err != nil {
			t.Fatalf("Failed to copy files via PROXYSSH: %v, (stdout: %q, stderr: %q)", err, stdout, stderr)
		}

		// Compare 2 directories - they should contain identical files
		if err = h.CompareDirFiles(srcdir, dstdir); err != nil {
			t.Fatalf("Found differences in the copied files from %q to %q: %v, (stdout: %q, stderr: %q)", srcdir, dstdir, err, stdout, stderr)
		}
	})

	// Re-requesting the access to copy in other direction
	t.Run("Requesting access 2 to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(
			context.Background(),
			connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
				ApplicationResourceUid: resUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		accUsername = resp.Msg.Data.Username
		accKey = resp.Msg.Data.Key

		if accUsername == "" {
			t.Fatalf("Unable to get access to Resource: %v", resp.Msg.Data)
		}
	})

	t.Run("Uploading files by SCP SFTP through PROXYSSH", func(t *testing.T) {
		// Create temp dirs for input and output
		srcdir, err := os.MkdirTemp("", "srcdir")
		if err != nil {
			t.Fatalf("Unable to create srcdir: %v", err)
		}
		defer os.RemoveAll(srcdir)
		dstdir, err := os.MkdirTemp("", "dstdir")
		if err != nil {
			t.Fatalf("Unable to create dstdir: %v", err)
		}
		defer os.RemoveAll(dstdir)

		// Create a few random files
		var srcFiles []string
		if srcFiles, err = h.CreateRandomFiles(srcdir, 5); err != nil {
			t.Fatalf("Unable to generate random files: %v", err)
		}

		// Writing ssh private key to temp file
		proxyKeyFile, err := os.CreateTemp("", "proxykey")
		if err != nil {
			t.Fatalf("Unable to create temp proxykey file: %v", err)
		}
		defer os.Remove(proxyKeyFile.Name())
		_, err = proxyKeyFile.WriteString(accKey)
		if err != nil {
			t.Fatalf("Unable to write temp proxykey file: %v", err)
		}
		proxyKeyFile.Close()
		err = os.Chmod(proxyKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp proxykey file mod: %v", err)
		}

		proxyHost, proxyPort, err := net.SplitHostPort(proxysshEndpoint)

		args := []string{
			"-v",
			"-s", // Forcing SFTP for the scp < v9.0
			"-i", proxyKeyFile.Name(),
			"-P", proxyPort,
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
		}
		args = append(args, srcFiles...)
		args = append(args, "admin@"+proxyHost+":"+dstdir)

		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, nil, "scp", args...)
		if err != nil {
			t.Fatalf("Failed to copy files via PROXYSSH: %v, (stdout: %q, stderr: %q)", err, stdout, stderr)
		}

		// Compare 2 directories - they should contain identical files
		if err = h.CompareDirFiles(srcdir, dstdir); err != nil {
			t.Fatalf("Found differences in the copied files from %q to %q: %v, (stdout: %q, stderr: %q)", srcdir, dstdir, err, stdout, stderr)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}

// Test ProxySSH port forwarding functionality with key-to-key authentication
func Test_proxyssh_port_key2key(t *testing.T) {
	t.Parallel()
	afi := h.NewStoppedAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
    proxyssh:
      bind_address: 127.0.0.1:0
  providers:
    test:`)

	var proxysshEndpoint string
	afi.WaitForLog(" proxyssh.addr=", func(substring, line string) bool {
		data := strings.SplitN(strings.TrimSpace(line), substring, 2)
		addrport, err := netip.ParseAddrPort(data[1])
		if err != nil {
			t.Fatalf("ERROR: Unable to parse address:port from data %q: %v", data[1], err)
			return false
		}
		proxysshEndpoint = addrport.String()
		t.Logf("Located proxyssh endpoint: %q", proxysshEndpoint)

		return true
	})

	afi.Start(t)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Still need HTTPS client to test the port proxy working correctly
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: tr,
	}

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	sshdKey, err := crypt.GenerateSSHKey()
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}
	sshdPubKey, err := crypt.GetSSHPubKeyFromPem(sshdKey)
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}

	// Running SSH port server
	_, sshdPort := h.MockSSHPortServer(t, "testuser", "", string(sshdPubKey))

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
						Authentication: &aquariumv2.Authentication{
							Username: "testuser",
							Key:      string(sshdKey),
							Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	var resUID string
	t.Run("Resource should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	// Now working with the created Application to get access
	var acc *aquariumv2.GateProxySSHAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(
			context.Background(),
			connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
				ApplicationResourceUid: resUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		acc = resp.Msg.Data

		if acc.Key == "" {
			t.Fatalf("Unable to get access to Resource: %v", resUID)
		}
	})

	t.Run("Using ProxySSH to establish local port forwarding", func(t *testing.T) {
		// Writing ssh private key to temp file
		proxyKeyFile, err := os.CreateTemp("", "proxykey")
		if err != nil {
			t.Fatalf("Unable to create temp proxykey file: %v", err)
		}
		defer os.Remove(proxyKeyFile.Name())
		_, err = proxyKeyFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp proxykey file: %v", err)
		}
		proxyKeyFile.Close()
		err = os.Chmod(proxyKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp proxykey file mod: %v", err)
		}

		proxyHost, proxyPort, err := net.SplitHostPort(proxysshEndpoint)
		if err != nil {
			t.Fatalf("Unable to parse ProxySSH endpoint: %v", err)
		}

		_, apiPort, err := net.SplitHostPort(afi.APIEndpoint())
		// Find a free local port for forwarding
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("Unable to find free port: %v", err)
		}
		proxyApiPort := listener.Addr().(*net.TCPAddr).Port
		listener.Close()

		// Running command with timeout in background
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "ssh", "-v",
			// ssh -N -R 2223:localhost:2222 -p 2222 testuser@127.0.0.1
			// ssh -N -L 2223:localhost:2222 -p 2222 testuser@127.0.0.1
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			"-N", // Don't establish ssh session
			"-L", strconv.Itoa(proxyApiPort)+":localhost:"+apiPort,
			proxyHost,
		)
		t.Log("DEBUG: Executing:", strings.Join(cmd.Args, " "), acc.Password, string(sshdKey))

		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Start()

		// Wait for ssh port passthrough startup
		time.Sleep(2 * time.Second)

		// Requesting Fish API through proxied port for the next test
		var newAcc aquariumv2.GateProxySSHServiceGetResourceAccessResponse
		apitest.New().
			EnableNetworking(cli).
			Post("https://127.0.0.1:"+strconv.Itoa(proxyApiPort)+"/grpc/aquarium.v2.GateProxySSHService/GetResourceAccess").
			JSON(`{"application_resource_uid": "`+acc.ApplicationResourceUid+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&newAcc)

		if newAcc.GetData().Uid == "" || newAcc.GetData().Uid == uuid.Nil.String() {
			t.Fatalf("Unable to get access to Resource: %v", acc.Uid)
		}
	})

	// TODO: For some reason mock server does not accept reverse port forwarding, but
	// I spent too much time on that already, so the direct forwarding enough for testing now
	//t.Run("Executing SSH port reverse pass through PROXYSSH", func(t *testing.T) {
	//	// Writing ssh private key to temp file
	//	proxyKeyFile, err := os.CreateTemp("", "proxykey")
	//	if err != nil {
	//		t.Fatalf("Unable to create temp proxykey file: %v", err)
	//	}
	//	defer os.Remove(proxyKeyFile.Name())
	//	_, err = proxyKeyFile.WriteString(acc.Key)
	//	if err != nil {
	//		t.Fatalf("Unable to write temp proxykey file: %v", err)
	//	}
	//	proxyKeyFile.Close()
	//	err = os.Chmod(proxyKeyFile.Name(), 0600)
	//	if err != nil {
	//		t.Fatalf("Unable to change temp proxykey file mod: %v", err)
	//	}
	//	proxyHost, proxyPort, err := net.SplitHostPort(proxysshEndpoint)
	//	_, apiPort, err := net.SplitHostPort(afi.APIEndpoint())
	//	// Picking semi-random port to listen on
	//	proxyApiPort, _ := strconv.Atoi(apiPort)
	//	proxyApiPort += 10
	//
	//	// Running command with timeout in background
	//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	//	defer cancel()
	//	cmd := exec.CommandContext(ctx, "ssh", "-v",
	//		// ssh -N -R 2223:localhost:2222 -p 2222 testuser@127.0.0.1
	//		// ssh -N -L 2223:localhost:2222 -p 2222 testuser@127.0.0.1
	//		"-i", proxyKeyFile.Name(),
	//		"-p", proxyPort,
	//		"-oStrictHostKeyChecking=no",
	//		"-oUserKnownHostsFile=/dev/null",
	//		"-oGlobalKnownHostsFile=/dev/null",
	//		"-l", "admin",
	//		"-N", // Don't establish ssh session
	//		"-R", strconv.Itoa(proxyApiPort)+":localhost:"+apiPort,
	//		proxyHost,
	//	)
	//	cmd.Stdout = os.Stderr
	//	cmd.Stderr = os.Stderr
	//	cmd.Start()
	//
	//	// Wait for ssh port passthrough startup
	//	time.Sleep(2*time.Second)
	//
	//	// Requesting Fish API through proxied port
	//	apitest.New().
	//		EnableNetworking(cli).
	//		Get("https://127.0.0.1:"+strconv.Itoa(proxyApiPort)+"/api/v1/application/"+app.Uid+"/resource").
	//		BasicAuth("admin", afi.AdminToken()).
	//		Expect(t).
	//		Status(http.StatusOK).
	//		End().
	//		JSON(&res)
	//})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}
