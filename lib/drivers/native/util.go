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
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// Common lock to properly acquire unique User ID
var user_create_lock sync.Mutex

// Returns the total resources available for the node after alteration
func (d *Driver) getAvailResources() (avail_cpu, avail_ram uint) {
	if d.cfg.CpuAlter < 0 {
		avail_cpu = d.total_cpu - uint(-d.cfg.CpuAlter)
	} else {
		avail_cpu = d.total_cpu + uint(d.cfg.CpuAlter)
	}

	if d.cfg.RamAlter < 0 {
		avail_ram = d.total_ram - uint(-d.cfg.RamAlter)
	} else {
		avail_ram = d.total_ram + uint(d.cfg.RamAlter)
	}

	return
}

// Load images and unpack them according the tags
func (d *Driver) loadImages(user string, images []drivers.Image, disk_paths map[string]string) error {
	var wg sync.WaitGroup
	for _, image := range images {
		log.Info("Native: Loading the required image:", image.Name, image.Version, image.Url)

		// Running the background routine to download, unpack and process the image
		wg.Add(1)
		go func(image drivers.Image) {
			defer wg.Done()
			if err := image.DownloadUnpack(d.cfg.ImagesPath, d.cfg.DownloadUser, d.cfg.DownloadPassword); err != nil {
				log.Error("Native: Unable to download and unpack the image:", image.Name, image.Url, err)
			}
		}(image)
	}

	log.Debug("Native: Wait for all the background image processes to be done...")
	wg.Wait()

	// The images have to be processed sequentially - child images could override the parent files
	for _, image := range images {
		image_unpacked := filepath.Join(d.cfg.ImagesPath, image.Name+"-"+image.Version)

		// Getting the image subdir name in the unpacked dir
		subdir := ""
		items, err := os.ReadDir(image_unpacked)
		if err != nil {
			return log.Error("Native: Unable to read the unpacked directory:", image_unpacked, err)
		}
		for _, f := range items {
			if strings.HasPrefix(f.Name(), image.Name) {
				if f.Type()&fs.ModeSymlink != 0 {
					// Potentially it can be a symlink (like used in local tests)
					if _, err := os.Stat(filepath.Join(image_unpacked, f.Name())); err != nil {
						log.Warn("Native: The image symlink is broken:", f.Name(), err)
						continue
					}
				}
				subdir = f.Name()
				break
			}
		}
		if subdir == "" {
			log.Errorf("Native: Unpacked image '%s' has no subfolder '%s', only: %q", image_unpacked, image.Name, items)
			return fmt.Errorf("Native: The image was unpacked incorrectly, please check log for the errors")
		}

		// Unpacking the image according its specified tag. If tag is empty - unpacks to home dir,
		// otherwise if tag exists in the disks map - then use its path to unpack there
		image_archive := filepath.Join(image_unpacked, subdir, image.Name+".tar")
		unpack_path, ok := disk_paths[image.Tag]
		if !ok {
			return log.Error("Native: Unable to find where to unpack the image:", image.Tag, image_archive, err)
		}

		// Since the image is under Fish node control and user could have no read access to the file
		// it's a good idea to use stdin of the tar command to unpack properly.
		f, err := os.Open(image_archive)
		if err != nil {
			return log.Error("Native: Unable to read the image:", image_archive, err)
		}
		defer f.Close()
		log.Info("Native: Unpacking image:", user, image_archive, unpack_path)
		_, _, err = runAndLog(5*time.Minute, f, d.cfg.SudoPath, "-n", "/usr/bin/tar", "-xf", "-", "--uname", user, "-C", unpack_path+"/")
		if err != nil {
			return log.Error("Native: Unable to unpack the image:", image_archive, err)
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

	if _, _, err = runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "create", "/Users/"+user, "RealName", "Aquarium Fish env user"); err != nil {
		err = log.Error("Native: Error user set RealName:", err)
		return
	}

	// Configure default shell
	if _, _, err = runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "create", "/Users/"+user, "UserShell", "/bin/sh"); err != nil {
		err = log.Error("Native: Error user set UserShell:", err)
		return
	}

	// Choose the UniqueID for the new user
	user_create_lock.Lock()
	{
		// Locate the unassigned user id
		var stdout string
		if stdout, _, err = runAndLog(5*time.Second, nil, "/usr/bin/dscl", ".", "list", "/Users", "UniqueID"); err != nil {
			user_create_lock.Unlock()
			err = log.Error("Native: Unable to list directory users:", err)
			return
		}

		// Finding the max user id in the OS
		user_id := int64(1000) // Min 1000 is ok for most of the unix systems
		split_stdout := strings.Split(strings.TrimSpace(stdout), "\n")
		for _, line := range split_stdout {
			line_id := line[strings.LastIndex(line, " ")+1:]
			line_id_num, err := strconv.ParseInt(line_id, 10, 64)
			if err != nil {
				log.Warnf("Native: Unable to parse user id from line: %q", line)
				continue
			}
			if line_id_num > user_id {
				user_id = line_id_num
			}
		}

		// Increment max user id and use it as unique id for new user
		if _, _, err = runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "create", "/Users/"+user, "UniqueID", fmt.Sprint(user_id+1)); err != nil {
			user_create_lock.Unlock()
			err = log.Error("Native: Unable to set user UniqueID:", err)
			return
		}
	}
	user_create_lock.Unlock()

	if len(groups) > 0 {
		// Locate the primary user group id
		var stdout string
		if stdout, _, err = runAndLog(5*time.Second, nil, "/usr/bin/dscl", ".", "list", "/Groups", "PrimaryGroupID"); err != nil {
			err = log.Error("Native: Unable to list directory groups:", err)
			return
		}

		// Finding the primary group id in the list
		primary_group_id := int64(-1)
		split_stdout := strings.Split(strings.TrimSpace(stdout), "\n")
		for _, line := range split_stdout {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, groups[0]+" ") {
				line_id := line[strings.LastIndex(line, " ")+1:]
				if primary_group_id, err = strconv.ParseInt(line_id, 10, 64); err != nil {
					err = log.Error("Native: Unable to parse group id in line:", line)
					return
				}
				break
			}
		}

		if primary_group_id == -1 {
			err = log.Error("Native: Unable to find id for group:", groups[0])
		}

		// Set user primary group
		if _, _, err = runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "create", "/Users/"+user, "PrimaryGroupID", fmt.Sprint(primary_group_id)); err != nil {
			err = log.Error("Native: Unable to set user PrimaryGroupID:", err)
			return
		}

		// If there are other groups required - add user to them too
		if len(groups) > 1 {
			for _, group := range groups[1:] {
				if _, _, err = runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "append", "/Groups/"+group, "GroupMembership", user); err != nil {
					err = log.Error("Native: Unable to add user to group:", group, err)
					return
				}
			}
		}
	}

	// Set the default home directory
	homedir = filepath.Join("/Users", user)
	if _, _, err = runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "create", "/Users/"+user, "NFSHomeDirectory", homedir); err != nil {
		err = log.Error("Native: Unable to set user NFSHomeDirectory:", err)
		return
	}

	// Populate the default user home directory
	if _, _, err = runAndLog(30*time.Second, nil, c.SudoPath, "-n", "/usr/sbin/createhomedir", "-c", "-u", user); err != nil {
		err = log.Error("Native: Unable to populate the default user directory:", err)
		return
	}

	return
}

