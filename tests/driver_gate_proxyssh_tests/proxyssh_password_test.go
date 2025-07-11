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
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Checks that proxyssh can establish ssh connection with TTY and execute there a simple command
// Client will use password and proxy will connect to target by password
// WARN: This test requires `sh` binary to be available in PATH
func Test_proxyssh_ssh_password2password_tty_access(t *testing.T) {
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

	// Running SSH Pty server with shell
	sshdHost, sshdPort := h.MockSSHPtyServer(t, "testuser", "testpass", "")

	// First executing a simple one directly over the mock server with a little validation
	// NOTE: Previously we used it to compare with proxyssh output, but multiple variables made it
	// very unstable, so I leave it for now here commented and check just for echo output
	//var sshdTestOutput string
	t.Run("Executing SSH shell directly on mock SSHD", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(sshdHost+":"+sshdPort, "testuser", "testpass", "echo 'Its ALIVE!'")
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}
		// SSH output is full of special symbols, so looking just for the desired output
		if !strings.Contains(string(response), "\nIts ALIVE!") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q not in %q", "\nIts ALIVE!", string(response))
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
							Password: "testpass",
							Port: func() int32 {
								port := sshdPort
								if len(port) > 0 {
									// Convert string port to int32
									var portInt int32
									fmt.Sscanf(port, "%d", &portInt)
									return portInt
								}
								return 22
							}(),
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

		if appUID == "" || appUID == uuid.Nil.String() {
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

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
		resUID = resp.Msg.Data.Uid
	})

	// Now working with the created Application to get access
	var accUsername, accPassword string
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

		if resp.Msg.Data.Username == "" {
			t.Fatalf("Unable to get access to Resource: %v", resp.Msg.Data)
		}
		accUsername = resp.Msg.Data.Username
		accPassword = resp.Msg.Data.Password
	})

	// Now running the same but through proxy - and we should get the identical answer
	t.Run("Executing SSH shell through PROXYSSH", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'Its ALIVE!'")
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}
		// SSH output is full of special symbols, so looking just for the desired output
		// if string(response) != sshdTestOutput {
		if !strings.Contains(string(response), "\nIts ALIVE!") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q != %q", "\nIts ALIVE!", string(response))
		}
	})

	t.Run("Checking the PROXYSSH token could be used only once", func(t *testing.T) {
		_, err := h.RunCmdPtySSH(proxysshEndpoint, accUsername, accPassword, "echo 'Its ALIVE!'")
		if err == nil {
			t.Fatalf("Apparently PROXYSSH token could be used once more - no deal: %v", err)
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

// Test ProxySSH SCP functionality
func Test_proxyssh_scp_password2password_copy(t *testing.T) {
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
							Port: func() int32 {
								port := sshdPort
								if len(port) > 0 {
									// Convert string port to int32
									var portInt int32
									fmt.Sscanf(port, "%d", &portInt)
									return portInt
								}
								return 22
							}(),
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

		if appUID == "" || appUID == uuid.Nil.String() {
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

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
		resUID = resp.Msg.Data.Uid
	})

	// Now working with the created Application to get access
	var accUsername, accPassword string
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
		accPassword = resp.Msg.Data.Password

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
		var srcFiles []string
		if srcFiles, err = h.CreateRandomFiles(srcdir, 5); err != nil {
			t.Fatalf("Unable to generate random files: %v", err)
		}

		err = h.RunSftp(proxysshEndpoint, accUsername, accPassword, srcFiles, dstdir, false)
		if err != nil {
			t.Fatalf("Failed to copy files via PROXYSSH: %v", err)
		}

		// Compare 2 directories - they should contain identical files
		if err = h.CompareDirFiles(srcdir, dstdir); err != nil {
			t.Fatalf("Found differences in the copied files from %q to %q: %v", srcdir, dstdir, err)
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
		accPassword = resp.Msg.Data.Password

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

		err = h.RunSftp(proxysshEndpoint, accUsername, accPassword, srcFiles, dstdir, true)
		if err != nil {
			t.Fatalf("Failed to copy files via PROXYSSH: %v", err)
		}

		// Compare 2 directories - they should contain identical files
		if err = h.CompareDirFiles(srcdir, dstdir); err != nil {
			t.Fatalf("Found differences in the copied files from %q to %q: %v", srcdir, dstdir, err)
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
