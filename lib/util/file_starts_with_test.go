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
	"os"
	"path"
	"testing"
)

func TestFileStartsWithGood(t *testing.T) {
	tmp_file := path.Join(t.TempDir(), "test.txt")

	in_data := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")

	os.WriteFile(tmp_file, in_data, 0644)

	if err := FileStartsWith(tmp_file, []byte("test1 ")); err != nil {
		t.Fatalf(`FileStartsWith("test1 ") = %v, want: nil`, err)
	}
}

func TestFileStartsNotEqual(t *testing.T) {
	tmp_file := path.Join(t.TempDir(), "test.txt")

	in_data := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")

	os.WriteFile(tmp_file, in_data, 0644)

	if err := FileStartsWith(tmp_file, []byte("test2 ")); err != ErrFileStartsWithNotEqual {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, ErrFileStartsWithNotEqual)
	}
}

func TestFileStartsDirectory(t *testing.T) {
	tmp_file := t.TempDir()

	if err := FileStartsWith(tmp_file, []byte("test2 ")); err != ErrFileStartsWithDirectory {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, ErrFileStartsWithDirectory)
	}
}

func TestFileStartsSmall(t *testing.T) {
	tmp_file := path.Join(t.TempDir(), "test.txt")

	in_data := []byte("small file\n")

	os.WriteFile(tmp_file, in_data, 0644)

	if err := FileStartsWith(tmp_file, []byte("biiiiiiiiiig prefix")); err != ErrFileStartsWithFileTooSmall {
		t.Fatalf(`FileStartsWith("test2 ") = %v, want: %v`, err, ErrFileStartsWithFileTooSmall)
	}
}
