/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package aquariumv2

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// NodePingDelay defines delay between the pings to keep the node active in the cluster
const NodePingDelay = 10

// Init prepares Node for usage
func (n *Node) Init(nodeAddress, certPath string) error {
	// Set the node external address
	n.Address = nodeAddress

	// Read certificate's pubkey to put or compare
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(certBytes)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	pubkeyDer, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return err
	}

	// Update the pubkey once - it can not be changed for the node name for now,
	// maybe later the process of key switch will be implemented
	if n.Pubkey == nil {
		// Set the pubkey once
		n.Pubkey = pubkeyDer
	} else {
		// Validate the existing pubkey
		if !bytes.Equal(n.Pubkey, pubkeyDer) {
			return fmt.Errorf("Fish Node: The pubkey was changed for Node, that's not supported")
		}
	}

	// Collect the node definition data
	n.Definition.Update()

	return nil
}

// Update syncs the NodeDefinition to the current machine state
func (nd *NodeDefinition) Update() {
	// Get host info
	if hostInfo, err := host.Info(); err == nil {
		nd.Host = HostInfo{
			Hostname:        hostInfo.Hostname,
			Os:              hostInfo.OS,
			Platform:        hostInfo.Platform,
			PlatformFamily:  hostInfo.PlatformFamily,
			PlatformVersion: hostInfo.PlatformVersion,
			KernelVersion:   hostInfo.KernelVersion,
			KernelArch:      hostInfo.KernelArch,
		}
	}

	// Get memory info
	if memInfo, err := mem.VirtualMemory(); err == nil {
		nd.Memory = MemoryInfo{
			Total:       memInfo.Total,
			Available:   memInfo.Available,
			Used:        memInfo.Used,
			UsedPercent: float32(memInfo.UsedPercent),
		}
	}

	// Get CPU info
	if cpuInfo, err := cpu.Info(); err == nil {
		nd.Cpu = make([]CpuInfo, len(cpuInfo))
		for i, info := range cpuInfo {
			nd.Cpu[i] = CpuInfo{
				Cpu:        fmt.Sprintf("%d", info.CPU),
				VendorId:   info.VendorID,
				Family:     info.Family,
				Model:      info.Model,
				Stepping:   fmt.Sprintf("%d", info.Stepping),
				PhysicalId: info.PhysicalID,
				CoreId:     info.CoreID,
				Cores:      int32(info.Cores),
				ModelName:  info.ModelName,
				Mhz:        float32(info.Mhz),
				CacheSize:  fmt.Sprintf("%d", info.CacheSize),
				Microcode:  info.Microcode,
			}
		}
	}

	// Initialize disks map if nil
	if nd.Disks == nil {
		nd.Disks = make(map[string]DiskUsage)
	}

	// Get disk usage for root
	if diskInfo, err := disk.Usage("/"); err == nil {
		nd.Disks["/"] = DiskUsage{
			Path:        diskInfo.Path,
			Fstype:      diskInfo.Fstype,
			Total:       diskInfo.Total,
			Free:        diskInfo.Free,
			Used:        diskInfo.Used,
			UsedPercent: float32(diskInfo.UsedPercent),
		}
	}

	// Get network interfaces
	if netInfo, err := net.Interfaces(); err == nil {
		nd.Nets = make([]NetworkInterface, len(netInfo))
		for i, info := range netInfo {
			addrs := make([]string, len(info.Addrs))
			for j, addr := range info.Addrs {
				addrs[j] = addr.Addr
			}
			nd.Nets[i] = NetworkInterface{
				Name:  info.Name,
				Addrs: addrs,
				Flags: info.Flags,
			}
		}
	}
}
