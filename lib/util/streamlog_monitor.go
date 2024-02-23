/**
 * Copyright 2024 Adobe. All rights reserved.
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
	"bytes"

	"github.com/adobe/aquarium-fish/lib/log"
)

var LineBreak = []byte("\n")
var EmptyByte = []byte{}

// Wraps an existing io.Reader to monitor the log stream and adds prefix before each line
type StreamLogMonitor struct {
	Prefix  string   // Prefix for the line
	linebuf [][]byte // Where line will live until EOL or close
}

// Read 'overrides' the underlying io.Reader's Read method
func (slm *StreamLogMonitor) Write(p []byte) (int, error) {
	index := 0
	prev_index := 0
	for index < len(p) {
		index += bytes.Index(p[prev_index:], LineBreak)
		if index == -1 {
			// The data does not contain EOL, so appending to buffer and wait
			slm.linebuf = append(slm.linebuf, p)
			break
		}
		// The newline was found, so prepending the line buffer and print it out
		// We don't need the EOF in the line (log.Infof adds), so increment index after processing
		slm.linebuf = append(slm.linebuf, p[prev_index:index])
		log.Info(slm.Prefix + string(bytes.Join(slm.linebuf, EmptyByte)))
		clear(slm.linebuf)
		index++
		prev_index = index
	}

	return len(p), nil
}
