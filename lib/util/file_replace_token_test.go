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
	"bytes"
	"os"
	"path"
	"testing"
)

func Test_file_replace_token_simple_proceed(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <token> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, false, false,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_simple_skip_uppercase_src(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <TOKEN> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <TOKEN> test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, false, false,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_simple_skip_uppercase_token(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <token> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <token> test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, false, false,
		"<TOKEN>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<TOKEN>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_anycase_token_proceed(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <token> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, false, true,
		"<TOKEN>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<TOKEN>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_anycase_src_proceed(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <TOKEN> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, false, true,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_anycase_multiple(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <TOKEN> <token> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test5 test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, false, true,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_add(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n" +
		"test5\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, true, false,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_do_not_add_if_replaced(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <token> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test4 test5 test6\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		false, true, false,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}

func Test_file_replace_token_full_line(t *testing.T) {
	tmpFile := path.Join(t.TempDir(), "test.txt")

	inData := []byte("" +
		"test1 test2 test3\n" +
		"test4 <token> test6\n" +
		"test7 test8 test9\n")
	outData := []byte("" +
		"test1 test2 test3\n" +
		"test5\n" +
		"test7 test8 test9\n")

	os.WriteFile(tmpFile, inData, 0o644)

	FileReplaceToken(tmpFile,
		true, false, false,
		"<token>", "test5",
	)

	body, err := os.ReadFile(tmpFile)

	if err != nil || !bytes.Equal(body, outData) {
		t.Fatalf(`FileReplaceToken("<token>", "test5") = %q, %v, want: %q, error`, body, err, outData)
	}
}
