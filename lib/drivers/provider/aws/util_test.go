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

package aws

import (
	"fmt"
	"testing"
	"time"
)

// Make sure there is no more issues in a simple logic of awsLastYearFilterValues like the Jan bug
func Test_awsLastYearFilterValues(t *testing.T) {
	// Next 100 years should be enough to be sure
	imagesTill := time.Now()
	endyear := imagesTill.AddDate(100, 0, 0).Year()
	for imagesTill.Year() < endyear {
		curryear := imagesTill.Year()
		t.Run(fmt.Sprintf("Testing year `%d`", curryear), func(t *testing.T) {
			for imagesTill.Year() == curryear {
				imagesTill = imagesTill.AddDate(0, 1, 0)
				out := awsLastYearFilterValues(imagesTill)
				if len(out) != 12 {
					t.Fatalf("awsLastYearFilterValues(`%s`) = `%s` (len = %d); want: 12 values", imagesTill.Format("2006-01"), out, len(out))
				}
			}
		})
	}
}
