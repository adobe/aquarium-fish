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

package util

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func FileReplaceToken(path string, fullLine, add, anycase bool, tokenValues ...string) error {
	// Open input file
	inF, err := os.OpenFile(path, os.O_RDONLY, 0o644)
	if err != nil {
		return err
	}
	defer inF.Close()

	// Check it's not a dir
	if info, err := inF.Stat(); err == nil && info.IsDir() {
		return fmt.Errorf("Util: Unable to replace token in directory")
	}

	// Open output file
	outF, err := os.CreateTemp(filepath.Dir(path), "tmp")
	if err != nil {
		return err
	}
	defer outF.Close()

	var tokens []string
	var values []string

	// Walking through the list of tokens to split them into pairs
	// 0 - key, 1 - value
	for i, tv := range tokenValues {
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
	sc := bufio.NewScanner(inF)
	for sc.Scan() {
		line := sc.Text()
		compLine := line
		if anycase {
			compLine = strings.ToLower(line)
		}
		for i, value := range values {
			if strings.Contains(compLine, tokens[i]) {
				replaced[i] = true
				if fullLine {
					line = value
					break // No need to check the other tokens
				} else {
					if anycase {
						// We're not using RE because it's hard to predict the token
						// and escape it to compile the proper regular expression
						// so instead we using just regular replace by position of the token
						idx := strings.Index(compLine, tokens[i])
						for idx != -1 {
							// To support unicode use runes
							line = string([]rune(line)[0:idx]) + value + string([]rune(line)[idx+len(tokens[i]):len(line)])
							compLine = strings.ToLower(line)
							idx = strings.Index(compLine, tokens[i])
						}
					} else {
						line = strings.ReplaceAll(line, tokens[i], value)
					}
				}
			}
		}
		// Probably not the best way to assume there was just \n
		if _, err := io.WriteString(outF, line+"\n"); err != nil {
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
				if _, err := io.WriteString(outF, value+"\n"); err != nil {
					return err
				}
			}
		}
	}

	// Close the out file
	if err := outF.Close(); err != nil {
		return err
	}

	// Close the input file
	if err := inF.Close(); err != nil {
		return err
	}

	// Replace input file with out file
	if err := os.Rename(outF.Name(), path); err != nil {
		return err
	}

	return nil
}
