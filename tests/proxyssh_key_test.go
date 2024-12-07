/**
 * Copyright 2024 Adobe. All rights reserved.
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
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Checks that proxyssh can establish ssh connection with TTY and execute there a simple command
// Client will use key and proxy will connect to target by password
// WARN: This test requires `ssh` and `sh` binary to be available in PATH
func Test_proxyssh_ssh_key2password_tty_access(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

drivers:
  - name: test`)

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
	sshPort := h.TestSSHPtyServer(t, "testuser", "testpass", "")

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","password":"testpass","port":`+sshPort+`}
			}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.UID == uuid.Nil {
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res types.Resource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/resource")).
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
	var acc types.ResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/resource/"+res.UID.String()+"/access")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
		}
	})

	t.Run("Executing SSH shell through PROXYSSH", func(t *testing.T) {
		// Writing ssh private key to temp file
		tempFile, err := os.CreateTemp("", "key")
		if err != nil {
			t.Fatalf("Unable to create temp file: %v", err)
		}
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp file: %v", err)
		}
		tempFile.Close()
		err = os.Chmod(tempFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp file mod: %v", err)
		}

		host, port, err := net.SplitHostPort(afi.ProxySSHEndpoint())

		input := bytes.NewBufferString("echo 'Its ALIVE!'\nexit\n")
		stdout, _, err := util.RunAndLog("TEST", 5*time.Second, input, "ssh",
			"-i", tempFile.Name(),
			"-p", port,
			"-tt", // We need to request PTY for server
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			host,
		)
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}

		// SSH output is full of special symbols, so looking just for the desired output
		if !strings.Contains(stdout, "\nIts ALIVE!\n") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q not in %q", "\nIts ALIVE!\n", stdout)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}

// Checks that proxyssh can establish ssh connection with TTY and execute there a simple command
// Client will use key and proxy will connect to target by key
// WARN: This test requires `ssh` and `sh` binary to be available in PATH
func Test_proxyssh_ssh_key2key_tty_access(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

drivers:
  - name: test`)

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

	serverkey, err := crypt.GenerateSSHKey()
	if err != nil {
		t.Fatalf("Can't create ssh key for mock server: %v", err)
	}
	serverpubkey, err := crypt.GetSSHPubKeyFromPem(serverkey)
	if err != nil {
		t.Fatalf("Can't create ssh key for mock server: %v", err)
	}
	serverkeyjson, err := json.Marshal(string(serverkey))
	if err != nil {
		t.Fatalf("Can't encode ssh key to json: %v", err)
	}

	// Running SSH Pty server with shell
	sshPort := h.TestSSHPtyServer(t, "testuser", "", string(serverpubkey))

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","key":`+string(serverkeyjson)+`,"port":`+sshPort+`}
			}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.UID == uuid.Nil {
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res types.Resource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/resource")).
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
	var acc types.ResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/resource/"+res.UID.String()+"/access")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
		}
	})

	t.Run("Executing SSH shell through PROXYSSH", func(t *testing.T) {
		// Writing ssh private key to temp file
		tempFile, err := os.CreateTemp("", "key")
		if err != nil {
			t.Fatalf("Unable to create temp file: %v", err)
		}
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp file: %v", err)
		}
		tempFile.Close()
		err = os.Chmod(tempFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp file mod: %v", err)
		}

		host, port, err := net.SplitHostPort(afi.ProxySSHEndpoint())

		input := bytes.NewBufferString("echo 'Its ALIVE!'\nexit\n")
		stdout, _, err := util.RunAndLog("TEST", 5*time.Second, input, "ssh",
			"-i", tempFile.Name(),
			"-p", port,
			"-tt", // We need to request PTY for server
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"-l", "admin",
			host,
		)
		if err != nil {
			t.Fatalf("Failed to execute command via PROXYSSH: %v", err)
		}

		// SSH output is full of special symbols, so looking just for the desired output
		if !strings.Contains(stdout, "\nIts ALIVE!\n") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q not in %q", "\nIts ALIVE!\n", stdout)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}

// Checks that proxyssh can copy files back and forth by scp
// WARN: This test requires `scp` binary to be available in PATH
func Test_proxyssh_scp_key2password_copy(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

drivers:
  - name: test`)

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
	sshPort := h.TestSSHSftpServer(t, "testuser", "testpass", "")

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","password":"testpass","port":`+sshPort+`}
			}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.UID == uuid.Nil {
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res types.Resource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/resource")).
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
	var acc types.ResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/resource/"+res.UID.String()+"/access")).
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
		if _, err = h.CreateRandomFiles(srcdir, 5); err != nil {
			t.Fatalf("Unable to generate random files: %v", err)
		}

		// Writing ssh private key to temp file
		tempFile, err := os.CreateTemp("", "key")
		if err != nil {
			t.Fatalf("Unable to create temp file: %v", err)
		}
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp file: %v", err)
		}
		tempFile.Close()
		err = os.Chmod(tempFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp file mod: %v", err)
		}

		host, port, err := net.SplitHostPort(afi.ProxySSHEndpoint())

		_, _, err = util.RunAndLog("TEST", 5*time.Second, nil, "scp",
			"-i", tempFile.Name(),
			"-P", port,
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
			"admin@" + host + ":" + srcdir + "/*",
			dstdir,
		)
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
			Get(afi.APIAddress("api/v1/resource/"+res.UID.String()+"/access")).
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

		// Writing ssh private key to temp file
		tempFile, err := os.CreateTemp("", "key")
		if err != nil {
			t.Fatalf("Unable to create temp file: %v", err)
		}
		defer os.Remove(tempFile.Name())
		_, err = tempFile.WriteString(acc.Key)
		if err != nil {
			t.Fatalf("Unable to write temp file: %v", err)
		}
		tempFile.Close()
		err = os.Chmod(tempFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp file mod: %v", err)
		}

		host, port, err := net.SplitHostPort(afi.ProxySSHEndpoint())

		args := []string{
			"-i", tempFile.Name(),
			"-P", port,
			"-oStrictHostKeyChecking=no",
			"-oUserKnownHostsFile=/dev/null",
			"-oGlobalKnownHostsFile=/dev/null",
		}
		args = append(args, srcFiles...)
		args = append(args, "admin@" + host + ":" + dstdir)

		_, _, err = util.RunAndLog("TEST", 5*time.Second, nil, "scp", args...)
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
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}
