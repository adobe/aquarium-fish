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
func Test_awsLastYearFilterValues_dynamic(t *testing.T) {
	// Next 10 years should be enough to be sure
	imagesTill := time.Now()
	endyear := imagesTill.AddDate(10, 0, 0).Year()
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

func Test_awsLastYearFilterValues_static(t *testing.T) {
	imagesTill, _ := time.Parse("2006-01-02", "2025-05-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-* 2024-11-* 2024-10-* 2024-09-* 2024-08-* 2024-07-* 2024-06-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-06-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-* 2024-11-* 2024-10-* 2024-09-* 2024-08-* 2024-07-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-07-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-* 2024-11-* 2024-10-* 2024-09-* 2024-08-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-08-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-* 2024-11-* 2024-10-* 2024-09-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-09-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-09-* 2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-* 2024-11-* 2024-10-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-10-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-10-* 2025-09-* 2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-* 2024-11-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-11-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-11-* 2025-10-* 2025-09-* 2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-* 2024-12-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2025-12-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2025-12-* 2025-11-* 2025-10-* 2025-09-* 2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-* 2025-01-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2026-01-29")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2026-01-* 2025-12-* 2025-11-* 2025-10-* 2025-09-* 2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-* 2025-02-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
	imagesTill, _ = time.Parse("2006-01-02", "2026-02-28")
	t.Run(fmt.Sprintf("Testing date `%s`", imagesTill), func(t *testing.T) {
		want := "[2026-02-* 2026-01-* 2025-12-* 2025-11-* 2025-10-* 2025-09-* 2025-08-* 2025-07-* 2025-06-* 2025-05-* 2025-04-* 2025-03-*]"
		out := fmt.Sprintf("%s", awsLastYearFilterValues(imagesTill))
		if out != want {
			t.Fatalf("awsLastYearFilterValues(`%s`) = `%s`, but should be: %s", imagesTill.Format("2006-01"), out, want)
		}
	})
}
