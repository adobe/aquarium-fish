package crypt

import (
	"bytes"
	"crypto/rand"
	"log"
	"math/big"

	"golang.org/x/crypto/argon2"
)

const (
	Algo_Argon2       = "Argon2"
	Argon2_Memory     = 524288
	Argon2_Operations = 4
	Argon2_Time       = 1
	Argon2_Threads    = 1
	Argon2_SaltBytes  = 8
	Argon2_StrBytes   = 128

	rand_string_charset = "abcdefghijkmnopqrstuvwxyz" +
		"ABCDEFGHJKLMNPQRSTUVWXYZ123456789" // Base58
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
		log.Panic("Err generating random bytes", err)
	}
	return
}

// Create random string of specified size
func RandString(size int) string {
	data := make([]byte, size)
	charset_len := big.NewInt(int64(len(rand_string_charset)))
	for i := range data {
		charset_pos, err := rand.Int(rand.Reader, charset_len)
		if err != nil {
			log.Panic("Err generating random string", err)
		}
		data[i] = rand_string_charset[charset_pos.Int64()]
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
