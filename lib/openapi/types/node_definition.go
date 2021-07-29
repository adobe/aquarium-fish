package types

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
	"github.com/shirou/gopsutil/net"
)

func (nd NodeDefinition) GormDataType() string {
	return "blob"
}

func (nd *NodeDefinition) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New(fmt.Sprint("Failed to unmarshal JSONB value:", value))
	}

	err := json.Unmarshal(bytes, nd)
	return err
}

func (nd NodeDefinition) Value() (driver.Value, error) {
	return json.Marshal(nd)
}

func (nd *NodeDefinition) Update() {
	nd.Host, _ = host.Info()
	nd.Memory, _ = mem.VirtualMemory()
	nd.Cpu, _ = cpu.Info()

	if nd.Disks == nil {
		nd.Disks = make(map[string]*disk.UsageStat)
	}
	nd.Disks["/"], _ = disk.Usage("/")

	nd.Nets, _ = net.Interfaces()
}
