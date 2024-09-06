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
	"fmt"
	"testing"
)

var (
	TestHumanSizeParseString = [][2]string{
		{`0`, `0B`},
		{`0B`, `0B`},
		{`0EB`, `0B`},
		{`5B`, `5B`},
		{`9`, `9B`},
		{`10`, `10B`},
		{`512`, `512B`},
		{`1024`, `1KB`},
		{`1048576`, `1MB`},
		{`110MB`, `110MB`},
		{`1024MB`, `1GB`},
		{`155GB`, `155GB`},
		{`1024GB`, `1TB`},
		{`128TB`, `128TB`},
		{`1024TB`, `1PB`},
		{`169PB`, `169PB`},
		{`1024PB`, `1EB`},
		{`15EB`, `15EB`}, // Maximum
	}
)

// Verify all the inputs will be parsed correctly
func Test_human_size_parse_string(t *testing.T) {
	for _, testcase := range TestHumanSizeParseString {
		t.Run(fmt.Sprintf("Testing `%s`", testcase[0]), func(t *testing.T) {
			out, err := NewHumanSize(testcase[0])
			if out.String() != testcase[1] {
				t.Fatalf("NewHumanSize(`%s`) = `%s`, %v; want: `%s`", testcase[0], out, err, testcase[1])
			}
		})
	}
}
