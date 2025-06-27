/**
 * Copyright 2022-2025 Adobe. All rights reserved.
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

package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alessio/shellescape"
)

// SerializeMetadata serializes dictionary to usable format
func SerializeMetadata(format, prefix string, data map[string]any) (out []byte, err error) {
	var lines [][]byte
	switch format {
	case "json": // Default json
		return json.Marshal(data)
	case "env": // Plain format suitable to use in shell
		m := DotSerialize(prefix, data)
		for key, val := range m {
			line := cleanShellKey(strings.ReplaceAll(shellescape.StripUnsafe(key), ".", "_"))
			if len(line) == 0 {
				continue
			}
			value := []byte("=" + shellescape.Quote(val) + "\n")
			lines = append(lines, append(line, value...))
		}
	case "export": // Format env with exports for easy usage with source
		m := DotSerialize(prefix, data)
		for key, val := range m {
			line := cleanShellKey(strings.ReplaceAll(shellescape.StripUnsafe(key), ".", "_"))
			if len(line) == 0 {
				continue
			}
			line = append([]byte("export "), line...)
			value := []byte("=" + shellescape.Quote(val) + "\n")
			lines = append(lines, append(line, value...))
		}
	case "ps1": // Plain format suitable to use in powershell
		m := DotSerialize(prefix, data)
		for key, val := range m {
			line := cleanShellKey(strings.ReplaceAll(shellescape.StripUnsafe(key), ".", "_"))
			if len(line) == 0 {
				continue
			}
			// Shell quote is not applicable here, so using the custom one
			value := []byte("='" + strings.ReplaceAll(val, "'", "''") + "'\n")
			lines = append(lines, append([]byte("$"), append(line, value...)...))
		}
	default:
		return out, fmt.Errorf("Unsupported `format`: %s", format)
	}

	// We need to sort output to present it in similar way since map is not ordered
	sort.Slice(lines, func(i, j int) bool {
		return bytes.Compare(lines[i], lines[j]) < 0
	})

	for _, l := range lines {
		out = append(out, l...)
	}

	return out, nil
}

func cleanShellKey(in string) []byte {
	s := []byte(in)
	j := 0
	for _, b := range s {
		if j == 0 && ('0' <= b && b <= '9') {
			// Skip first numeric symbols
			continue
		}
		if ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || ('0' <= b && b <= '9') || b == '_' {
			s[j] = b
			j++
		}
	}
	return s[:j]
}
