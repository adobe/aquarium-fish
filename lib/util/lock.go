package util

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The function creates the lock file, notice - remove it yourself
func CreateLock(lock_path string) error {
	lock_file, err := os.Create(lock_path)
	if err != nil {
		log.Println("Util: Unable to create the lock file:", lock_path)
		return err
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
				pid, err := strconv.ParseInt(strings.SplitN(string(lock_info), " ", 2)[0], 10, 64)
				if err != nil {
					// No pid in the lock file - it's actually a small chance it's create/write
					// delay, but it's so small I want to ignore it
					log.Printf("Util: Lock file doesn't contain pid of the process '%s': %s\n", lock_path, lock_info)
					clean()
					os.Remove(lock_path)
					break
				}
				if proc, err := os.FindProcess(int(pid)); err != nil || proc.Signal(syscall.Signal(0)) != nil {
					log.Printf("Util: No process running for lock file '%s': %s\n", lock_path, lock_info)
					clean()
					os.Remove(lock_path)
					break
				}
				log.Printf("Util: Waiting for '%s', pid %s\n", lock_path, lock_info)
			}
		}

		time.Sleep(5 * time.Second)
		wait_counter += 1
	}

	return nil
}
