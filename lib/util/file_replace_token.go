package util

import (
	"bufio"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func FileReplaceToken(path string, token string, value string, full_line bool, add bool) error {
	// Open input file
	in_f, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer in_f.Close()

	// Open output file
	out_f, err := ioutil.TempFile(filepath.Dir(path), "new")
	if err != nil {
		return err
	}
	defer out_f.Close()

	// Replace while copying
	sc := bufio.NewScanner(in_f)
	replaced := false
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, token) {
			if full_line {
				line = value
			} else {
				strings.ReplaceAll(line, token, value)
			}
			replaced = true
		}
		// Probably not the best way to assume there was just \n
		if _, err := io.WriteString(out_f, line+"\n"); err != nil {
			return err
		}
	}
	if sc.Err() != nil {
		return err
	}

	// Add if was not replaced
	if !replaced && add {
		if _, err := io.WriteString(out_f, value+"\n"); err != nil {
			return err
		}
	}

	// Close the out file
	if err := out_f.Close(); err != nil {
		return err
	}

	// Close the input file
	if err := in_f.Close(); err != nil {
		return err
	}

	// Replace input file with out file
	if err := os.Rename(out_f.Name(), path); err != nil {
		return err
	}

	return nil
}
