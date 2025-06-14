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

package util

import (
	"os"
	"path"
	"testing"
)

func TestFileStartsWithGood(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")

	os.WriteFile(tmpFile, inData, 0o644)

	if err := FileStartsWith(tmpFile, []byte("test1 ")); err != nil {
		t.Fatalf(`FileStartsWith("test1 ") = %v, want: nil`, err)
	}
}

func TestFileStartsNotEqual(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")

	os.WriteFile(tmpFile, inData, 0o644)

	if err := FileStartsWith(tmpFile, []byte("test2 ")); err != errFileStartsWithNotEqual {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, errFileStartsWithNotEqual)
	}
}

func TestFileStartsDirectory(t *testing.T) {
	tmpFile := t.TempDir()

	if err := FileStartsWith(tmpFile, []byte("test2 ")); err != errFileStartsWithDirectory {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, errFileStartsWithDirectory)
	}
}

func TestFileStartsSmall(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("small file\n")

	os.WriteFile(tmpFile, inData, 0o644)

	if err := FileStartsWith(tmpFile, []byte("biiiiiiiiiig prefix")); err != errFileStartsWithFileTooSmall {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, errFileStartsWithFileTooSmall)
	}
}
