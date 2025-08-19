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

package tests

import (
	"context"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/crypt"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	"github.com/adobe/aquarium-fish/lib/util"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test that one-time access (default) works exactly once for password authentication
func Test_proxyssh_access_password_onetime(t *testing.T) {
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

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Running SSH Pty server with shell
	_, sshdPort := h.MockSSHPtyServer(t, "testuser", "testpass", "")

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(context.Background(), connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "test-label",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver:    "test",
					Resources: &aquariumv2.Resources{Cpu: 1, Ram: 2},
					Authentication: &aquariumv2.Authentication{
						Username: "testuser",
						Password: "testpass",
						Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
					},
				}},
			},
		}))
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{LabelUid: labelUID},
		}))
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}))
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
		resp, err := appClient.GetResource(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid
	})

	var accUsername, accPassword string
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(context.Background(), connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
			ApplicationResourceUid: resUID,
		}))
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		accUsername = resp.Msg.Data.Username
		accPassword = resp.Msg.Data.Password
		if accUsername == "" {
			t.Fatalf("Unable to get access to Resource: %v", resp.Msg.Data)
		}
	})

	t.Run("First SSH connection should succeed", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'test'")
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}
		if !strings.Contains(string(response), "test") {
			t.Fatalf("Unexpected response from command through PROXYSSH: %q", string(response))
		}
	})

	t.Run("Second SSH connection should fail (one-time access)", func(t *testing.T) {
		_, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'test'")
		if err == nil {
			t.Fatalf("Second SSH connection should have failed for one-time access")
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})
}

// Test that static access can be used multiple times for password authentication
func Test_proxyssh_access_password_static(t *testing.T) {
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

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Running SSH Pty server with shell
	_, sshdPort := h.MockSSHPtyServer(t, "testuser", "testpass", "")

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(context.Background(), connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "test-label",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver:    "test",
					Resources: &aquariumv2.Resources{Cpu: 1, Ram: 2},
					Authentication: &aquariumv2.Authentication{
						Username: "testuser",
						Password: "testpass",
						Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
					},
				}},
			},
		}))
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{LabelUid: labelUID},
		}))
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}))
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
		resp, err := appClient.GetResource(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid
	})

	var accUsername, accPassword string
	t.Run("Requesting static access to the Application Resource", func(t *testing.T) {
		static := true
		resp, err := proxySSHClient.GetResourceAccess(context.Background(), connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
			ApplicationResourceUid: resUID,
			Static:                 &static,
		}))
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		accUsername = resp.Msg.Data.Username
		accPassword = resp.Msg.Data.Password
		if accUsername == "" {
			t.Fatalf("Unable to get access to Resource: %v", resp.Msg.Data)
		}
		if !resp.Msg.Data.Static {
			t.Fatalf("Access should be marked as static: %v", resp.Msg.Data.Static)
		}
	})

	t.Run("First SSH connection should succeed", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'test1'")
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}
		if !strings.Contains(string(response), "test1") {
			t.Fatalf("Unexpected response from command through PROXYSSH: %q", string(response))
		}
	})

	t.Run("Second SSH connection should succeed (static access)", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'test2'")
		if err != nil {
			t.Fatalf("Failed to execute second command via PROXYSSH: %v", err)
		}
		if !strings.Contains(string(response), "test2") {
			t.Fatalf("Unexpected response from second command through PROXYSSH: %q", string(response))
		}
	})

	t.Run("Third SSH connection should succeed (static access)", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'test3'")
		if err != nil {
			t.Fatalf("Failed to execute third command via PROXYSSH: %v", err)
		}
		if !strings.Contains(string(response), "test3") {
			t.Fatalf("Unexpected response from third command through PROXYSSH: %q", string(response))
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})
}

// Test that one-time access (default) works exactly once for key authentication
func Test_proxyssh_access_key_onetime(t *testing.T) {
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

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Generate SSH key for mock server
	sshdKey, err := crypt.GenerateSSHKey()
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}
	sshdPubKey, err := crypt.GetSSHPubKeyFromPem(sshdKey)
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}

	// Running SSH Pty server with shell
	_, sshdPort := h.MockSSHPtyServer(t, "testuser", "", string(sshdPubKey))

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(context.Background(), connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "test-label",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver:    "test",
					Resources: &aquariumv2.Resources{Cpu: 1, Ram: 2},
					Authentication: &aquariumv2.Authentication{
						Username: "testuser",
						Key:      string(sshdKey),
						Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
					},
				}},
			},
		}))
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{LabelUid: labelUID},
		}))
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}))
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
		resp, err := appClient.GetResource(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid
	})

	var acc *aquariumv2.GateProxySSHAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		resp, err := proxySSHClient.GetResourceAccess(context.Background(), connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
			ApplicationResourceUid: resUID,
		}))
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		acc = resp.Msg.Data
		if acc.Key == "" {
			t.Fatalf("Unable to get access to Resource: %v", resUID)
		}
	})

	t.Run("First SSH connection should succeed", func(t *testing.T) {
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

		// In order to emulate terminal input we using pipe to write
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			defer pipeWriter.Close()
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'test'\n"))
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			time.Sleep(100 * time.Millisecond)
		}()

		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt",
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v (stderr: %s)", err, stderr)
		}
		if !strings.Contains(stdout, "test") {
			t.Fatalf("Unexpected response from command through PROXYSSH: %q", stdout)
		}
	})

	t.Run("Second SSH connection should fail (one-time access)", func(t *testing.T) {
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

		// In order to emulate terminal input we using pipe to write
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			defer pipeWriter.Close()
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'test2'\n"))
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			time.Sleep(100 * time.Millisecond)
		}()

		_, _, err = util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt",
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err == nil {
			t.Fatalf("Second SSH connection should have failed for one-time access")
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})
}

