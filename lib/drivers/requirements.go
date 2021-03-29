package drivers

import (
	"errors"
)

// Resource requirements
type Requirements struct {
	Cpu   uint            `json:"cpu"`   // Number of CPU cores to use
	Ram   uint            `json:"ram"`   // Amount of memory in GB
	Disks map[string]Disk `json:"disks"` // Disks to create and connect
}

type Disk struct {
	Size  uint `json:"size"`  // Amount of disk space in GB
	Reuse bool `json:"reuse"` // Do not remove the disk and reuse it for the next image run
}

func (r *Requirements) Validate() error {
	// Check resources
	if r.Cpu < 1 {
		return errors.New("Driver: Number of CPU cores is less then 1")
	}
	if r.Ram < 1 {
		return errors.New("Driver: Amount of RAM is less then 1GB")
	}
	for name, data := range r.Disks {
		if name == "" {
			return errors.New("Driver: Disk name can't be empty")
		}
		if data.Size < 1 {
			return errors.New("Driver: Size of the disk can't be less than 1GB")
		}
	}

	return nil
}
