package drivers

import (
	"errors"
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
	Reuse bool   `json:"reuse"` // Do not remove the disk and reuse it for the next image run
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
		if data.Type != "hfs+" && data.Type != "exfat" && data.Type != "fat32" {
			return errors.New("Driver: Type of disk must be either 'hfs+', 'exfat' or 'fat32'")
		}
		if data.Size < 1 {
			return errors.New("Driver: Size of the disk can't be less than 1GB")
		}
	}
	if r.Network != "" && r.Network != "nat" {
		return errors.New("Driver: The network configuration must be either '' (empty for hosted) or 'nat'")
	}

	return nil
}
