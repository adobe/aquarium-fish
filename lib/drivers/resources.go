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
// It's used for 2 purposes: in Label definitions to describe the required amount of resources and
// in Fish to store the currently used resources, so could add and subtract resources.
// Modificators are used for parallel node usage by different Applications, they are stored for the
// first Application and used for the others to determine node tenancy/overbook tolerance.
type Resources struct {
	Cpu     uint            `json:"cpu"`     // Number of CPU cores to use
	Ram     uint            `json:"ram"`     // Amount of memory in GB
	Disks   map[string]Disk `json:"disks"`   // Disks to create and connect
	Network string          `json:"network"` // Which network configuration to use for VM

	// The modificators to simultaneous execution
	Multitenancy bool `json:"multitenancy"` // Tolerate to run along with the others
	CpuOverbook  bool `json:"cpu_overbook"` // Tolerate to CPU overbooking
	RamOverbook  bool `json:"ram_overbook"` // Tolerate to RAM overbooking
}

type Disk struct {
	Type  string `json:"type"`  // Type of the filesystem to create
	Label string `json:"label"` // Volume name will be given to the disk, empty will use the disk key
	Size  uint   `json:"size"`  // Amount of disk space in GB
	Reuse bool   `json:"reuse"` // Do not remove the disk and reuse it for the next resource run
	Clone string `json:"clone"` // Clone the snapshot of existing disk instead of creating the new one
}

func (r *Resources) Validate(disk_types []string, check_net bool) error {
	// Check resources
	if r.Cpu < 1 {
		return fmt.Errorf("Driver: Number of CPU cores is less then 1")
	}
	if r.Ram < 1 {
		return fmt.Errorf("Driver: Amount of RAM is less then 1GB")
	}
	for name, disk := range r.Disks {
		if name == "" {
			return fmt.Errorf("Driver: Disk name can't be empty")
		}
		if len(disk_types) > 0 && !util.Contains(disk_types, disk.Type) {
			return fmt.Errorf(fmt.Sprintf("Driver: Type of disk must be one of: %+q", disk_types))
		}
		if disk.Size < 1 {
			return fmt.Errorf("Driver: Size of the disk can't be less than 1GB")
		}
	}
	if check_net && r.Network != "" && r.Network != "nat" {
		return fmt.Errorf("Driver: The network configuration must be either '' (empty for hostonly) or 'nat'")
	}

	return nil
}

// Adds the Resources data to the existing data
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

// Subtracts the Resources data to the existing data
func (r *Resources) Subtract(res Resources) (err error) {
	if r.Cpu < res.Cpu {
		err = fmt.Errorf("Driver: Unable to subtract more CPU than we have: %d < %d", r.Cpu, res.Cpu)
		r.Cpu = 0
	} else {
		r.Cpu -= res.Cpu
	}
	if r.Ram < res.Ram {
		mem_err := fmt.Errorf("Driver: Unable to subtract more RAM than we have: %d < %d", r.Ram, res.Ram)
		if err != nil {
			err = fmt.Errorf("%v, %v", err, mem_err)
		}
		r.Cpu = 0
	} else {
		r.Cpu -= res.Cpu
	}

	// TODO: Process disk too

	return
}

// Checks if the Resources are filled with some values
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
