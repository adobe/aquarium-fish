/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/util"
)

// Common lock to properly acquire unique User ID
var userCreateLock sync.Mutex

// Returns the total resources available for the node after alteration
func (d *Driver) getAvailResources() (availCPU, availRAM uint32) {
	if d.cfg.CPUAlter < 0 {
		availCPU = d.totalCPU - uint32(-d.cfg.CPUAlter)
	} else {
		availCPU = d.totalCPU + uint32(d.cfg.CPUAlter)
	}

	if d.cfg.RAMAlter < 0 {
		availRAM = d.totalRAM - uint32(-d.cfg.RAMAlter)
	} else {
		availRAM = d.totalRAM + uint32(d.cfg.RAMAlter)
	}

	return
}

// Load images and unpack them according the tags
func (d *Driver) loadImages(user string, images []provider.Image, diskPaths map[string]string) error {
	logger := log.WithFunc("native", "loadImages").With("provider.name", d.name)
	var wg sync.WaitGroup
	for _, image := range images {
		logger.Info("Loading the required image", "image_name", image.Name, "image_version", image.Version, "image_url", image.URL)

		// Running the background routine to download, unpack and process the image
		wg.Add(1)
		go func(image provider.Image) {
			defer wg.Done()
			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				logger.Error("Unable to download and unpack the image", "err", err)
			}
		}(image)
	}

	logger.Debug("Wait for all the background image processes to be done")
	wg.Wait()

	// The images have to be processed sequentially - child images could override the parent files
	for _, image := range images {
		imageUnpacked := filepath.Join(d.cfg.ImagesPath, image.Name+"-"+image.Version)

		// Getting the image subdir name in the unpacked dir
		subdir := ""
		items, err := os.ReadDir(imageUnpacked)
		if err != nil {
			logger.Error("Unable to read the unpacked directory", "image_unpacked", imageUnpacked, "err", err)
			return fmt.Errorf("NATIVE: %s: Unable to read the unpacked directory %q: %v", d.name, imageUnpacked, err)
		}
		for _, f := range items {
			if strings.HasPrefix(f.Name(), image.Name) {
				if f.Type()&fs.ModeSymlink != 0 {
					// Potentially it can be a symlink (like used in local tests)
					if _, err := os.Stat(filepath.Join(imageUnpacked, f.Name())); err != nil {
						logger.Warn("The image symlink is broken", "symlink", f.Name(), "err", err)
						continue
					}
				}
				subdir = f.Name()
				break
			}
		}
		if subdir == "" {
			logger.Error("Unpacked image has no subfolder", "image_unpacked", imageUnpacked, "image_name", image.Name, "items", items)
			return fmt.Errorf("Native: The image was unpacked incorrectly, please check log for the errors")
		}

		// Unpacking the image according its specified tag. If tag is empty - unpacks to home dir,
		// otherwise if tag exists in the disks map - then use its path to unpack there
		imageArchive := filepath.Join(imageUnpacked, subdir, image.Name+".tar")
		unpackPath, ok := diskPaths[image.Tag]
		if !ok {
			logger.Error("Unable to find where to unpack the image", "image_tag", image.Tag, "image_archive", imageArchive)
			return fmt.Errorf("NATIVE: %s: Unable to find where to unpack the image %q %q: %v", d.name, image.Tag, imageArchive, err)
		}

		// Since the image is under Fish node control and user could have no read access to the file
		// it's a good idea to use stdin of the tar command to unpack properly.
		f, err := os.Open(imageArchive)
		if err != nil {
			logger.Error("Unable to read the image", "image.archive", imageArchive, "err", err)
			return fmt.Errorf("NATIVE: %s: Unable to read the image %q: %v", d.name, imageArchive, err)
		}
		logger.Info("Unpacking image", "user", user, "image_archive", imageArchive, "unpack_path", unpackPath)
		_, _, err = util.RunAndLog("native", 5*time.Minute, f, d.cfg.SudoPath, "-n", d.cfg.TarPath, "-xf", "-", "--uname", user, "-C", unpackPath+"/")
		f.Close()
		if err != nil {
			logger.Error("Unable to unpack the image", "image.archive", imageArchive, "err", err)
			return fmt.Errorf("NATIVE: %s: Unable to unpack the image %q: %v", d.name, imageArchive, err)
		}
	}

	logger.Info("The images are processed")

	return nil
}

