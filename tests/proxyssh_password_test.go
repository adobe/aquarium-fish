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
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Checks that proxyssh can establish ssh connection with TTY and execute there a simple command
// Client will use password and proxy will connect to target by password
// WARN: This test requires `sh` binary to be available in PATH
func Test_proxyssh_ssh_password2password_tty_access(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
    proxyssh:
      bind_address: 127.0.0.1:0
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Still need HTTPS client to request SSH access to the machine
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: tr,
	}

	// Running SSH Pty server with shell
	sshdHost, sshdPort := h.MockSSHPtyServer(t, "testuser", "testpass", "")

	// First executing a simple one directly over the mock server with a little validation
	var sshdTestOutput string
	t.Run("Executing SSH shell directly on mock SSHD", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(sshdHost+":"+sshdPort, "testuser", "testpass", "echo 'Its ALIVE!'")
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}
		// SSH output is full of special symbols, so looking just for the desired output
		if !strings.Contains(string(response), "Its ALIVE!\r\n") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q not in %q", "\r\nIts ALIVE!\r\n", string(response))
		}
		sshdTestOutput = string(response)
	})

	var label aquariumv2.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","password":"testpass","port":`+sshdPort+`}
			}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label.Uid)
		}
	})

	var app aquariumv2.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.Uid+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.Uid == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", app.Uid)
		}
	})

	var appState aquariumv2.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res aquariumv2.ApplicationResource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/resource")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)

		if res.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", res.Identifier)
		}
	})

	// Now working with the created Application to get access
	var acc aquariumv2.GateProxySSHAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.Uid+"/access")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
		}
	})

	// Now running the same but through proxy - and we should get the identical answer
	t.Run("Executing SSH shell through PROXYSSH", func(t *testing.T) {
		response, err := h.RunCmdPtySSH(afi.ProxySSHEndpoint(), acc.Username, acc.Password, "echo 'Its ALIVE!'")
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}
		// SSH output is full of special symbols, so looking just for the desired output
		if string(response) != sshdTestOutput {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q != %q", sshdTestOutput, string(response))
		}
	})

	t.Run("Checking the PROXYSSH token could be used only once", func(t *testing.T) {
		_, err := h.RunCmdPtySSH(afi.ProxySSHEndpoint(), acc.Username, acc.Password, "echo 'Its ALIVE!'")
		if err == nil {
			t.Fatalf("Apparently PROXYSSH token could be used once more - no deal: %v", err)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}

// Checks that proxyssh can copy files back and forth by scp
// Client will use password and proxy will connect to target by password
func Test_proxyssh_scp_password2password_copy(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
    proxyssh:
      bind_address: 127.0.0.1:0
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Still need HTTPS client to request SSH access to the machine
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: tr,
	}

	// Running SSH Sftp server with shell
	_, sshdPort := h.MockSSHSftpServer(t, "testuser", "testpass", "")

	var label aquariumv2.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","password":"testpass","port":`+sshdPort+`}
			}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label.Uid)
		}
	})

	var app aquariumv2.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.Uid+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.Uid == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", app.Uid)
		}
	})

	var appState aquariumv2.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res aquariumv2.ApplicationResource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/resource")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)

		if res.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", res.Identifier)
		}
	})

	// Now working with the created Application to get access
	var acc aquariumv2.GateProxySSHAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.Uid+"/access")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
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

		err = h.RunSftp(afi.ProxySSHEndpoint(), acc.Username, acc.Password, srcFiles, dstdir, false)
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
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.Uid+"/access")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
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

		err = h.RunSftp(afi.ProxySSHEndpoint(), acc.Username, acc.Password, srcFiles, dstdir, true)
		if err != nil {
			t.Fatalf("Failed to copy files via PROXYSSH: %v", err)
		}

		// Compare 2 directories - they should contain identical files
		if err = h.CompareDirFiles(srcdir, dstdir); err != nil {
			t.Fatalf("Found differences in the copied files from %q to %q: %v", srcdir, dstdir, err)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}
