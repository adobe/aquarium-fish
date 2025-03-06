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
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// Update syncs the NodeDefinition to the current machine state
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
