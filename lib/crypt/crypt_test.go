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

package crypt

import (
	"fmt"
	"testing"
)

// To prevent compiler optimization
var result1 Hash
var result2 bool

// Tests user password hash function performance
func Benchmark_hash_new(b *testing.B) {
	// To prevent compiler optimization
	var r Hash

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		r = NewHash(RandString(32), nil)
	}
	b.StopTimer()

	result1 = r
}

// IsEqual is not that different from generating the new hash, but worth to
// check because it's used more often during application execution life
func Benchmark_hash_isequal(b *testing.B) {
	h := NewHash(RandString(32), nil)

	// To prevent compiler optimization
	var r bool

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		r = h.IsEqual(RandString(32))
	}
	b.StopTimer()

	result2 = r
}

// Make sure the hash generation algo will be the same across the Fish versions to be
// certain the update will not cause issues with users passwords validation compatibility
func Test_ensure_hash_same(t *testing.T) {
	input := "abcdefghijklmnopqrstuvwxyz012345"
	salt := []byte("abcdefgh")
	result := "f3ca14a142596fe4b1c5441cc962cf88b8f7c59243abfbca89f34e24aa8c9e25"

	h := NewHash(input, salt)

	if fmt.Sprintf("%x", h.Hash) != result {
		t.Fatalf("The change of hashing algo props caused incompatibility in hash value")
	}
}
