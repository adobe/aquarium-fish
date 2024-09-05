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
	Argon2_Algo = "Argon2id"
	// Default tuned to process at least 20 API requests/sec on 2CPU
	Argon2_Memory     = 64 * 1024 // 64MB
	Argon2_Iterations = 1
	Argon2_Threads    = 8 // Optimal to quickly execute one request, with not much overhead
	Argon2_SaltLen    = 8
	Argon2_HashLen    = 32

	// <= v0.7.4 hash params for backward-compatibility
	// could easily choke the API system and cause OOMs so not recommended to use them
	v074_Argon2_Algo       = "Argon2"
	v074_Argon2_Memory     = 524288
	v074_Argon2_Iterations = 1
	v074_Argon2_Threads    = 1

	RandStringCharsetB58 = "abcdefghijkmnopqrstuvwxyz" +
		"ABCDEFGHJKLMNPQRSTUVWXYZ123456789" // Base58
	RandStringCharsetAZ = "abcdefghijklmnopqrstuvwxyz" // Only a-z
)

type Hash struct {
	Algo string
	Prop properties `gorm:"embedded;embeddedPrefix:prop_"`
	Salt []byte
	Hash []byte
}

// Properties of Argon2id algo
type properties struct {
	Memory     uint32
	Iterations uint32
	Threads    uint8
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

// Generate a salted hash for the input string with default parameters
func NewHash(input string, salt []byte) (h Hash) {
	h.Algo = Argon2_Algo
	if salt != nil {
		h.Salt = salt
	} else {
		h.Salt = RandBytes(Argon2_SaltLen)
	}
	h.Prop.Iterations = Argon2_Iterations
	h.Prop.Memory = Argon2_Memory
	h.Prop.Threads = Argon2_Threads

	// Create hash data
	h.Hash = argon2.IDKey([]byte(input), h.Salt, h.Prop.Iterations, h.Prop.Memory, h.Prop.Threads, Argon2_HashLen)

	return
}

// Check the input equal to the current hashed one
func (h *Hash) IsEqual(input string) bool {
	if h.Algo == v074_Argon2_Algo {
		// Legacy low-performant parameters, not defined in hash
		h.Prop.Iterations = v074_Argon2_Iterations
		h.Prop.Memory = v074_Argon2_Memory
		h.Prop.Threads = v074_Argon2_Threads
	}

	return bytes.Equal(h.Hash, argon2.IDKey([]byte(input), h.Salt, h.Prop.Iterations, h.Prop.Memory, h.Prop.Threads, uint32(len(h.Hash))))
}

func (hash *Hash) IsEmpty() bool {
	return hash.Algo == ""
}
