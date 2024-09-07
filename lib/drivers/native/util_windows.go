//go:build windows

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
	"fmt"
	"github.com/alessio/shellescape"
	"os"
	"os/exec"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func unpackForPlatform(user string, err error, imageArchive string, unpackPath string, d *Driver) (error, bool) {
	// Since the image is under Fish node control and user could have no read access to the file
	// it's a good idea to use stdin of the tar command to unpack properly.
	f, err := os.Open(imageArchive)
	if err != nil {
		return log.Error("Native: Unable to read the image:", imageArchive, err), true
	}
	log.Info("Native: Unpacking image:", user, imageArchive, unpackPath)
	_, _, err = runAndLog(5*time.Minute, f, "-n", d.cfg.TarPath, "-xf", "-C", unpackPath+"/")
	f.Close()
	if err != nil {
		return log.Error("Native: Unable to unpack the image:", imageArchive, err), true
	}
	return nil, false
}

func isEnvAllocated(user string) bool {
	return true
}

// Create the new user to run workload from its name
// Don't forget to deleteUser if operation fails
func userCreate(c *Config, groups []string) (user, homedir string, err error) {
	user = generateUniqueUsername(user)

	if _, _, err = runAndLog(5*time.Second, nil, "cmd", "/c", "net", "user", user, "/add", "/fullname:"+"\"Aquarium Fish env user\""); err != nil {
		err = log.Error("Native: Error user set RealName:", err)
		return
	}

	// If there are other groups required - add user to them too
	if len(groups) > 1 {
		for _, group := range groups[1:] {
			if _, _, err = runAndLog(5*time.Second, nil, "cmd", "/c", "net", "localgroup", group, user, "/add"); err != nil {
				err = log.Error("Native: Unable to add user to group:", group, err)
				return
			}
		}
	}

	// Creates the home directory because it must exist before setting it as a users homedir
	if err = os.MkdirAll(homedir, 0o750); err != nil {
		err = log.Error("Native: Unable to create the user home directory:", err)
	}
	if _, _, err = runAndLog(30*time.Second, nil, "cmd", "/c", "net", "user", user, "/homedir:"+"\"homedir\""); err != nil {
		err = log.Error("Native: Unable to set the user home directory:", err)
		return
	}

	return
}

// Runs the executable as defined user
func userRun(c *Config, envData *EnvData, user, entry string, metadata map[string]any) (err error) {
	// Entry value could contain template data
	var tmpData string
	if tmpData, err = processTemplate(envData, entry); err != nil {
		return log.Error("Native: Unable to process `entry` template:", entry, err)
	}
	entry = tmpData

	// Metadata values could contain template data
	envVars := []string{}
	for key, val := range metadata {
		if tmpData, err = processTemplate(envData, fmt.Sprintf("%v", val)); err != nil {
			return log.Errorf("Native: Unable to process metadata `%s` template: %v", key, err)
		}
		// Add to the map of the variables to store
		envVars = append(envVars, fmt.Sprintf("%v=%v", key, val))
	}

	// Prepare the command to execute entry from user home directory
	cmd := exec.Command("cmd.exe", "/c", "runas", "/user:"+user, "-c", "\""+shellescape.StripUnsafe(entry)+"\"") // #nosec G204
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, envVars...)
	if envData != nil && envData.Disks != nil {
		if _, ok := envData.Disks[""]; ok {
			cmd.Dir = envData.Disks[""]
		}
	}

	// Printing stdout/stderr with proper prefix
	cmd.Stdout = &util.StreamLogMonitor{
		Prefix: fmt.Sprintf("%s: ", user),
	}
	cmd.Stderr = &util.StreamLogMonitor{
		Prefix: fmt.Sprintf("%s: ", user),
	}

	// Run the process in background, it should live even when the Fish node is down
	if err = cmd.Start(); err != nil {
		return log.Error("Native: Unable to run the process:", err)
	}
	// TODO: Probably I should run cmd.Wait to make sure the captured OS resources are released,
	// but not sure about that... Maybe create a goroutine that will sit and wait there?

	log.Debugf("Native: Started entry for user %q in directory %q with PID %d: %s", user, cmd.Dir, cmd.Process.Pid, entry)

	// Giving the process 1 second to read the env file and not die from some unexpected error
	time.Sleep(time.Second)
	if cmd.Err != nil {
		err = log.Error("Native: The process ended quickly with error:", user, cmd.Err)
	}

	if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		err = log.Error("Native: The process ended quickly with non-zero exit code:", user, cmd.ProcessState.ExitCode(), cmd.ProcessState.Pid(), cmd.ProcessState.SystemTime(), cmd.ProcessState.UserTime(), cmd.ProcessState.String())
	}

	return err
}

// Stop the user processes
func userStop(c *Config, user string) (outErr error) { //nolint:unparam
	// In theory we can use `sysadminctl -deleteUser` command instead, which is also stopping all the
	// user processes and cleans up the home dir, but it asks for elevated previleges so not sure how
	// useful it will be in automation...

	// Note: some operations may fail, but they should not interrupt the whole cleanup process

	// Interrupt all the user processes
	taskUserFilter := fmt.Sprintf("%q", "USERNAME eq "+user)
	if _, _, err := runAndLog(5*time.Second, nil, "taskkill", "/F", "/FI", taskUserFilter); err != nil {
		log.Debug("Native: Unable to interrupt the user apps:", user, err)
	}
	// Check if no apps are running after interrupt - ps will end up with error if there is none apps left
	if _, _, err := runAndLog(5*time.Second, nil, "tasklist", "/FI", taskUserFilter); err == nil {
		// Some apps are still running - give them 5 seconds to complete their processes
		time.Sleep(5 * time.Second)
		if _, _, err := runAndLog(5*time.Second, nil, "taskkill", "/F", "/FI", taskUserFilter); err != nil {
			log.Warn("Native: Unable to kill the user apps:", user, err)
		}
	}

	return
}

// Delete user and clean up
func userDelete(c *Config, user string) (outErr error) {
	// Stopping the processes because they could cause user lock
	outErr = userStop(c, user)

	// Sometimes delete of the user could not be done due to os blocking it, so retrying 5 times
	if _, _, err := runAndLogRetry(5, 5*time.Second, nil, "cmd", "/c", "net", "user", user, "/delete"); err != nil {
		outErr = log.Error("Native: Unable to delete user:", err)
	}

	if _, _, err := runAndLog(5*time.Second, nil, "powershell", "-C", "Remove-Item", "C:\\Users\\"+user, "-Recurse", "-Force"); err != nil {
		outErr = log.Error("Native: Unable to remove the user home directory:", err)
	}

	return
}

// Unmount user volumes and delete the disk files
func disksDelete(c *Config, user string) (outErr error) {
	//TODO: Implement disksDelete for windows
	return outErr
}

// Creates disks directories described by the disks map, returns the map of disks to mount paths
func (d *Driver) disksCreate(user string, disks map[string]types.ResourcesDisk) (map[string]string, error) {
	//TODO: Implement disksCreate for windows
	return nil, nil
}