func processTemplate(tpl_data *EnvData, value string) (string, error) {
	if tpl_data == nil {
		return value, nil
	}
	tmpl, err := template.New("").Parse(value)
	// Yep, still could fail here for example due to the template vars are not here
	if err != nil {
		return "", fmt.Errorf("Native: Unable to parse template: %v, %v", value, err)
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, *tpl_data)
	if err != nil {
		return "", fmt.Errorf("Native: Unable to execute template: %v, %v", value, err)
	}

	return buf.String(), nil
}

// Runs the executable as defined user
func userRun(c *Config, env_data *EnvData, user, entry string, metadata map[string]any) (err error) {
	// Entry value could contain template data
	var tmp_data string
	if tmp_data, err = processTemplate(env_data, entry); err != nil {
		return log.Error("Native: Unable to process `entry` template:", entry, err)
	}
	entry = tmp_data

	// Prepare the command to execute entry from user home directory
	cmd := exec.Command(c.SudoPath, "-n", "/usr/bin/su", "-l", user, entry)

	// Metadata values could contain template data
	for key, val := range metadata {
		if tmp_data, err = processTemplate(env_data, fmt.Sprintf("%v", val)); err != nil {
			return log.Errorf("Native: Unable to process metadata `%s` template: %v", key, err)
		}
		// Fill the command env vars
		cmd.Env = append(cmd.Environ(), fmt.Sprintf("%s=%v", key, tmp_data))
	}

	// Run the process in background, it should live even when the Fish node is down
	if err = cmd.Start(); err != nil {
		return log.Error("Native: Unable to run the process:", err)
	}

	return nil
}

// Stop the user processes
func userStop(c *Config, user string) (out_err error) {
	// In theory we can use `sysadminctl -deleteUser` command instead, which is also stopping all the
	// user processes and cleans up the home dir, but it asks for elevated previleges so not sure how
	// useful it will be in automation...

	// Note: some operations may fail, but they should not interrupt the whole cleanup process

	// Interrupt all the user processes
	if _, _, err := runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/killall", "-INT", "-u", user); err != nil {
		log.Warn("Native: Unable to interrupt the user apps:", err)
	}
	// Check if no apps are running after interrupt - ps will end up with error if there is none apps left
	if _, _, err := runAndLog(5*time.Second, nil, "ps", "-U", user); err == nil {
		// Some apps are still running - give them 5 seconds to complete their processes
		time.Sleep(5 * time.Second)
		if _, _, err := runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/killall", "-KILL", "-u", user); err != nil {
			log.Warn("Native: Unable to kill the user apps:", err)
		}
	}

	return
}

