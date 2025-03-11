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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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
	_, sshdPort := h.MockSSHPtyServer(t, "testuser", "testpass", "")

	var label types.Label
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

	var res types.ApplicationResource
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
	var acc types.ApplicationResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.UID.String()+"/access")).
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

		proxyHost, proxyPort, err := net.SplitHostPort(afi.ProxySSHEndpoint())

		// In order to emulate terminal input we using pipe to write. This allows us to keep the
		// stdin opened while we woking with ssh app, otherwise something like
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
		if !strings.Contains(stdout, "Its ALIVE!\n") {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q not in %q (stderr: %s)", "Its ALIVE!\n", stdout, stderr)
			//} else {
			//	t.Log(fmt.Sprintf("Correct response from command through PROXYSSH: %q in %q (stderr: %s)", "Its ALIVE!\n", stdout, stderr))
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

	sshdKey, err := crypt.GenerateSSHKey()
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}
	sshdPubKey, err := crypt.GetSSHPubKeyFromPem(sshdKey)
	if err != nil {
		t.Fatalf("Can't create ssh key for mock sshd: %v", err)
	}
	sshdKeyJsonStr, err := json.Marshal(string(sshdKey))
	if err != nil {
		t.Fatalf("Can't encode ssh key to json: %v", err)
	}

	// Running mock SSH Pty server with shell
	sshdHost, sshdPort := h.MockSSHPtyServer(t, "testuser", "", string(sshdPubKey))

	// First executing a simple one directly over the mock server with a little validation
	var sshdTestOutput string
	t.Run("Executing SSH shell directly on mock SSHD", func(t *testing.T) {
		// Writing ssh private key to temp file
		sshdKeyFile, err := os.CreateTemp("", "sshdkey")
		if err != nil {
			t.Fatalf("Unable to create temp sshdkey file: %v", err)
		}
		defer os.Remove(sshdKeyFile.Name())
		_, err = sshdKeyFile.Write(sshdKey)
		if err != nil {
			t.Fatalf("Unable to write temp sshdkey file: %v", err)
		}
		sshdKeyFile.Close()
		err = os.Chmod(sshdKeyFile.Name(), 0600)
		if err != nil {
			t.Fatalf("Unable to change temp sshdkey file mod: %v", err)
		}

		// In order to emulate terminal input we using pipe to write. This allows us to keep the
		// stdin opened while we woking with ssh app, otherwise something like
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
		sshdTestOutput = stdout
	})

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","key":`+string(sshdKeyJsonStr)+`,"port":`+sshdPort+`}
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

	var res types.ApplicationResource
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
	var acc types.ApplicationResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.UID.String()+"/access")).
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

		proxyHost, proxyPort, err := net.SplitHostPort(afi.ProxySSHEndpoint())

		// In order to emulate terminal input we using pipe to write. This allows us to keep the
		// stdin opened while we woking with ssh app, otherwise something like
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
		if stdout != sshdTestOutput {
			t.Fatalf("Incorrect response from command through PROXYSSH: %q != %q (stderr: %s)", sshdTestOutput, stdout, stderr)
			//} else {
			//	t.Log(fmt.Printf("Correct response from command through PROXYSSH: %q == %q (stderr: %s)", sshdTestOutput, stdout, stderr))
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
	_, sshdPort := h.MockSSHSftpServer(t, "testuser", "testpass", "")

	var label types.Label
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

	var res types.ApplicationResource
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
	var acc types.ApplicationResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.UID.String()+"/access")).
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

		proxyHost, proxyPort, err := net.SplitHostPort(afi.ProxySSHEndpoint())

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
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.UID.String()+"/access")).
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

		proxyHost, proxyPort, err := net.SplitHostPort(afi.ProxySSHEndpoint())

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

// Checks that proxyssh can forward port back and forth and the API becomes available on it
// Client will use key and proxy will connect to target by key
// WARN: This test requires `ssh` binary to be available in PATH
func Test_proxyssh_port_key2key(t *testing.T) {
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

	// Running SSH Port server
	_, sshdPort := h.MockSSHPortServer(t, "testuser", "", string(serverpubkey))

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{
				"driver":"test",
				"resources":{"cpu":1,"ram":2},
				"authentication":{"username":"testuser","key":`+string(serverkeyjson)+`,"port":`+sshdPort+`}
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

	var res types.ApplicationResource
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
	var acc types.ApplicationResourceAccess
	t.Run("Requesting access to the Application Resource", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/applicationresource/"+res.UID.String()+"/access")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
		}
	})

	t.Run("Executing SSH port forward pass through PROXYSSH", func(t *testing.T) {
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

		proxyHost, proxyPort, err := net.SplitHostPort(afi.ProxySSHEndpoint())
		_, apiPort, err := net.SplitHostPort(afi.APIEndpoint())
		// Picking semi-random port to listen on
		proxyApiPort, _ := strconv.Atoi(apiPort)
		proxyApiPort += 10

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
		t.Log("DEBUG: Executing:", strings.Join(cmd.Args, " "), acc.Password, string(serverkey))

		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Start()

		// Wait for ssh port passthrough startup
		time.Sleep(2 * time.Second)

		// Requesting Fish API through proxied port for the next test
		apitest.New().
			EnableNetworking(cli).
			Get("https://127.0.0.1:"+strconv.Itoa(proxyApiPort)+"/api/v1/applicationresource/"+res.UID.String()+"/access").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&acc)

		if res.Identifier == "" {
			t.Fatalf("Unable to get access to Resource: %v", res.Identifier)
		}
	})

	// TODO: For some reason mock server does not accept reverse port forwarding, but
	// I spent too much time on that already, so the direct forwarding enough for testing now
	/*t.Run("Executing SSH port reverse pass through PROXYSSH", func(t *testing.T) {
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

		proxyHost, proxyPort, err := net.SplitHostPort(afi.ProxySSHEndpoint())
		_, apiPort, err := net.SplitHostPort(afi.APIEndpoint())
		// Picking semi-random port to listen on
		proxyApiPort, _ := strconv.Atoi(apiPort)
		proxyApiPort += 10

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
			"-R", strconv.Itoa(proxyApiPort)+":localhost:"+apiPort,
			proxyHost,
		)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Start()

		// Wait for ssh port passthrough startup
		time.Sleep(2*time.Second)

		// Requesting Fish API through proxied port
		apitest.New().
			EnableNetworking(cli).
			Get("https://127.0.0.1:"+strconv.Itoa(proxyApiPort)+"/api/v1/application/"+app.UID.String()+"/resource").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)
	})*/

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
