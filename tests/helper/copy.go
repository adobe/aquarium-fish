/**
 * Copyright 2023-2025 Adobe. All rights reserved.
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

// Simple copy file
package helper

import (
	"io"
	"os"
	"path/filepath"
)

// CopyFile will copy files around
func CopyFile(src, dst string) error {
	fin, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fin.Close()

	os.MkdirAll(filepath.Dir(dst), 0o755)
	fout, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer fout.Close()

	if _, err = io.Copy(fout, fin); err != nil {
		return err
	}

	return nil
}
