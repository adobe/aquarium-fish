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
	"strconv"
	"strings"
)

type HumanSize uint64

const (
	B  HumanSize = 1
	KB           = B << 10
	MB           = KB << 10
	GB           = MB << 10
	TB           = GB << 10
	PB           = TB << 10
	EB           = PB << 10

	fnUnmarshalText string = "UnmarshalText"
	maxUint64       uint64 = (1 << 64) - 1
	cutoff          uint64 = maxUint64 / 10
)

func NewHumanSize(input string) (HumanSize, error) {
	var hs HumanSize
	err := hs.UnmarshalText([]byte(input))
	return hs, err
}

func (hs HumanSize) MarshalText() ([]byte, error) {
	return []byte(hs.String()), nil
}

// To be properly parsed the text should contain number and unit ("B", "KB", "MB"...) in the end
func (hs *HumanSize) UnmarshalText(data []byte) error {
	input := strings.TrimSpace(string(data))
	length := len(input)

	// Detecting unit & multiplier
	var mult HumanSize = 0
	unit := ""
	unit_len := 0
	if length > 1 {
		unit = input[length-2:]
		unit_len = 2
	} else {
		unit = input
		unit_len = length
	}
	switch unit {
	case "KB":
		mult = KB
	case "MB":
		mult = MB
	case "GB":
		mult = GB
	case "TB":
		mult = TB
	case "PB":
		mult = PB
	case "EB":
		mult = EB
	default:
		// Could be something incorrect, B or number - so bytes
		if unit[0] >= '0' && unit[0] <= '9' {
			// It's byte
			if unit_len > 1 {
				if unit[1] == 'B' {
					unit_len = 1
				} else if unit[1] >= '0' && unit[1] <= '9' {
					unit_len = 0
				} else {
					mult = 0
				}
			} else {
				unit_len = 0
			}
			unit = "B"
			mult = B
		}
	}
	if mult == 0 {
		return fmt.Errorf("Unable to parse provided human size unit: %s", input)
	}

	// Detecting value
	val, err := strconv.ParseUint(input[:length-unit_len], 10, 64)
	if err != nil {
		return fmt.Errorf("Unable to parse provided human size value: %s", input)
	}

	if mult != B && val > maxUint64/uint64(mult) {
		// Overflow
		return fmt.Errorf("Unable to store provided human size value in bytes: max uint64 < %s", input)
	}

	*hs = HumanSize(val * uint64(mult))

	return nil
}

func (hs HumanSize) Bytes() uint64 {
	return uint64(hs)
}

func (hs HumanSize) String() string {
	switch {
	case hs == 0:
		return fmt.Sprint("0B")
	case hs%EB == 0:
		return fmt.Sprintf("%dEB", hs/EB)
	case hs%PB == 0:
		return fmt.Sprintf("%dPB", hs/PB)
	case hs%TB == 0:
		return fmt.Sprintf("%dTB", hs/TB)
	case hs%GB == 0:
		return fmt.Sprintf("%dGB", hs/GB)
	case hs%MB == 0:
		return fmt.Sprintf("%dMB", hs/MB)
	case hs%KB == 0:
		return fmt.Sprintf("%dKB", hs/KB)
	default:
		return fmt.Sprintf("%dB", hs)
	}
}
