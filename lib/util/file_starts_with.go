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
	"bytes"
	"fmt"
	"os"
)

var (
	errFileStartsWithDirectory    = fmt.Errorf("FileStartsWith: Unable to check file prefix for directory")
	errFileStartsWithFileTooSmall = fmt.Errorf("FileStartsWith: File is too small for prefix")
	errFileStartsWithNotEqual     = fmt.Errorf("FileStartsWith: File is not starts with the prefix")
)

// FileStartsWith checks the file starts with required prefix
func FileStartsWith(path string, prefix []byte) error {
	// Open input file
	inF, err := os.OpenFile(path, os.O_RDONLY, 0o644)
	if err != nil {
		return err
	}
	defer inF.Close()

	// Check it's not a dir
	if info, err := inF.Stat(); err == nil && info.IsDir() {
		return errFileStartsWithDirectory
	}

	buf := make([]byte, len(prefix))
	length, err := inF.Read(buf)
	if err != nil {
		return err
	}
	if length != len(prefix) {
		return errFileStartsWithFileTooSmall
	}

	if bytes.Equal(prefix, buf) {
		return nil
	}

	return errFileStartsWithNotEqual
}