// Delete user and clean up
func userDelete(c *Config, user string) (out_err error) {
	// Stopping the processes because they could cause user lock
	out_err = userStop(c, user)

	if _, _, err := runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/usr/bin/dscl", ".", "delete", "/Users/"+user); err != nil {
		out_err = log.Error("Native: Unable to delete user:", err)
	}

	if _, _, err := runAndLog(5*time.Second, nil, c.SudoPath, "-n", "/bin/rm", "-rf", "/Users/"+user); err != nil {
		out_err = log.Error("Native: Unable to remove the user home directory:", err)
	}

	return
}

// Unmount user volumes and delete the disk files
func disksDelete(c *Config, user string) (out_err error) {
	// Stopping the processes because they could cause user lock
	out_err = userStop(c, user)

	// Getting the list of the mounted volumes
	volumes, err := os.ReadDir("/Volumes")
	if err != nil {
		out_err = log.Error("Native: Unable to list mounted volumes:", err)
	}
	env_volumes := []string{}
	for _, file := range volumes {
		if file.IsDir() && strings.HasPrefix(file.Name(), user) {
			env_volumes = append(env_volumes, filepath.Join("/Volumes", file.Name()))
		}
	}

	// Umount the disk volumes if needed
	mounts, _, err := runAndLog(3*time.Second, nil, "/sbin/mount")
	if err != nil {
		out_err = log.Error("Native: Unable to list the mount points:", user, err)
	}
	for _, vol_path := range env_volumes {
		if strings.Contains(mounts, vol_path) {
			if _, _, err := runAndLog(5*time.Second, nil, "/usr/bin/hdiutil", "detach", vol_path); err != nil {
				out_err = log.Error("Native: Unable to detach the volume disk:", user, vol_path, err)
			}
		}
	}

	// Cleaning the env work directory with disks
	workspace_path := filepath.Join(c.WorkspacePath, user)
	if _, err := os.Stat(workspace_path); !os.IsNotExist(err) {
		if err := os.RemoveAll(workspace_path); err != nil {
			out_err = log.Error("Native: Unable to remove user env workspace:", user, err)
		}
	}

	return
}

// Creates disks directories described by the disks map, returns the map of disks to mount paths
func (d *Driver) disksCreate(user string, disks map[string]types.ResourcesDisk) (map[string]string, error) {
	// Create disks
	disk_paths := make(map[string]string, len(disks))

	for d_name, disk := range disks {
		disk_path := filepath.Join(d.cfg.WorkspacePath, user, "disk-"+d_name)
		if err := os.MkdirAll(filepath.Dir(disk_path), 0o755); err != nil {
			return disk_paths, err
		}

		// Create disk
		// TODO: Ensure failures doesn't leave the changes behind (like mounted disks or files)

		if disk.Type == "dir" {
			if err := os.MkdirAll(disk_path, 0o777); err != nil {
				return disk_paths, err
			}
			disk_paths[d_name] = disk_path
			// TODO: Validate the available disk space for disk.Size
			continue
		}

		// Create virtual disk in order to restrict the disk space
		dmg_path := disk_path + ".dmg"

		label := d_name
		if disk.Label != "" {
			// Label can be used as mount point so cut the path separator out
			label = strings.ReplaceAll(disk.Label, "/", "")
		} else {
			disk.Label = label
		}

		// Do not recreate the disk if it is exists
		if _, err := os.Stat(dmg_path); os.IsNotExist(err) {
			args := []string{"create", dmg_path,
				"-fs", "HFS+",
				"-layout", "NONE",
				"-volname", label,
				"-size", fmt.Sprintf("%dm", disk.Size*1024),
			}
			if _, _, err := runAndLog(10*time.Minute, nil, "/usr/bin/hdiutil", args...); err != nil {
				return disk_paths, log.Error("Native: Unable to create dmg disk:", dmg_path, err)
			}
		}

		mount_point := filepath.Join("/Volumes", fmt.Sprintf("%s_%s", user, d_name))

		// Attach & mount disk
		if _, _, err := runAndLog(10*time.Second, nil, "/usr/bin/hdiutil", "attach", dmg_path, "-owners", "on", "-mountpoint", mount_point); err != nil {
			return disk_paths, log.Error("Native: Unable to attach dmg disk:", dmg_path, mount_point, err)
		}

		// Change the owner of the volume to user
		if _, _, err := runAndLog(5*time.Second, nil, d.cfg.SudoPath, "-n", "/usr/sbin/chown", "-R", user+":staff", mount_point+"/"); err != nil {
			return disk_paths, fmt.Errorf("Native: Error user disk mount path chown: %v", err)
		}

		// (Optional) Disable spotlight for the mounted volume
		if _, _, err := runAndLog(5*time.Second, nil, d.cfg.SudoPath, "/usr/bin/mdutil", "-i", "off", mount_point+"/"); err != nil {
			log.Warn("Native: Unable to disable spotlight for the volume:", mount_point, err)
		}

		disk_paths[d_name] = mount_point
	}

	return disk_paths, nil
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
	returnStdout := strings.Replace(stdout.String(), "\r\n", "\n", -1)
	returnStderr := strings.Replace(stderr.String(), "\r\n", "\n", -1)

	return returnStdout, returnStderr, err
}
