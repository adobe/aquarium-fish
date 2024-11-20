/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package native

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
)

func generateUniqueUsername(user string) string {
	// Id if the resource is the username created from "fish-" prefix and 6 a-z random chars
	// WARNING: sudoers file is tied up to this format of username, so please avoid the changes
	user = "fish-" + crypt.RandStringCharset(6, crypt.RandStringCharsetAZ)
	return user
}

// Returns the total resources available for the node after alteration
func (d *Driver) getAvailResources() (availCPU, availRAM uint) {
	if d.cfg.CPUAlter < 0 {
		availCPU = d.totalCPU - uint(-d.cfg.CPUAlter)
	} else {
		availCPU = d.totalCPU + uint(d.cfg.CPUAlter)
	}

	if d.cfg.RAMAlter < 0 {
		availRAM = d.totalRAM - uint(-d.cfg.RAMAlter)
	} else {
		availRAM = d.totalRAM + uint(d.cfg.RAMAlter)
	}

	return
}

// Load images and unpack them according the tags
func (d *Driver) loadImages(user string, images []drivers.Image, diskPaths map[string]string) error {
	var wg sync.WaitGroup
	for _, image := range images {
		log.Info("Native: Loading the required image:", image.Name, image.Version, image.URL)

		// Running the background routine to download, unpack and process the image
		wg.Add(1)
		go func(image drivers.Image) {
			defer wg.Done()
			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				log.Error("Native: Unable to download and unpack the image:", image.Name, image.URL, err)
			}
		}(image)
	}

	log.Debug("Native: Wait for all the background image processes to be done...")
	wg.Wait()

	// The images have to be processed sequentially - child images could override the parent files
	for _, image := range images {
		imageUnpacked := filepath.Join(d.cfg.ImagesPath, image.Name+"-"+image.Version)

		// Getting the image subdir name in the unpacked dir
		subdir := ""
		items, err := os.ReadDir(imageUnpacked)
		if err != nil {
			return log.Error("Native: Unable to read the unpacked directory:", imageUnpacked, err)
		}
		for _, f := range items {
			if strings.HasPrefix(f.Name(), image.Name) {
				if f.Type()&fs.ModeSymlink != 0 {
					// Potentially it can be a symlink (like used in local tests)
					if _, err := os.Stat(filepath.Join(imageUnpacked, f.Name())); err != nil {
						log.Warn("Native: The image symlink is broken:", f.Name(), err)
						continue
					}
				}
				subdir = f.Name()
				break
			}
		}
		if subdir == "" {
			log.Errorf("Native: Unpacked image '%s' has no subfolder '%s', only: %q", imageUnpacked, image.Name, items)
			return fmt.Errorf("Native: The image was unpacked incorrectly, please check log for the errors")
		}

		// Unpacking the image according its specified tag. If tag is empty - unpacks to home dir,
		// otherwise if tag exists in the disks map - then use its path to unpack there
		imageArchive := filepath.Join(imageUnpacked, subdir, image.Name+".tar")
		unpackPath, ok := diskPaths[image.Tag]
		if !ok {
			return log.Error("Native: Unable to find where to unpack the image:", image.Tag, imageArchive, err)
		}

		unpackForPlatform(user, err, imageArchive, unpackPath, d)
	}

	log.Info("Native: The images are processed.")

	return nil
}

func processTemplate(tplData *EnvData, value string) (string, error) {
	if tplData == nil {
		return value, nil
	}
	tmpl, err := template.New("").Parse(value)
	// Yep, still could fail here for example due to the template vars are not here
	if err != nil {
		return "", fmt.Errorf("Native: Unable to parse template: %v, %v", value, err)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, *tplData)
	if err != nil {
		return "", fmt.Errorf("Native: Unable to execute template: %v, %v", value, err)
	}

	return buf.String(), nil
}

// Runs & logs the executable command
func runAndLog(timeout time.Duration, stdin io.Reader, path string, arg ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	// Running command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, arg...)

	log.Debug("Native: Executing:", cmd.Path, strings.Join(cmd.Args[1:], " "))
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
		err = fmt.Errorf("Native: Command timed out")
	} else if _, ok := err.(*exec.ExitError); ok {
		message := stderrString
		if message == "" {
			message = stdoutString
		}

		err = fmt.Errorf("Native: Command exited with error: %v: %s", err, message)
	}

	if len(stdoutString) > 0 {
		log.Debug("Native: stdout:", stdoutString)
	}
	if len(stderrString) > 0 {
		log.Debug("Native: stderr:", stderrString)
	}

	// Replace these for Windows, we only want to deal with Unix style line endings.
	returnStdout := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
	returnStderr := strings.ReplaceAll(stderr.String(), "\r\n", "\n")

	return returnStdout, returnStderr, err
}

// Will retry on error and store the retry output and errors to return
func runAndLogRetry(retry int, timeout time.Duration, stdin io.Reader, path string, arg ...string) (stdout string, stderr string, err error) { //nolint:unparam
	counter := 0
	for {
		counter++
		rout, rerr, err := runAndLog(timeout, stdin, path, arg...)
		if err != nil {
			stdout += fmt.Sprintf("\n--- Fish: Command execution attempt %d ---\n", counter)
			stdout += rout
			stderr += fmt.Sprintf("\n--- Fish: Command execution attempt %d ---\n", counter)
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
