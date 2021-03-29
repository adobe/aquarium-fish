package util

import (
	"bufio"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func FileReplaceToken(path string, full_line, add, anycase bool, token_values ...string) error {
	// Open input file
	in_f, err := os.OpenFile(path, os.O_RDONLY, 0o644)
	if err != nil {
		return err
	}
	defer in_f.Close()

	// Check it's not a dir
	if info, err := in_f.Stat(); err == nil && info.IsDir() {
		return errors.New("Util: Unable to replace token in directory")
	}

	// Open output file
	out_f, err := ioutil.TempFile(filepath.Dir(path), "tmp")
	if err != nil {
		return err
	}
	defer out_f.Close()

	var tokens []string
	var values []string

	for i, tv := range token_values {
		if i%2 == 0 {
			if anycase {
				tokens = append(tokens, strings.ToLower(tv))
			} else {
				tokens = append(tokens, tv)
			}
		} else {
			values = append(values, tv)
		}
	}

	replaced := make([]bool, len(values))

	// Replace while copying
	sc := bufio.NewScanner(in_f)
	for sc.Scan() {
		line := sc.Text()
		comp_line := line
		if anycase {
			comp_line = strings.ToLower(line)
		}
		for i, value := range values {
			if strings.Contains(comp_line, tokens[i]) {
				replaced[i] = true
				if full_line {
					line = value
					break // No need to check the other tokens
				} else {
					strings.ReplaceAll(line, tokens[i], value)
				}
			}
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
	if add {
		for i, value := range values {
			if !replaced[i] {
				if _, err := io.WriteString(out_f, value+"\n"); err != nil {
					return err
				}
			}
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
