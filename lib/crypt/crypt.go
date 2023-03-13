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

package crypt

import (
	"bytes"
	"crypto/rand"
	"math/big"

	"golang.org/x/crypto/argon2"

	"github.com/adobe/aquarium-fish/lib/log"
)

const (
	Algo_Argon2       = "Argon2"
	Argon2_Memory     = 524288
	Argon2_Operations = 4
	Argon2_Time       = 1
	Argon2_Threads    = 1
	Argon2_SaltBytes  = 8
	Argon2_StrBytes   = 128

	RandStringCharsetB58 = "abcdefghijkmnopqrstuvwxyz" +
		"ABCDEFGHJKLMNPQRSTUVWXYZ123456789" // Base58
	RandStringCharsetAZ = "abcdefghijklmnopqrstuvwxyz" // Only a-z
)

type Hash struct {
	Algo string
	Salt []byte
	Hash []byte
}

// Create random bytes of specified size
func RandBytes(size int) (data []byte) {
	data = make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		log.Error("Crypt: Unable to generate random bytes:", err)
	}
	return
}

// By default use base58
func RandString(size int) string {
	return RandStringCharset(size, RandStringCharsetB58)
}

// Create random string of specified size
func RandStringCharset(size int, charset string) string {
	data := make([]byte, size)
	charset_len := big.NewInt(int64(len(charset)))
	for i := range data {
		charset_pos, err := rand.Int(rand.Reader, charset_len)
		if err != nil {
			log.Error("Crypt: Failed to generate random string:", err)
		}
		data[i] = charset[charset_pos.Int64()]
	}
	return string(data)
}

// Generate a salted hash for the input string
func Generate(password string, salt []byte) (hash Hash) {
	hash.Algo = Algo_Argon2

	// Check salt and if not provided - use generator
	if salt != nil {
		hash.Salt = salt
	} else {
		hash.Salt = RandBytes(Argon2_SaltBytes)
	}

	// Create hash data
	hash.Hash = argon2.IDKey([]byte(password), hash.Salt,
		Argon2_Time, Argon2_Memory, Argon2_Threads, Argon2_StrBytes)

	return
}

// Compare string to generated hash
func (hash *Hash) IsEqual(password string) bool {
	return bytes.Compare(hash.Hash, Generate(password, hash.Salt).Hash) == 0
}

func (hash *Hash) IsEmpty() bool {
	return hash.Algo == ""
}
