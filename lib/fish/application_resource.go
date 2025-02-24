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
	"time"

	"github.com/google/uuid"
	"github.com/mostlygeek/arp"
	"go.mills.io/bitcask/v2"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ApplicationResourceList returns a list of all known ApplicationResource objects
func (f *Fish) ApplicationResourceList() (rs []types.ApplicationResource, err error) {
	err = f.db.Collection("application_resource").List(&rs)
	return rs, err
}

// ApplicationResourceListNode returns list of resources for provided NodeUID
func (f *Fish) ApplicationResourceListNode(nodeUID types.NodeUID) (rs []types.ApplicationResource, err error) {
	all, err := f.ApplicationResourceList()
	if err == nil {
		for _, r := range all {
			if r.NodeUID == nodeUID {
				rs = append(rs, r)
			}
		}
	}
	return rs, err
}

// ApplicationResourceCreate makes new Resource
func (f *Fish) ApplicationResourceCreate(r *types.ApplicationResource) error {
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
	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	return f.db.Collection("application_resource").Add(r.UID.String(), r)
}

// ApplicationResourceDelete removes Resource
func (f *Fish) ApplicationResourceDelete(uid types.ApplicationResourceUID) error {
	// First delete any references to this resource.
	err := f.ApplicationResourceAccessDeleteByResource(uid)
	if err != nil {
		log.Warnf("Unable to delete ApplicationResourceAccess associated with ApplicationResourceUID %s: %v", uid, err)
	}
	// Now purge the resource.
	return f.db.Collection("application_resource").Delete(uid.String())
}

// ApplicationResourceSave stores ApplicationResource
func (f *Fish) ApplicationResourceSave(res *types.ApplicationResource) error {
	res.UpdatedAt = time.Now()
	return f.db.Collection("application_resource").Add(res.UID.String(), res)
}

// ApplicationResourceGet returns Resource by it's UID
func (f *Fish) ApplicationResourceGet(uid types.ApplicationResourceUID) (res *types.ApplicationResource, err error) {
	err = f.db.Collection("application_resource").Get(uid.String(), &res)
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

// ApplicationResourceGetByIP returns Resource by it's IP address
func (f *Fish) ApplicationResourceGetByIP(ip string) (res *types.ApplicationResource, err error) {
	// Check by IP first
	all, err := f.ApplicationResourceList()
	if err != nil {
		return nil, fmt.Errorf("Fish: Unable to get any ApplicationResource")
	}
	for _, r := range all {
		if r.NodeUID == f.GetNodeUID() && r.IpAddr == ip {
			res = &r
			break
		}
	}
	if res != nil {
		// Check if the state is allocated to prevent old resources access
		if f.ApplicationIsAllocated(res.ApplicationUID) != nil {
			return nil, fmt.Errorf("Fish: Prohibited to access the ApplicationResource of not allocated Application")
		}

		return res, nil
	}

	// Make sure the IP is the controlled network, otherwise someone from outside
	// could become a local node resource, so let's be careful
	if !isControlledNetwork(ip) {
		return nil, fmt.Errorf("Fish: Prohibited to serve the ApplicationResource IP from not controlled network")
	}

	// Check by MAC and update IP if found
	// need to fix due to on mac arp can return just one digit
	hwAddr := fixHwAddr(arp.Search(ip))
	if hwAddr == "" {
		return nil, bitcask.ErrKeyNotFound
	}
	for _, r := range all {
		if r.NodeUID == f.GetNodeUID() && r.HwAddr == hwAddr {
			res = &r
			break
		}
	}
	if res == nil {
		return nil, fmt.Errorf("Fish: No ApplicationResource with HW address %s", hwAddr)
	}

	// Check if the state is allocated to prevent old resources access
	if f.ApplicationIsAllocated(res.ApplicationUID) != nil {
		return nil, fmt.Errorf("Fish: Prohibited to access the ApplicationResource of not allocated Application")
	}

	log.Debug("Fish: Update IP address for the ApplicationResource", res.ApplicationUID, ip)
	res.IpAddr = ip
	err = f.ApplicationResourceSave(res)

	return res, err
}

// ApplicationResourceGetByApplication returns ApplicationResource by ApplicationUID
func (f *Fish) ApplicationResourceGetByApplication(appUID types.ApplicationUID) (res *types.ApplicationResource, err error) {
	all, err := f.ApplicationResourceList()
	if err == nil {
		for _, r := range all {
			if r.ApplicationUID == appUID {
				return &r, nil
			}
		}
	}
	return res, fmt.Errorf("Fish: Unable to find ApplicationResource with requested Application UID: %s", appUID.String())
}
