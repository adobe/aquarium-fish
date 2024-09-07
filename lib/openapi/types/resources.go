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

package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"path"

	"github.com/adobe/aquarium-fish/lib/util"
)

// GormDataType describes how to store Resources in database
func (Resources) GormDataType() string {
	return "blob"
}

// Scan converts the Resources to json bytes
func (r *Resources) Scan(value any) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("Failed to unmarshal JSONB value: %s", value)
	}

	err := json.Unmarshal(bytes, r)

	// Init the value, otherwise will return undesired nil
	if err == nil && r.NodeFilter == nil {
		r.NodeFilter = []string{}
	}

	return err
}

// Value converts json bytes to Resources
func (r Resources) Value() (driver.Value, error) {
	// Init the value, otherwise will return undesired nil
	if r.NodeFilter == nil {
		r.NodeFilter = []string{}
	}
	return json.Marshal(r)
}

// Validate makes sure the Resources are defined correctly
func (r *Resources) Validate(diskTypes []string, checkNet bool) error {
	// Check resources
	if r.Cpu < 1 {
		return fmt.Errorf("Resources: Number of CPU cores is less then 1")
	}
	if r.Ram < 1 {
		return fmt.Errorf("Resources: Amount of RAM is less then 1GB")
	}
	for name, disk := range r.Disks {
		if name == "" {
			return fmt.Errorf("Resources: Disk name can't be empty")
		}
		if len(diskTypes) > 0 && !util.Contains(diskTypes, disk.Type) {
			return fmt.Errorf("Resources: Type of disk must be one of: %+q", diskTypes)
		}
		if disk.Size < 1 {
			return fmt.Errorf("Resources: Size of the disk can't be less than 1GB")
		}
	}
	if len(r.NodeFilter) > 0 {
		// Check filter patterns are correct
		for _, pattern := range r.NodeFilter {
			_, err := path.Match(pattern, "whatever")
			if err != nil {
				return fmt.Errorf("Resources: Bad pattern %q, please consult `path.Match` docs: %v", pattern, err)
			}
		}
	}
	if checkNet && r.Network != "" && r.Network != "nat" {
		return fmt.Errorf("Resources: The network configuration must be either '' (empty for hostonly) or 'nat'")
	}

	return nil
}

// Add increases the Resources utilization by provided Resources
func (r *Resources) Add(res Resources) error {
	if r.Cpu == 0 && r.Ram == 0 {
		// Set tenancy modificators for the first resource
		r.Multitenancy = res.Multitenancy
		r.CpuOverbook = res.CpuOverbook
		r.RamOverbook = res.RamOverbook
	}

	// Set the used CPU & RAM
	r.Cpu += res.Cpu
	r.Ram += res.Ram

	// TODO: Process disk too
	return nil
}

// Subtract decreases utilization of Resources by provided Resources
func (r *Resources) Subtract(res Resources) (err error) {
	if r.Cpu < res.Cpu {
		err = fmt.Errorf("Resources: Unable to subtract more CPU than we have: %d < %d", r.Cpu, res.Cpu)
		r.Cpu = 0
	} else {
		r.Cpu -= res.Cpu
	}
	if r.Ram < res.Ram {
		memErr := fmt.Errorf("Resources: Unable to subtract more RAM than we have: %d < %d", r.Ram, res.Ram)
		if err != nil {
			err = fmt.Errorf("%v, %v", err, memErr)
		}
		r.Ram = 0
	} else {
		r.Ram -= res.Ram
	}

	// TODO: Process disk too

	return
}

// IsEmpty checks if the Resources are filled with some values
func (r *Resources) IsEmpty() bool {
	if r.Cpu != 0 {
		return false
	}
	if r.Ram != 0 {
		return false
	}
	if len(r.Disks) > 0 {
		return false
	}

	return true
}
