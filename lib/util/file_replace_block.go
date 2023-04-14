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

func FileReplaceBlock(path, block_from, block_to string, lines ...string) error {
	// Open input file
	in_f, err := os.OpenFile(path, os.O_RDONLY, 0o644)
	if err != nil {
		return err
	}
	defer in_f.Close()

	// Check it's not a dir
	if info, err := in_f.Stat(); err == nil && info.IsDir() {
		return fmt.Errorf("Util: Unable to replace block in directory")
	}

	// Open output file
	out_f, err := os.CreateTemp(filepath.Dir(path), "tmp")
	if err != nil {
		return err
	}
	defer out_f.Close()

	// Replace while copying
	sc := bufio.NewScanner(in_f)
	found_from := false
	replaced := false
	for sc.Scan() {
		line := sc.Text()
		if replaced {
			if _, err := io.WriteString(out_f, line+"\n"); err != nil {
				return err
			}
			continue
		}
		if !found_from {
			if strings.Contains(line, block_from) {
				found_from = true
				continue
			}
			if _, err := io.WriteString(out_f, line+"\n"); err != nil {
				return err
			}
		} else {
			if strings.Contains(line, block_to) {
				for _, l := range lines {
					if _, err := io.WriteString(out_f, l+"\n"); err != nil {
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
	if found_from && !replaced {
		for _, l := range lines {
			if _, err := io.WriteString(out_f, l+"\n"); err != nil {
				return err
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
