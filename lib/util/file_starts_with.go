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
	"errors"
	"os"
)

var (
	ErrFileStartsWithDirectory    = errors.New("FileStartsWith: Unable to check file prefix for directory")
	ErrFileStartsWithFileTooSmall = errors.New("FileStartsWith: File is too small for prefix")
	ErrFileStartsWithNotEqual     = errors.New("FileStartsWith: File is not starts with the prefix")
)

func FileStartsWith(path string, prefix []byte) error {
	// Open input file
	in_f, err := os.OpenFile(path, os.O_RDONLY, 0o644)
	if err != nil {
		return err
	}
	defer in_f.Close()

	// Check it's not a dir
	if info, err := in_f.Stat(); err == nil && info.IsDir() {
		return ErrFileStartsWithDirectory
	}

	buf := make([]byte, len(prefix))
	length, err := in_f.Read(buf)
	if err != nil {
		return err
	}
	if length != len(prefix) {
		return ErrFileStartsWithFileTooSmall
	}

	if bytes.Equal(prefix, buf) {
		return nil
	}

	return ErrFileStartsWithNotEqual
}
