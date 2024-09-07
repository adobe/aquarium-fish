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

package fish

import (
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	"github.com/mostlygeek/arp"
	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// ResourceFind lists Resources that fits filter
func (f *Fish) ResourceFind(filter *string) (rs []types.Resource, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return rs, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&rs).Error
	return rs, err
}

// ResourceListNode returns list of resources for provided NodeUID
func (f *Fish) ResourceListNode(nodeUID types.NodeUID) (rs []types.Resource, err error) {
	err = f.db.Where("node_uid = ?", nodeUID).Find(&rs).Error
	return rs, err
}

// ResourceCreate makes new Resource
func (f *Fish) ResourceCreate(r *types.Resource) error {
	if r.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if r.LabelUID == uuid.Nil {
		return fmt.Errorf("Fish: LabelUID can't be unset")
	}
	if r.NodeUID == uuid.Nil {
		return fmt.Errorf("Fish: NodeUID can't be unset")
	}
	if len(r.Identifier) == 0 {
		return fmt.Errorf("Fish: Identifier can't be empty")
	}
	// TODO: check JSON
	if len(r.Metadata) < 2 {
		return fmt.Errorf("Fish: Metadata can't be empty")
	}

	r.UID = f.NewUID()
	return f.db.Create(r).Error
}

// ResourceDelete removes Resource
func (f *Fish) ResourceDelete(uid types.ResourceUID) error {
	// First delete any references to this resource.
	err := f.ResourceAccessDeleteByResource(uid)
	if err != nil {
		log.Errorf("Unable to delete ResourceAccess associated with Resource UID=%v: %v", uid, err)
	}
	// Now purge the resource.
	return f.db.Delete(&types.Resource{}, uid).Error
}

// ResourceSave stores Resource
func (f *Fish) ResourceSave(res *types.Resource) error {
	return f.db.Save(res).Error
}

// ResourceGet returns Resource by it's UID
func (f *Fish) ResourceGet(uid types.ResourceUID) (res *types.Resource, err error) {
	res = &types.Resource{}
	err = f.db.First(res, uid).Error
	return res, err
}

func fixHwAddr(hwaddr string) string {
	split := strings.Split(hwaddr, ":")
	if len(split) == 6 {
		// MAC address fix
		for i, v := range split {
			split[i] = fmt.Sprintf("%02s", v)
		}
		hwaddr = strings.Join(split, ":")
	}

	return hwaddr
}

func checkIPv4Address(network *net.IPNet, ip net.IP) bool {
	// Processing only networks we controlling (IPv4)
	// TODO: not 100% ensurance over the network control, but good enough for now
	if !strings.HasSuffix(network.IP.String(), ".1") {
		return false
	}

	// Make sure checked IP is in the network
	if !network.Contains(ip) {
		return false
	}

	return true
}

func isControlledNetwork(ip string) bool {
	// Relatively long process executed for each request, but gives us flexibility
	// TODO: Could be optimized to collect network data on start or periodically
	ipParsed := net.ParseIP(ip)

	ifaces, err := net.Interfaces()
	if err != nil {
		log.Errorf("Unable to get the available network interfaces: %+v\n", err.Error())
		return false
	}

	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			log.Errorf("Unable to get available addresses of the interface %s: %+v\n", i.Name, err.Error())
			continue
		}

		for _, a := range addrs {
			if v, ok := a.(*net.IPNet); ok && checkIPv4Address(v, ipParsed) {
				return true
			}
		}
	}
	return false
}

// ResourceGetByIP returns Resource by it's IP address
func (f *Fish) ResourceGetByIP(ip string) (res *types.Resource, err error) {
	res = &types.Resource{}

	// Check by IP first
	err = f.db.Where("node_uid = ?", f.GetNodeUID()).Where("ip_addr = ?", ip).First(res).Error
	if err == nil {
		// Check if the state is allocated to prevent old resources access
		if f.ApplicationIsAllocated(res.ApplicationUID) != nil {
			return nil, fmt.Errorf("Fish: Prohibited to access the Resource of not allocated Application")
		}

		return res, nil
	}

	// Make sure the IP is the controlled network, otherwise someone from outside
	// could become a local node resource, so let's be careful
	if !isControlledNetwork(ip) {
		return nil, fmt.Errorf("Fish: Prohibited to serve the Resource IP from not controlled network")
	}

	// Check by MAC and update IP if found
	// need to fix due to on mac arp can return just one digit
	hwAddr := fixHwAddr(arp.Search(ip))
	if hwAddr == "" {
		return nil, gorm.ErrRecordNotFound
	}
	err = f.db.Where("node_uid = ?", f.GetNodeUID()).Where("hw_addr = ?", hwAddr).First(res).Error
	if err != nil {
		return nil, fmt.Errorf("Fish: %s for HW address %s", err, hwAddr)
	}

	// Check if the state is allocated to prevent old resources access
	if f.ApplicationIsAllocated(res.ApplicationUID) != nil {
		return nil, fmt.Errorf("Fish: Prohibited to access the Resource of not allocated Application")
	}

	log.Debug("Fish: Update IP address for the Resource of Application", res.ApplicationUID, ip)
	res.IpAddr = ip
	err = f.ResourceSave(res)

	return res, err
}

// ResourceGetByApplication returns Resource by ApplicationUID
func (f *Fish) ResourceGetByApplication(appUID types.ApplicationUID) (res *types.Resource, err error) {
	res = &types.Resource{}
	err = f.db.Where("application_uid = ?", appUID).First(res).Error
	return res, err
}

// ResourceServiceMappingByApplicationAndDest is trying to find the ResourceServiceMapping record with Application and Location if possible.
// The application in priority, location - secondary priority, if no such records found - default will be used.
func (f *Fish) ResourceServiceMappingByApplicationAndDest(appUID types.ApplicationUID, dest string) string {
	sm := &types.ServiceMapping{}

	err := f.db.Where(
		"application_uid = ?", appUID).Where(
		"location_uid = ?", f.GetLocationName()).Where(
		"service = ?", dest).Order("application_uid DESC").Order("location_uid DESC").First(sm).Error
	if err != nil {
		return ""
	}

	return sm.Redirect
}