// Test that static access can be used multiple times for key authentication
func Test_proxyssh_access_key_static(t *testing.T) {
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

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	proxySSHClient := aquariumv2connect.NewGateProxySSHServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Generate SSH key for mock server
	sshdKey, err := crypt.GenerateSSHKey()
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}
	sshdPubKey, err := crypt.GetSSHPubKeyFromPem(sshdKey)
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}

	// Running SSH Pty server with shell
	_, sshdPort := h.MockSSHPtyServer(t, "testuser", "", string(sshdPubKey))

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(context.Background(), connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "test-label",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver:    "test",
					Resources: &aquariumv2.Resources{Cpu: 1, Ram: 2},
					Authentication: &aquariumv2.Authentication{
						Username: "testuser",
						Key:      string(sshdKey),
						Port:     func() int32 { port, _ := strconv.Atoi(sshdPort); return int32(port) }(),
					},
				}},
			},
		}))
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{LabelUid: labelUID},
		}))
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}))
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
		resp, err := appClient.GetResource(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}
		resUID = resp.Msg.Data.Uid
	})

	var acc *aquariumv2.GateProxySSHAccess
	t.Run("Requesting static access to the Application Resource", func(t *testing.T) {
		static := true
		resp, err := proxySSHClient.GetResourceAccess(context.Background(), connect.NewRequest(&aquariumv2.GateProxySSHServiceGetResourceAccessRequest{
			ApplicationResourceUid: resUID,
			Static:                 &static,
		}))
		if err != nil {
			t.Fatal("Failed to get resource access:", err)
		}
		acc = resp.Msg.Data
		if acc.Key == "" {
			t.Fatalf("Unable to get access to Resource: %v", resUID)
		}
		if !resp.Msg.Data.Static {
			t.Fatalf("Access should be marked as static: %v", resp.Msg.Data.Static)
		}
	})

	t.Run("First SSH connection should succeed", func(t *testing.T) {
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

		// In order to emulate terminal input we using pipe to write
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			defer pipeWriter.Close()
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'test1'\n"))
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			time.Sleep(100 * time.Millisecond)
		}()

		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt",
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v (stderr: %s)", err, stderr)
		}
		if !strings.Contains(stdout, "test1") {
			t.Fatalf("Unexpected response from command through PROXYSSH: %q", stdout)
		}
	})

	t.Run("Second SSH connection should succeed (static access)", func(t *testing.T) {
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

		// In order to emulate terminal input we using pipe to write
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			defer pipeWriter.Close()
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'test2'\n"))
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			time.Sleep(100 * time.Millisecond)
		}()

		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt",
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute second command via PROXYSSH: %v (stderr: %s)", err, stderr)
		}
		if !strings.Contains(stdout, "test2") {
			t.Fatalf("Unexpected response from second command through PROXYSSH: %q", stdout)
		}
	})

	t.Run("Third SSH connection should succeed (static access)", func(t *testing.T) {
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

		// In order to emulate terminal input we using pipe to write
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			defer pipeWriter.Close()
			time.Sleep(time.Second)
			pipeWriter.Write([]byte("echo 'test3'\n"))
			time.Sleep(500 * time.Millisecond)
			pipeWriter.Write([]byte("exit\n"))
			time.Sleep(100 * time.Millisecond)
		}()

		stdout, stderr, err := util.RunAndLog("TEST", 5*time.Second, pipeReader, "ssh", "-v",
			"-i", proxyKeyFile.Name(),
			"-p", proxyPort,
			"-tt",
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			proxyHost,
		)
		if err != nil {
			t.Fatalf("Failed to execute third command via PROXYSSH: %v (stderr: %s)", err, stderr)
		}
		if !strings.Contains(stdout, "test3") {
			t.Fatalf("Unexpected response from third command through PROXYSSH: %q", stdout)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: appUID,
		}))
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})
}