func isEnvAllocated(user string) bool {
	_, err := os.Stat("/Users/" + user)
	return !os.IsNotExist(err)
}

// Create the new user to run workload from it's name
// Don't forget to deleteUser if operation fails
func (d *Driver) userCreate(groups []string) (user, homedir string, err error) {
	c := &d.cfg
	// Id if the resource is the user name created from fish- prefix and 6 a-z random chars
	// WARNING: sudoers file is tied up to this format of user name, so please avoid the changes
	user = "fish-" + crypt.RandStringCharset(6, crypt.RandStringCharsetAZ)
	logger := log.WithFunc("native", "userCreate").With("provider.name", d.name, "user", user)

	// In theory we can use `sysadminctl -addUser` command instead, but it asks for elevated previleges
	// so not sure how useful it will be in automation...

	if _, _, err = util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "RealName", "Aquarium Fish env user"); err != nil {
		logger.Error("Error user set RealName", "err", err)
		err = fmt.Errorf("NATIVE: %s: Error user %q set RealName: %v", d.name, user, err)
		return
	}

	// Configure default shell
	if _, _, err = util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "UserShell", c.ShPath); err != nil {
		logger.Error("Error user set UserShell", "err", err)
		err = fmt.Errorf("NATIVE: %s: Error user %q set UserShell: %v", d.name, user, err)
		return
	}

	// Choose the UniqueID for the new user
	userCreateLock.Lock()
	{
		// Locate the unassigned user id
		var stdout string
		if stdout, _, err = util.RunAndLog("native", 5*time.Second, nil, c.DsclPath, ".", "list", "/Users", "UniqueID"); err != nil {
			userCreateLock.Unlock()
			logger.Error("Unable to list directory users", "err", err)
			err = fmt.Errorf("NATIVE: %s: Unable to list directory users: %v", d.name, err)
			return
		}

		// Finding the max user id in the OS
		userID := int64(1000) // Min 1000 is ok for most of the unix systems
		splitStdout := strings.Split(strings.TrimSpace(stdout), "\n")
		for _, line := range splitStdout {
			lineID := line[strings.LastIndex(line, " ")+1:]
			lineIDNum, err := strconv.ParseInt(lineID, 10, 64)
			if err != nil {
				logger.Warn("Unable to parse user id from line", "line", line)
				continue
			}
			if lineIDNum > userID {
				userID = lineIDNum
			}
		}

		// Increment max user id and use it as unique id for new user
		if _, _, err = util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "UniqueID", fmt.Sprint(userID+1)); err != nil {
			userCreateLock.Unlock()
			logger.Error("Unable to set user UniqueID", "err", err)
			err = fmt.Errorf("NATIVE: %s: Unable to set user %q UniqueID: %v", d.name, user, err)
			return
		}
	}
	userCreateLock.Unlock()

	// Locate the primary user group id
	primaryGroup, e := osuser.LookupGroup(groups[0])
	if e != nil {
		logger.Error("Unable to locate group GID", "group", groups[0], "err", e)
		err = fmt.Errorf("NATIVE: %s: Unable to locate group %q GID for: %v", d.name, groups[0], e)
		return
	}

	// Set user primary group
	if _, _, err = util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "PrimaryGroupID", primaryGroup.Gid); err != nil {
		logger.Error("Unable to set user PrimaryGroupID", "err", err)
		err = fmt.Errorf("NATIVE: %s: Unable to set user %q PrimaryGroupID: %v", d.name, user, err)
		return
	}

	// If there are other groups required - add user to them too
	if len(groups) > 1 {
		for _, group := range groups[1:] {
			if _, _, err = util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "append", "/Groups/"+group, "GroupMembership", user); err != nil {
				logger.Error("Unable to add user to group", "group", group, "err", err)
				err = fmt.Errorf("NATIVE: %s: Unable to add user %q to group %q: %v", d.name, user, group, err)
				return
			}
		}
	}

	// Set the default home directory
	homedir = filepath.Join("/Users", user)
	if _, _, err = util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "create", "/Users/"+user, "NFSHomeDirectory", homedir); err != nil {
		logger.Error("Unable to set user NFSHomeDirectory", "err", err)
		err = fmt.Errorf("NATIVE: %s: Unable to set user %q NFSHomeDirectory: %v", d.name, user, err)
		return
	}

	// Populate the default user home directory
	if _, _, err = util.RunAndLog("native", 30*time.Second, nil, c.SudoPath, "-n", c.CreatehomedirPath, "-c", "-u", user); err != nil {
		logger.Error("Unable to populate the default user directory", "err", err)
		err = fmt.Errorf("NATIVE: %s: Unable to populate the default user %q directory: %v", d.name, user, err)
		return
	}

	return
}

