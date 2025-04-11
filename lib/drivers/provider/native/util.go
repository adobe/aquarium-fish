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
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/alessio/shellescape"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers/provider"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Common lock to properly acquire unique User ID
var userCreateLock sync.Mutex

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
func (d *Driver) loadImages(user string, images []provider.Image, diskPaths map[string]string) error {
	var wg sync.WaitGroup
	for _, image := range images {
		log.Info("Native: Loading the required image:", image.Name, image.Version, image.URL)

		// Running the background routine to download, unpack and process the image
		wg.Add(1)
		go func(image provider.Image) {
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

		// Since the image is under Fish node control and user could have no read access to the file
		// it's a good idea to use stdin of the tar command to unpack properly.
		f, err := os.Open(imageArchive)
		if err != nil {
			return log.Error("Native: Unable to read the image:", imageArchive, err)
		}
		log.Info("Native: Unpacking image:", user, imageArchive, unpackPath)
		_, _, err = util.RunAndLog("NATIVE", 5*time.Minute, f, d.cfg.SudoPath, "-n", d.cfg.TarPath, "-xf", "-", "--uname", user, "-C", unpackPath+"/")
		f.Close()
		if err != nil {
			return log.Error("Native: Unable to unpack the image:", imageArchive, err)
		}
	}

	log.Info("Native: The images are processed.")

	return nil
}

func isEnvAllocated(user string) bool {
	_, err := os.Stat("/Users/" + user)
	return !os.IsNotExist(err)
}

// Create the new user to run workload from it's name
// Don't forget to deleteUser if operation fails
func userCreate(c *Config, groups []string) (user, homedir string, err error) {
	// Id if the resource is the user name created from fish- prefix and 6 a-z random chars
	// WARNING: sudoers file is tied up to this format of user name, so please avoid the changes
	user = "fish-" + crypt.RandStringCharset(6, crypt.RandStringCharsetAZ)

	// In theory we can use `sysadminctl -addUser` command instead, but it asks for elevated previleges
	// so not sure how useful it will be in automation...

	if _, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "RealName", "Aquarium Fish env user"); err != nil {
		err = log.Error("Native: Error user set RealName:", err)
		return
	}

	// Configure default shell
	if _, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "UserShell", c.ShPath); err != nil {
		err = log.Error("Native: Error user set UserShell:", err)
		return
	}

	// Choose the UniqueID for the new user
	userCreateLock.Lock()
	{
		// Locate the unassigned user id
		var stdout string
		if stdout, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.DsclPath, ".", "list", "/Users", "UniqueID"); err != nil {
			userCreateLock.Unlock()
			err = log.Error("Native: Unable to list directory users:", err)
			return
		}

		// Finding the max user id in the OS
		userID := int64(1000) // Min 1000 is ok for most of the unix systems
		splitStdout := strings.Split(strings.TrimSpace(stdout), "\n")
		for _, line := range splitStdout {
			lineID := line[strings.LastIndex(line, " ")+1:]
			lineIDNum, err := strconv.ParseInt(lineID, 10, 64)
			if err != nil {
				log.Warnf("Native: Unable to parse user id from line: %q", line)
				continue
			}
			if lineIDNum > userID {
				userID = lineIDNum
			}
		}

		// Increment max user id and use it as unique id for new user
		if _, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "UniqueID", fmt.Sprint(userID+1)); err != nil {
			userCreateLock.Unlock()
			err = log.Error("Native: Unable to set user UniqueID:", err)
			return
		}
	}
	userCreateLock.Unlock()

	// Locate the primary user group id
	primaryGroup, e := osuser.LookupGroup(groups[0])
	if e != nil {
		err = log.Error("Native: Unable to locate group GID for:", groups[0], e)
		return
	}

	// Set user primary group
	if _, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "PrimaryGroupID", primaryGroup.Gid); err != nil {
		err = log.Error("Native: Unable to set user PrimaryGroupID:", err)
		return
	}

	// If there are other groups required - add user to them too
	if len(groups) > 1 {
		for _, group := range groups[1:] {
			if _, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "append", "/Groups/"+group, "GroupMembership", user); err != nil {
				err = log.Error("Native: Unable to add user to group:", group, err)
				return
			}
		}
	}

	// Set the default home directory
	homedir = filepath.Join("/Users", user)
	if _, _, err = util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "NFSHomeDirectory", homedir); err != nil {
		err = log.Error("Native: Unable to set user NFSHomeDirectory:", err)
		return
	}

	// Populate the default user home directory
	if _, _, err = util.RunAndLog("NATIVE", 30*time.Second, nil, c.SudoPath, "-n", c.CreatehomedirPath, "-c", "-u", user); err != nil {
		err = log.Error("Native: Unable to populate the default user directory:", err)
		return
	}

	return
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

