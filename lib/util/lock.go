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

package util

import (
	"fmt"
	"io/ioutil"
	"math"
	"math/bits"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
)

// The function creates the lock file, notice - remove it yourself
func CreateLock(lock_path string) error {
	lock_file, err := os.Create(lock_path)
	if err != nil {
		return log.Error("Util: Unable to create the lock file:", lock_path)
	}

	// Writing pid into the file for additional info
	lock_file.Write([]byte(fmt.Sprintf("%d", os.Getpid())))
	lock_file.Close()

	return nil
}

// Wait for the lock file and clean func will be executed if it's invalid
func WaitLock(lock_path string, clean func()) error {
	wait_counter := 0
	for {
		if _, err := os.Stat(lock_path); os.IsNotExist(err) {
			break
		}
		if wait_counter%6 == 0 {
			// Read the lock file to print the pid
			if lock_info, err := ioutil.ReadFile(lock_path); err == nil {
				// Check the pid is running - because if the app crashes
				// it can leave the lock file (weak protection but worth it)
				pid, err := strconv.ParseInt(strings.SplitN(string(lock_info), " ", 2)[0], 10, bits.UintSize)
				if err != nil || pid < 0 || pid > math.MaxInt32 {
					// No valid pid in the lock file - it's actually a small chance it's create or
					// write delay, but it's so small I want to ignore it
					log.Warnf("Util: Lock file doesn't contain pid of the process '%s': %s - %v", lock_path, lock_info, err)
					clean()
					os.Remove(lock_path)
					break
				}
				if proc, err := os.FindProcess(int(pid)); err != nil || proc.Signal(syscall.Signal(0)) != nil {
					log.Warnf("Util: No process running for lock file '%s': %s", lock_path, lock_info)
					clean()
					os.Remove(lock_path)
					break
				}
				log.Debugf("Util: Waiting for '%s', pid %s", lock_path, lock_info)
			}
		}

		time.Sleep(5 * time.Second)
		wait_counter += 1
	}

	return nil
}
