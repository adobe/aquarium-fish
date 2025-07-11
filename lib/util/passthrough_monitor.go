/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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
	"fmt"
	"io"
	"time"

	"github.com/adobe/aquarium-fish/lib/log"
)

// PassThruMonitor wraps an existing io.Reader to monitor the stream
//
// It simply forwards the Read() call, while displaying
// the results from individual calls to it.
type PassThruMonitor struct {
	io.Reader
	Name   string // Prefix for the message
	Length int64  // Expected length

	total    int64
	progress float64
	printTs  time.Time
}

// Read 'overrides' the underlying io.Reader's Read method.
// This is the one that will be called by io.Copy(). We simply
// use it to keep track of byte counts and then forward the call.
func (pt *PassThruMonitor) Read(p []byte) (int, error) {
	n, err := pt.Reader.Read(p)
	if n > 0 {
		pt.total += int64(n)
		percentage := float64(pt.total) / float64(pt.Length) * float64(100)
		if percentage-pt.progress > 10 || time.Since(pt.printTs) > 30*time.Second {
			// Show status every 10% or 30 sec
			log.WithFunc("util", "PassThruMonitor.Read").Info(fmt.Sprintf("%s: %v%% (%dB)", pt.Name, int(percentage), pt.total))
			pt.progress = percentage
			pt.printTs = time.Now()
		}
	}

	return n, err
}