// Runs the executable as defined user
func userRun(c *Config, envData *EnvData, user, entry string, metadata map[string]any) (err error) {
	// Entry value could contain template data
	var tmpData string
	if tmpData, err = processTemplate(envData, entry); err != nil {
		return log.Error("Native: Unable to process `entry` template:", entry, err)
	}
	entry = tmpData

	// Metadata values could contain template data
	envVars := make(map[string]any)
	for key, val := range metadata {
		if tmpData, err = processTemplate(envData, fmt.Sprintf("%v", val)); err != nil {
			return log.Errorf("Native: Unable to process metadata `%s` template: %v", key, err)
		}
		// Add to the map of the variables to store
		envVars[key] = tmpData
	}

	// Unfortunately passing the environment through the cmd.Env and sudo/su is not that easy, so
	// using a temp file instead, which is removed right after the entry is started.
	envFileData, err := util.SerializeMetadata("export", "", envVars)
	if err != nil {
		return log.Errorf("Native: Unable to serialize metadata into 'export' format: %v", err)
	}
	// Using common /tmp dir available for each user in the system
	envFile, err := os.CreateTemp("/tmp", "*.metadata.sh")
	if err != nil {
		return log.Error("Native: Unable to create temp env file:", err)
	}
	defer os.Remove(envFile.Name())
	if _, err := envFile.Write(envFileData); err != nil {
		return log.Error("Native: Unable to write temp env file:", err)
	}
	if err := envFile.Close(); err != nil {
		return log.Error("Native: Unable to close temp env file:", err)
	}

	// Add ACL permission to the env file to allow to read it by unprevileged user
	if _, _, err := util.RunAndLogRetry("NATIVE", 5, 5*time.Second, nil, c.ChmodPath, "+a", fmt.Sprintf("user:%s:allow read,readattr,readextattr,readsecurity", user), envFile.Name()); err != nil {
		return log.Error("Native: Unable to set ACL for temp env file:", err)
	}

	// Prepare the command to execute entry from user home directory
	shellLine := fmt.Sprintf("source %s; %s", envFile.Name(), shellescape.Quote(shellescape.StripUnsafe(entry)))
	cmd := exec.Command(c.SudoPath, "-n", c.SuPath, "-l", user, "-c", shellLine) // #nosec G204
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

	log.Debugf("Native: Started entry for user %q in directory %q with PID %d: %s", user, cmd.Dir, cmd.Process.Pid, shellLine)

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
	if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.KillallPath, "-INT", "-u", user); err != nil {
		log.Debug("Native: Unable to interrupt the user apps:", user, err)
	}
	// Check if no apps are running after interrupt - ps will end up with error if there is none apps left
	if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, "ps", "-U", user); err == nil {
		// Some apps are still running - give them 5 seconds to complete their processes
		time.Sleep(5 * time.Second)
		if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.KillallPath, "-KILL", "-u", user); err != nil {
			log.Warn("Native: Unable to kill the user apps:", user, err)
		}
	}

	return
}

// Delete user and clean up
func userDelete(c *Config, user string) (outErr error) {
	// Stopping the processes because they could cause user lock
	outErr = userStop(c, user)

	// Sometimes delete of the user could not be done due to MacOS blocking it, so retrying 5 times
	// Native: Command exited with error: exit status 40: <main> delete status: eDSPermissionError <dscl_cmd> DS Error: -14120 (eDSPermissionError)
	if _, _, err := util.RunAndLogRetry("NATIVE", 5, 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "delete", "/Users/"+user); err != nil {
		outErr = log.Error("Native: Unable to delete user:", err)
	}

	if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, c.SudoPath, "-n", c.RmPath, "-rf", "/Users/"+user); err != nil {
		outErr = log.Error("Native: Unable to remove the user home directory:", err)
	}

	return
}

