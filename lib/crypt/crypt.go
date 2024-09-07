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

// Package crypt contains a number of cryptographic functions
package crypt

import (
	"bytes"
	"crypto/rand"
	"math/big"

	"golang.org/x/crypto/argon2"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Default parameters for the Argon2 hashing and some charsets usable for representing the data
const (
	Argon2Algo = "Argon2id"
	// Default tuned to process at least 20 API requests/sec on 2CPU
	Argon2Memory     = 64 * 1024 // 64MB
	Argon2Iterations = 1
	Argon2Threads    = 8 // Optimal to quickly execute one request, with not much overhead
	Argon2Saltlen    = 8
	Argon2Hashlen    = 32

	// <= v0.7.4 hash params for backward-compatibility
	// could easily choke the API system and cause OOMs so not recommended to use them
	v074Argon2Algo       = "Argon2"
	v074Argon2Memory     = 524288
	v074Argon2Iterations = 1
	v074Argon2Threads    = 1

	RandStringCharsetB58 = "abcdefghijkmnopqrstuvwxyz" +
		"ABCDEFGHJKLMNPQRSTUVWXYZ123456789" // Base58
	RandStringCharsetAZ = "abcdefghijklmnopqrstuvwxyz" // Only a-z
)

// Hash contains everything needed for storing and reproducing password hash
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

// RandBytes create random bytes of specified size
func RandBytes(size int) (data []byte) {
	data = make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		log.Error("Crypt: Unable to generate random bytes:", err)
	}
	return
}

// RandString generates random string with base58 characters
func RandString(size int) string {
	return RandStringCharset(size, RandStringCharsetB58)
}

// RandStringCharset creates random string of specified size
func RandStringCharset(size int, charset string) string {
	data := make([]byte, size)
	charsetLen := big.NewInt(int64(len(charset)))
	for i := range data {
		charsetPos, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			log.Error("Crypt: Failed to generate random string:", err)
		}
		data[i] = charset[charsetPos.Int64()]
	}
	return string(data)
}

// NewHash generates a salted hash for the input string with default parameters
func NewHash(input string, salt []byte) (h Hash) {
	h.Algo = Argon2Algo
	if salt != nil {
		h.Salt = salt
	} else {
		h.Salt = RandBytes(Argon2Saltlen)
	}
	h.Prop.Iterations = Argon2Iterations
	h.Prop.Memory = Argon2Memory
	h.Prop.Threads = Argon2Threads

	// Create hash data
	h.Hash = argon2.IDKey([]byte(input), h.Salt, h.Prop.Iterations, h.Prop.Memory, h.Prop.Threads, Argon2Hashlen)

	return
}

// IsEqual checks the input equal to the current hashed one
func (h *Hash) IsEqual(input string) bool {
	if h.Algo == v074Argon2Algo {
		// Legacy low-performant parameters, not defined in hash
		h.Prop.Iterations = v074Argon2Iterations
		h.Prop.Memory = v074Argon2Memory
		h.Prop.Threads = v074Argon2Threads
	}

	return bytes.Equal(h.Hash, argon2.IDKey([]byte(input), h.Salt, h.Prop.Iterations, h.Prop.Memory, h.Prop.Threads, uint32(len(h.Hash))))
}

// IsEmpty shows is the hash is actually not filled with data
func (h *Hash) IsEmpty() bool {
	return h.Algo == ""
}
