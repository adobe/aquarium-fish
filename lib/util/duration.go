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
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Duration is a simple wrapper to add serialization functions
type Duration time.Duration

var unitMap = map[string]Duration{
	"d": 24,
	"D": 24,
	"w": 7 * 24,
	"W": 7 * 24,
	"M": 30 * 24,
	"y": 365 * 24,
	"Y": 365 * 24,
}

// MarshalJSON represents Duration as JSON string
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON parses JSON string as Duration
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		(*d).StoreStringDuration(value)
		return nil
	default:
		return fmt.Errorf("incorrect duration type")
	}
}

// StoreStringDuration parses a duration string into a duration
// Example: "10d", "-1.5w" or "3Y4M5d"
// Added time units: d(D), w(W), M, y(Y)
func (d *Duration) StoreStringDuration(s string) error {
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}

	re := regexp.MustCompile(`(\d*\.\d+|\d+)[^\d]*`)
	strs := re.FindAllString(s, -1)
	var sumDur Duration
	for _, str := range strs {
		var hours Duration = 1
		for unit, h := range unitMap {
			if strings.Contains(str, unit) {
				str = strings.ReplaceAll(str, unit, "h")
				hours = h
				break
			}
		}

		dur, err := time.ParseDuration(str)
		if err != nil {
			return err
		}

		sumDur += Duration(dur) * hours
	}

	if neg {
		sumDur = -sumDur
	}

	*d = sumDur

	return nil
}
