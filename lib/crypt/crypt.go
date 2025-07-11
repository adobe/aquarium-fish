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

// Package crypt contains a number of cryptographic functions
package crypt

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"

	"golang.org/x/crypto/argon2"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
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

	RandStringCharsetB58 = "abcdefghijkmnopqrstuvwxyz" +
		"ABCDEFGHJKLMNPQRSTUVWXYZ123456789" // Base58
	RandStringCharsetAZ = "abcdefghijklmnopqrstuvwxyz" // Only a-z
)

// Hash contains everything needed for storing and reproducing password hash
type Hash struct {
	Algo string     `json:"algo"`
	Prop properties `json:"prop"`
	Salt []byte     `json:"salt"`
	Hash []byte     `json:"hash"`
}

// Properties of Argon2id algo
type properties struct {
	Memory     uint32 `json:"memory"`
	Iterations uint32 `json:"iterations"`
	Threads    uint8  `json:"threads"`
}

// RandBytes create random bytes of specified size
func RandBytes(size int) (data []byte) {
	data = make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		log.WithFunc("crypt", "RandBytes").Error("Unable to generate random bytes", "err", err)
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
			log.WithFunc("crypt", "RandStringCharset").Error("Failed to generate random string", "err", err)
		}
		data[i] = charset[charsetPos.Int64()]
	}
	return string(data)
}

// NewHash generates a salted hash for the input string with default parameters
func NewHash(input string, salt []byte) (h *Hash) {
	h = &Hash{
		Algo: Argon2Algo,
		Prop: properties{
			Iterations: Argon2Iterations,
			Memory:     Argon2Memory,
			Threads:    Argon2Threads,
		},
	}
	if salt != nil {
		h.Salt = salt
	} else {
		h.Salt = RandBytes(Argon2Saltlen)
	}

	// Create hash data
	h.Hash = argon2.IDKey([]byte(input), h.Salt, h.Prop.Iterations, h.Prop.Memory, h.Prop.Threads, Argon2Hashlen)

	return h
}

// IsEqual checks the input equal to the current hashed one
func (h *Hash) IsEqual(input string) bool {
	return bytes.Equal(h.Hash, argon2.IDKey([]byte(input), h.Salt, h.Prop.Iterations, h.Prop.Memory, h.Prop.Threads, uint32(len(h.Hash))))
}

// IsEmpty shows is the hash is actually not filled with data
func (h *Hash) IsEmpty() bool {
	return h.Algo == ""
}

func (h Hash) Serialize() (util.UnparsedJSON, error) {
	jsonHash, err := json.Marshal(h)
	if err != nil {
		log.WithFunc("crypt", "Serialize").Error("Unable to serialize Hash", "err", err)
		return util.UnparsedJSON("{}"), fmt.Errorf("Unable to serialize Hash: %v", err)
	}
	return util.UnparsedJSON(jsonHash), nil
}

func (h *Hash) Deserialize(jsonHash util.UnparsedJSON) error {
	err := json.Unmarshal([]byte(jsonHash), h)
	if err != nil {
		log.WithFunc("crypt", "Deserialize").Error("Unable to deserialize Hash", "err", err)
		return fmt.Errorf("Unable to deserialize Hash: %v", err)
	}
	return nil
}