func (d *Driver) processTemplate(tplData *EnvData, value string) (string, error) {
	if tplData == nil {
		return value, nil
	}
	tmpl, err := template.New("").Parse(value)
	// Yep, still could fail here for example due to the template vars are not here
	if err != nil {
		return "", fmt.Errorf("NATIVE: %s: Unable to parse template: %v, %v", d.name, value, err)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, *tplData)
	if err != nil {
		return "", fmt.Errorf("NATIVE: %s: Unable to execute template: %v, %v", d.name, value, err)
	}

	return buf.String(), nil
}

// Runs the executable as defined user
func (d *Driver) userRun(envData *EnvData, user, entry string, metadata map[string]any) (err error) {
	logger := log.WithFunc("native", "processTemplate").With("provider.name", d.name, "user", user, "entry", entry)
	c := d.cfg
	// Entry value could contain template data
	var tmpData string
	if tmpData, err = d.processTemplate(envData, entry); err != nil {
		logger.Error("Unable to process entry template", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to process `entry` template %q: %v", d.name, entry, err)
	}
	entry = tmpData

	// Metadata values could contain template data
	envVars := make(map[string]any)
	for key, val := range metadata {
		if tmpData, err = d.processTemplate(envData, fmt.Sprintf("%v", val)); err != nil {
			logger.Error("Unable to process metadata template", "key", key, "err", err)
			return fmt.Errorf("NATIVE: %s: Unable to process metadata `%s` template: %v", d.name, key, err)
		}
		// Add to the map of the variables to store
		envVars[key] = tmpData
	}

	// Unfortunately passing the environment through the cmd.Env and sudo/su is not that easy, so
	// using a temp file instead, which is removed right after the entry is started.
	envFileData, err := util.SerializeMetadata("export", "", envVars)
	if err != nil {
		logger.Error("Unable to serialize metadata into export format", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to serialize metadata into 'export' format: %v", d.name, err)
	}
	// Using common /tmp dir available for each user in the system
	envFile, err := os.CreateTemp("/tmp", "*.metadata.sh")
	if err != nil {
		logger.Error("Unable to create temp env file", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to create temp env file: %v", d.name, err)
	}
	defer os.Remove(envFile.Name())
	if _, err := envFile.Write(envFileData); err != nil {
		logger.Error("Unable to write temp env file", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to write temp env file: %v", d.name, err)
	}
	if err := envFile.Close(); err != nil {
		logger.Error("Unable to close temp env file", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to close temp env file: %v", d.name, err)
	}

	// Add ACL permission to the env file to allow to read it by unprevileged user
	if _, _, err := util.RunAndLogRetry("native", 5, 5*time.Second, nil, c.ChmodPath, "+a", fmt.Sprintf("user:%s:allow read,readattr,readextattr,readsecurity", user), envFile.Name()); err != nil {
		logger.Error("Unable to set ACL for temp env file", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to set ACL for temp env file: %v", d.name, err)
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
		logger.Error("Unable to run the process", "err", err)
		return fmt.Errorf("NATIVE: %s: Unable to run the process: %v", d.name, err)
	}
	// TODO: Probably I should run cmd.Wait to make sure the captured OS resources are released,
	// but not sure about that... Maybe create a goroutine that will sit and wait there?

	logger.Debug("Started entry for user in directory with PID", "dir", cmd.Dir, "pid", cmd.Process.Pid, "shell_line", shellLine)

	// Giving the process 1 second to read the env file and not die from some unexpected error
	time.Sleep(time.Second)
	if cmd.Err != nil {
		logger.Error("The process ended quickly with error", "err", cmd.Err)
		err = fmt.Errorf("NATIVE: %s: The process for %q ended quickly with error: %v", d.name, user, cmd.Err)
	}

	if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		systime := cmd.ProcessState.SystemTime()
		usertime := cmd.ProcessState.UserTime()
		logger.Error("The process ended quickly with non-zero exit code", "exit_code", cmd.ProcessState.ExitCode(), "pid", cmd.ProcessState.Pid(), "systime", systime, "usertime", usertime, "stderr", cmd.ProcessState.String())
		err = fmt.Errorf("NATIVE: %s: The process for %q ended quickly with non-zero exit code: code:%d, pid:%d, systime:%s, usertime:%s : %s",
			d.name, user, cmd.ProcessState.ExitCode(), cmd.ProcessState.Pid(), cmd.ProcessState.SystemTime(), cmd.ProcessState.UserTime(), cmd.ProcessState.String())
	}

	return err
}

// Stop the user processes
func (d *Driver) userStop(user string) (outErr error) { //nolint:unparam
	c := &d.cfg
	logger := log.WithFunc("native", "userStop").With("provider.name", d.name, "user", user)
	// In theory we can use `sysadminctl -deleteUser` command instead, which is also stopping all the
	// user processes and cleans up the home dir, but it asks for elevated previleges so not sure how
	// useful it will be in automation...

	// Note: some operations may fail, but they should not interrupt the whole cleanup process

	// Interrupt all the user processes
	if _, _, err := util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.KillallPath, "-INT", "-u", user); err != nil {
		logger.Debug("Unable to interrupt the user apps", "err", err)
	}
	// Check if no apps are running after interrupt - ps will end up with error if there is none apps left
	if _, _, err := util.RunAndLog("native", 5*time.Second, nil, "ps", "-U", user); err == nil {
		// Some apps are still running - give them 5 seconds to complete their processes
		time.Sleep(5 * time.Second)
		if _, _, err := util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.KillallPath, "-KILL", "-u", user); err != nil {
			logger.Warn("Unable to kill the user apps", "err", err)
		}
	}

	return
}

// Delete user and clean up
func (d *Driver) userDelete(user string) (outErr error) {
	c := &d.cfg
	logger := log.WithFunc("native", "userDelete").With("provider.name", d.name, "user", user)
	// Stopping the processes because they could cause user lock
	outErr = d.userStop(user)

	// Sometimes delete of the user could not be done due to MacOS blocking it, so retrying 5 times
	// Native: Command exited with error: exit status 40: <main> delete status: eDSPermissionError <dscl_cmd> DS Error: -14120 (eDSPermissionError)
	if _, _, err := util.RunAndLogRetry("native", 5, 5*time.Second, nil, c.SudoPath, "-n", c.DsclPath, ".", "delete", "/Users/"+user); err != nil {
		logger.Error("Unable to delete user", "err", err)
		outErr = fmt.Errorf("NATIVE: %s: Unable to delete user %q: %v", d.name, user, err)
	}

	if _, _, err := util.RunAndLog("native", 5*time.Second, nil, c.SudoPath, "-n", c.RmPath, "-rf", "/Users/"+user); err != nil {
		logger.Error("Unable to remove the user home directory", "err", err)
		outErr = fmt.Errorf("NATIVE: %s: Unable to remove the user %q home directory: %v", d.name, user, err)
	}

	return
}

// Unmount user volumes and delete the disk files
func (d *Driver) disksDelete(user string) (outErr error) {
	c := &d.cfg
	logger := log.WithFunc("native", "disksDelete").With("provider.name", d.name, "user", user)
	// Stopping the processes because they could cause user lock
	outErr = d.userStop(user)

	// Getting the list of the mounted volumes
	volumes, err := os.ReadDir("/Volumes")
	if err != nil {
		logger.Error("Unable to list mounted volumes", "err", err)
		outErr = fmt.Errorf("NATIVE: %s: Unable to list mounted volumes: %v", d.name, err)
	}
	envVolumes := []string{}
	for _, file := range volumes {
		if file.IsDir() && strings.HasPrefix(file.Name(), user) {
			envVolumes = append(envVolumes, filepath.Join("/Volumes", file.Name()))
		}
	}

	// Umount the disk volumes if needed
	mounts, _, err := util.RunAndLog("native", 3*time.Second, nil, c.MountPath)
	if err != nil {
		logger.Error("Unable to list the mount points", "err", err)
		outErr = fmt.Errorf("NATIVE: %s: Unable to list the mount points: %v", d.name, err)
	}
	for _, volPath := range envVolumes {
		if strings.Contains(mounts, volPath) {
			if _, _, err := util.RunAndLog("native", 5*time.Second, nil, c.HdiutilPath, "detach", volPath); err != nil {
				logger.Error("Unable to detach the volume disk", "vol_path", volPath, "err", err)
				outErr = fmt.Errorf("NATIVE: %s: Unable to detach the volume disk %q: %v", d.name, volPath, err)
			}
		}
	}

	// Cleaning the env work directory with disks
	workspacePath := filepath.Join(c.WorkspacePath, user)
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		if err := os.RemoveAll(workspacePath); err != nil {
			logger.Error("Unable to remove user env workspace", "err", err)
			outErr = fmt.Errorf("NATIVE: %s: Unable to remove user %q env workspace: %v", d.name, user, err)
		}
	}

	return
}

// Creates disks directories described by the disks map, returns the map of disks to mount paths
func (d *Driver) disksCreate(user string, disks map[string]typesv2.ResourcesDisk) (map[string]string, error) {
	logger := log.WithFunc("native", "disksCreate").With("provider.name", d.name, "user", user)
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
			if _, _, err := util.RunAndLog("native", 10*time.Minute, nil, d.cfg.HdiutilPath, args...); err != nil {
				logger.Error("Unable to create dmg disk", "dmg_path", dmgPath, "err", err)
				return diskPaths, fmt.Errorf("NATIVE: %s: Unable to create dmg disk %q: %v", d.name, dmgPath, err)
			}
		}

		mountPoint := filepath.Join("/Volumes", fmt.Sprintf("%s_%s", user, dName))

		// Attach & mount disk
		if _, _, err := util.RunAndLog("native", 10*time.Second, nil, d.cfg.HdiutilPath, "attach", dmgPath, "-owners", "on", "-mountpoint", mountPoint); err != nil {
			logger.Error("Unable to attach dmg disk", "dmg_path", dmgPath, "mount_point", mountPoint, "err", err)
			return diskPaths, fmt.Errorf("NATIVE: %s: Unable to attach dmg disk %q to %q: %v", d.name, dmgPath, mountPoint, err)
		}

		// Change the owner of the volume to user
		if _, _, err := util.RunAndLog("native", 5*time.Second, nil, d.cfg.SudoPath, "-n", d.cfg.ChownPath, "-R", user+":staff", mountPoint+"/"); err != nil {
			return diskPaths, fmt.Errorf("NATIVE: %s: Error user disk mount path %q chown: %v", d.name, mountPoint, err)
		}

		// (Optional) Disable spotlight for the mounted volume
		if _, _, err := util.RunAndLog("native", 5*time.Second, nil, d.cfg.SudoPath, d.cfg.MdutilPath, "-i", "off", mountPoint+"/"); err != nil {
			logger.Warn("Unable to disable spotlight for the volume", "mount_point", mountPoint, "err", err)
		}

		diskPaths[dName] = mountPoint
	}

	return diskPaths, nil
}
