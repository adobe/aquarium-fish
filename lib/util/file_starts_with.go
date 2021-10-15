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
