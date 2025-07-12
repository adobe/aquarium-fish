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

package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Runs & logs the executable command
func RunAndLog(section string, timeout time.Duration, stdin io.Reader, path string, arg ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	// Running command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, arg...)

	logger := log.WithFunc(section, "RunAndLog")
	logger.Debug("Executing", "cmd", cmd.Path, "args", strings.Join(cmd.Args[1:], " "))
	if stdin != nil {
		cmd.Stdin = stdin
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	stdoutString := strings.TrimSpace(stdout.String())
	stderrString := strings.TrimSpace(stderr.String())

	// Check the context error to see if the timeout was executed
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("%s: Command timed out", section)
	} else if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("%s: Command exited with error: %v: %s", section, err, message)
	}

	if len(stdoutString) > 0 {
		logger.Debug("stdout:", "stdout", stdoutString)
	}
	if len(stderrString) > 0 {
		logger.Debug("stderr: %s", "stderr", stderrString)
	}

	// Replace these for Windows, we only want to deal with Unix style line endings.
	returnStdout := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	returnStderr := strings.ReplaceAll(stderr.String(), "\r\n", "\n")

	return returnStdout, returnStderr, err
}

// Will retry on error and store the retry output and errors to return
func RunAndLogRetry(section string, retry int, timeout time.Duration, stdin io.Reader, path string, arg ...string) (stdout string, stderr string, err error) {
	counter := 0
	for {
		counter++
		rout, rerr, err := RunAndLog(section, timeout, stdin, path, arg...)
		if err != nil {
			stdout += fmt.Sprintf("\n--- %s: Command execution attempt %d ---\n", section, counter)
			stdout += rout
			stderr += fmt.Sprintf("\n--- %s: Command execution attempt %d ---\n", section, counter)
			stderr += rerr
			if counter <= retry {
				// Give command time to rest
				time.Sleep(time.Duration(counter) * time.Second)
				continue
			}
		}
		return stdout, stderr, err
	}
}
