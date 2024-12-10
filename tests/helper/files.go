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

// Simplifies work with file testing
package helper

import (
	"bytes"
	"crypto/md5" // #nosec G501
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// CreateRandomFiles will take directory and put there as much random files as you want
func CreateRandomFiles(dir string, amount int) ([]string, error) {
	var out []string
	data := make([]byte, 1024)
	for i := range amount {
		// Generate test data for file
		if _, err := rand.Read(data); err != nil {
			return nil, fmt.Errorf("Unable to generate test data: %v", err)
		}

		// Creating the test file
		path := filepath.Join(dir, "testfile_"+strconv.Itoa(i))
		file, err := os.Create(path)
		if err != nil {
			return nil, fmt.Errorf("Unable to open test file %q: %v", path, err)
		}

		out = append(out, path)

		// Filling file with 1KB of data
		_, err = file.Write(data)
		file.Close()
		if err != nil {
			return nil, fmt.Errorf("Unable to write test file %q: %v", path, err)
		}
	}

	return out, nil
}

func CompareDirFiles(dir1, dir2 string) error {
	err := filepath.Walk(dir1, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Construct the corresponding path in dir2
		relPath, err := filepath.Rel(dir1, path)
		if err != nil {
			return err
		}
		path2 := filepath.Join(dir2, relPath)

		// Check if the file exists in dir2
		if _, err := os.Stat(path2); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("File missing in dir2 %q: %v", path2, err)
			}
			return fmt.Errorf("Error checking file in dir2 %q: %v", path2, err)
		}

		// Compare file contents
		if equal, err := compareFiles(path, path2); err != nil {
			return fmt.Errorf("Error comparing files: %v", err)
		} else if !equal {
			return fmt.Errorf("Files differ: %q and %q", path, path2)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("Error comparing directories: %v", err)
	}

	return nil
}

func compareFiles(file1, file2 string) (bool, error) {
	// Use MD5 hash for quick comparison
	hash1, err := fileHash(file1)
	if err != nil {
		return false, err
	}

	hash2, err := fileHash(file2)
	if err != nil {
		return false, err
	}

	return bytes.Equal(hash1, hash2), nil
}

func fileHash(file string) ([]byte, error) {
	hash := md5.New() // #nosec G401
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(hash, f); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}
