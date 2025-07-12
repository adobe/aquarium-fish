/**
 * Copyright 2024-2025 Adobe. All rights reserved.
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

	"github.com/adobe/aquarium-fish/lib/log"
)

var lineBreak = []byte("\n")
var emptyByte = []byte{}

// StreamLogMonitor wraps an existing io.Reader to monitor the log stream and adds prefix before each line
type StreamLogMonitor struct {
	Prefix  string   // Prefix for the line
	linebuf [][]byte // Where line will live until EOL or close
}

// Write will read 'overrides' the underlying io.Reader's Read method
func (slm *StreamLogMonitor) Write(p []byte) (int, error) {
	logger := log.WithFunc("util", "StreamLogMonitor.Write")
	index := 0
	prevIndex := 0
	for index < len(p) {
		index += bytes.Index(p[prevIndex:], lineBreak)
		if index == -1 {
			// The data does not contain EOL, so appending to buffer and wait
			slm.linebuf = append(slm.linebuf, p)
			break
		}
		// The newline was found, so prepending the line buffer and print it out
		// We don't need the EOF in the line (log.Info().Msgf adds), so increment index after processing
		slm.linebuf = append(slm.linebuf, p[prevIndex:index])
		logger.Info(slm.Prefix + string(bytes.Join(slm.linebuf, emptyByte)))
		clear(slm.linebuf)
		index++
		prevIndex = index
	}

	return len(p), nil
}
