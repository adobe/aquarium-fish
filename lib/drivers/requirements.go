/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package drivers

import (
	"fmt"

	"github.com/adobe/aquarium-fish/lib/util"
)

// Resource requirements
type Requirements struct {
	Cpu     uint            `json:"cpu"`     // Number of CPU cores to use
	Ram     uint            `json:"ram"`     // Amount of memory in GB
	Disks   map[string]Disk `json:"disks"`   // Disks to create and connect
	Network string          `json:"network"` // Which network configuration to use for VM
}

type Disk struct {
	Type  string `json:"type"`  // Type of the filesystem to create
	Label string `json:"label"` // Volume name will be given to the disk, empty will use the disk key
	Size  uint   `json:"size"`  // Amount of disk space in GB
	Reuse bool   `json:"reuse"` // Do not remove the disk and reuse it for the next resource run
	Clone string `json:"clone"` // Clone the snapshot of existing disk instead of creating the new one
}

func (r *Requirements) Validate(disk_types []string) error {
	// Check resources
	if r.Cpu < 1 {
		return fmt.Errorf("Driver: Number of CPU cores is less then 1")
	}
	if r.Ram < 1 {
		return fmt.Errorf("Driver: Amount of RAM is less then 1GB")
	}
	for name, data := range r.Disks {
		if name == "" {
			return fmt.Errorf("Driver: Disk name can't be empty")
		}
		if !util.Contains(disk_types, data.Type) {
			return fmt.Errorf(fmt.Sprintf("Driver: Type of disk must be one of: %+q", disk_types))
		}
		if data.Size < 1 {
			return fmt.Errorf("Driver: Size of the disk can't be less than 1GB")
		}
	}
	if r.Network != "" && r.Network != "nat" {
		return fmt.Errorf("Driver: The network configuration must be either '' (empty for hostonly) or 'nat'")
	}

	return nil
}
