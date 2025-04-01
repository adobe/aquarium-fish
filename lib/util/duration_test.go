/**
 * Copyright 2025 Adobe. All rights reserved.
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
	"fmt"
	"testing"
	"time"
)

var (
	TestDurationParseString = [][2]string{
		{`0s`, `0s`},
		{`0d`, `0s`},
		{`0Y`, `0s`},
		{`1d`, `24h0m0s`},
		{`10d`, `240h0m0s`},
		{`1w`, `168h0m0s`},
		{`10s`, `10s`},
		{`10d5h2m3s`, `245h2m3s`},
		{`1M1d`, `744h0m0s`},
		{`-10y0w0d0h0m0s`, `-87600h0m0s`},
		{`10y`, `87600h0m0s`},
		{`1y1M1w1d1h1m1s`, `9673h1m1s`},
		{`1Y1M1W1D1h1m1s`, `9673h1m1s`},
		{`99y99M99w99d99h99m99s`, `957628h40m39s`},
	}
)

// Verify all the inputs will be parsed correctly
func Test_duration_parse_string(t *testing.T) {
	for _, testcase := range TestDurationParseString {
		t.Run(fmt.Sprintf("Testing `%s`", testcase[0]), func(t *testing.T) {
			out := Duration(0)
			err := out.StoreStringDuration(testcase[0])
			if time.Duration(out).String() != testcase[1] {
				t.Fatalf("Duration(`%s`) = `%s`, %v; want: `%s`", testcase[0], time.Duration(out), err, testcase[1])
			}
		})
	}
}