// Unmount user volumes and delete the disk files
func disksDelete(c *Config, user string) (outErr error) {
	// Stopping the processes because they could cause user lock
	outErr = userStop(c, user)

	// Getting the list of the mounted volumes
	volumes, err := os.ReadDir("/Volumes")
	if err != nil {
		outErr = log.Error("Native: Unable to list mounted volumes:", err)
	}
	envVolumes := []string{}
	for _, file := range volumes {
		if file.IsDir() && strings.HasPrefix(file.Name(), user) {
			envVolumes = append(envVolumes, filepath.Join("/Volumes", file.Name()))
		}
	}

	// Umount the disk volumes if needed
	mounts, _, err := util.RunAndLog("NATIVE", 3*time.Second, nil, c.MountPath)
	if err != nil {
		outErr = log.Error("Native: Unable to list the mount points:", user, err)
	}
	for _, volPath := range envVolumes {
		if strings.Contains(mounts, volPath) {
			if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, c.HdiutilPath, "detach", volPath); err != nil {
				outErr = log.Error("Native: Unable to detach the volume disk:", user, volPath, err)
			}
		}
	}

	// Cleaning the env work directory with disks
	workspacePath := filepath.Join(c.WorkspacePath, user)
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		if err := os.RemoveAll(workspacePath); err != nil {
			outErr = log.Error("Native: Unable to remove user env workspace:", user, err)
		}
	}

	return
}

// Creates disks directories described by the disks map, returns the map of disks to mount paths
func (d *Driver) disksCreate(user string, disks map[string]types.ResourcesDisk) (map[string]string, error) {
	// Create disks
	diskPaths := make(map[string]string, len(disks))

	for dName, disk := range disks {
		diskPath := filepath.Join(d.cfg.WorkspacePath, user, "disk-"+dName)
		if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
			return diskPaths, err
		}

		// Create disk
		// TODO: Ensure failures doesn't leave the changes behind (like mounted disks or files)

		if disk.Type == "dir" {
			if err := os.MkdirAll(diskPath, 0o777); err != nil {
				return diskPaths, err
			}
			diskPaths[dName] = diskPath
			// TODO: Validate the available disk space for disk.Size
			continue
		}

		// Create virtual disk in order to restrict the disk space
		dmgPath := diskPath + ".dmg"

		label := dName
		if disk.Label != "" {
			// Label can be used as mount point so cut the path separator out
			label = strings.ReplaceAll(disk.Label, "/", "")
		} else {
			disk.Label = label
		}

		// Do not recreate the disk if it is exists
		if _, err := os.Stat(dmgPath); os.IsNotExist(err) {
			args := []string{"create", dmgPath,
				"-fs", "HFS+",
				"-layout", "NONE",
				"-volname", label,
				"-size", fmt.Sprintf("%dm", disk.Size*1024),
			}
			if _, _, err := util.RunAndLog("NATIVE", 10*time.Minute, nil, d.cfg.HdiutilPath, args...); err != nil {
				return diskPaths, log.Error("Native: Unable to create dmg disk:", dmgPath, err)
			}
		}

		mountPoint := filepath.Join("/Volumes", fmt.Sprintf("%s_%s", user, dName))

		// Attach & mount disk
		if _, _, err := util.RunAndLog("NATIVE", 10*time.Second, nil, d.cfg.HdiutilPath, "attach", dmgPath, "-owners", "on", "-mountpoint", mountPoint); err != nil {
			return diskPaths, log.Error("Native: Unable to attach dmg disk:", dmgPath, mountPoint, err)
		}

		// Change the owner of the volume to user
		if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, d.cfg.SudoPath, "-n", d.cfg.ChownPath, "-R", user+":staff", mountPoint+"/"); err != nil {
			return diskPaths, fmt.Errorf("Native: Error user disk mount path chown: %v", err)
		}

		// (Optional) Disable spotlight for the mounted volume
		if _, _, err := util.RunAndLog("NATIVE", 5*time.Second, nil, d.cfg.SudoPath, d.cfg.MdutilPath, "-i", "off", mountPoint+"/"); err != nil {
			log.Warn("Native: Unable to disable spotlight for the volume:", mountPoint, err)
		}

		diskPaths[dName] = mountPoint
	}

	return diskPaths, nil
}
