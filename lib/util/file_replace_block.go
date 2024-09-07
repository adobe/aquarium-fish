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

// FileReplaceBlock is a simple block replace in the file
func FileReplaceBlock(path, blockFrom, blockTo string, lines ...string) error {
	// Open input file
	inF, err := os.OpenFile(path, os.O_RDONLY, 0o644)
	if err != nil {
		return err
	}
	defer inF.Close()

	// Check it's not a dir
	if info, err := inF.Stat(); err == nil && info.IsDir() {
		return fmt.Errorf("Util: Unable to replace block in directory")
	}

	// Open output file
	outF, err := os.CreateTemp(filepath.Dir(path), "tmp")
	if err != nil {
		return err
	}
	defer outF.Close()

	// Replace while copying
	sc := bufio.NewScanner(inF)
	foundFrom := false
	replaced := false
	for sc.Scan() {
		line := sc.Text()
		if replaced {
			if _, err := io.WriteString(outF, line+"\n"); err != nil {
				return err
			}
			continue
		}
		if !foundFrom {
			if strings.Contains(line, blockFrom) {
				foundFrom = true
				continue
			}
			if _, err := io.WriteString(outF, line+"\n"); err != nil {
				return err
			}
		} else {
			if strings.Contains(line, blockTo) {
				for _, l := range lines {
					if _, err := io.WriteString(outF, l+"\n"); err != nil {
						return err
					}
				}
				replaced = true
			}
		}
	}
	if sc.Err() != nil {
		return err
	}

	// Add in the end if was not replaced
	if foundFrom && !replaced {
		for _, l := range lines {
			if _, err := io.WriteString(outF, l+"\n"); err != nil {
				return err
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
	err = os.Rename(outF.Name(), path)

	return err
}
