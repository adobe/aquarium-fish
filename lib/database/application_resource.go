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

package database

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mostlygeek/arp"
	"go.mills.io/bitcask/v2"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// applicationResourceListImpl returns a list of all known ApplicationResource objects
func (d *Database) applicationResourceListImpl(_ context.Context) (rs []typesv2.ApplicationResource, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationResource).List(&rs)
	return rs, err
}

// applicationResourceListNodeImpl returns list of resources for provided NodeUID
func (d *Database) applicationResourceListNodeImpl(ctx context.Context, nodeUID typesv2.NodeUID) (rs []typesv2.ApplicationResource, err error) {
	all, err := d.ApplicationResourceList(ctx)
	if err == nil {
		for _, r := range all {
			if r.NodeUid == nodeUID {
				rs = append(rs, r)
			}
		}
	}
	return rs, err
}

// applicationResourceCreateImpl makes new Resource
func (d *Database) applicationResourceCreateImpl(_ context.Context, r *typesv2.ApplicationResource) error {
	if r.ApplicationUid == uuid.Nil {
		return fmt.Errorf("application resource application UID can't be unset")
	}
	if r.LabelUid == uuid.Nil {
		return fmt.Errorf("application resource label UID can't be unset")
	}
	if r.NodeUid == uuid.Nil {
		return fmt.Errorf("application resource node UID can't be unset")
	}
	if len(r.Identifier) == 0 {
		return fmt.Errorf("application resource identifier can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	r.Uid = d.NewUID()
	r.CreatedAt = time.Now()
	r.UpdatedAt = r.CreatedAt
	err := d.be.Collection(ObjectApplicationResource).Add(r.Uid.String(), r)

	// Notify subscribers about the new ApplicationResource
	notifySubscribersHelper(d, &d.subsApplicationResource, NewCreateEvent(r), ObjectApplicationResource)

	return err
}

// applicationResourceDeleteImpl removes Resource
func (d *Database) applicationResourceDeleteImpl(ctx context.Context, uid typesv2.ApplicationResourceUID) error {
	// Get the object before deleting it for notification
	res, getErr := d.ApplicationResourceGet(ctx, uid)
	if getErr != nil {
		return getErr
	}

	// First delete any references to this resource. We don't care about the error if it's happened.
	_ = d.GateProxySSHAccessDeleteByResource(uid)

	d.beMu.RLock()
	err := d.be.Collection(ObjectApplicationResource).Delete(uid.String())
	d.beMu.RUnlock()

	if err == nil && res != nil {
		// Notify subscribers about the removed ApplicationResource
		notifySubscribersHelper(d, &d.subsApplicationResource, NewRemoveEvent(res), ObjectApplicationResource)
	}

	return err
}

// applicationResourceSaveImpl stores ApplicationResource
func (d *Database) applicationResourceSaveImpl(_ context.Context, res *typesv2.ApplicationResource) error {
	res.UpdatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectApplicationResource).Add(res.Uid.String(), res)
	d.beMu.RUnlock()

	if err == nil {
		// Notify subscribers about the updated ApplicationResource
		notifySubscribersHelper(d, &d.subsApplicationResource, NewUpdateEvent(res), ObjectApplicationResource)
	}

	return err
}

// applicationResourceGetImpl returns Resource by it's UID
func (d *Database) applicationResourceGetImpl(_ context.Context, uid typesv2.ApplicationResourceUID) (res *typesv2.ApplicationResource, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationResource).Get(uid.String(), &res)
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
		log.WithFunc("database", "isControlledNetwork").Error("Unable to get the available network interfaces", "err", err.Error())
		return false
	}

	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			log.WithFunc("database", "isControlledNetwork").Error("Unable to get available addresses of the interface", "net_if", i.Name, "err", err.Error())
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

// applicationResourceGetByIPImpl returns Resource by it's IP address
func (d *Database) applicationResourceGetByIPImpl(ctx context.Context, ip string) (res *typesv2.ApplicationResource, err error) {
	// Check by IP first
	all, err := d.ApplicationResourceList(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get any ApplicationResource")
	}
	for _, r := range all {
		if r.NodeUid == d.GetNodeUID() && r.IpAddr == ip {
			res = &r
			break
		}
	}
	if res != nil {
		// Check if the state is allocated to prevent old resources access
		if d.ApplicationIsAllocated(ctx, res.ApplicationUid) != nil {
			return nil, fmt.Errorf("prohibited to access the ApplicationResource of not allocated Application")
		}

		return res, nil
	}

	// Make sure the IP is the controlled network, otherwise someone from outside
	// could become a local node resource, so let's be careful
	if !isControlledNetwork(ip) {
		return nil, fmt.Errorf("prohibited to serve the ApplicationResource IP from not controlled network")
	}

	// Check by MAC and update IP if found
	// need to fix due to on mac arp can return just one digit
	hwAddr := fixHwAddr(arp.Search(ip))
	if hwAddr == "" {
		return nil, bitcask.ErrKeyNotFound
	}
	for _, r := range all {
		if r.NodeUid == d.GetNodeUID() && r.HwAddr == hwAddr {
			res = &r
			break
		}
	}
	if res == nil {
		return nil, fmt.Errorf("no ApplicationResource with HW address %s", hwAddr)
	}

	// Check if the state is allocated to prevent old resources access
	if d.ApplicationIsAllocated(ctx, res.ApplicationUid) != nil {
		return nil, fmt.Errorf("prohibited to access the ApplicationResource of not allocated Application")
	}

	log.WithFunc("database", "applicationResourceGetByIPImpl").Debug("Update IP address for the ApplicationResource", "app_uid", res.ApplicationUid, "ip", ip)
	res.IpAddr = ip
	err = d.ApplicationResourceSave(ctx, res)

	return res, err
}

// applicationResourceGetByApplicationImpl returns ApplicationResource by ApplicationUID
func (d *Database) applicationResourceGetByApplicationImpl(ctx context.Context, appUID typesv2.ApplicationUID) (res *typesv2.ApplicationResource, err error) {
	all, err := d.ApplicationResourceList(ctx)
	if err == nil {
		for _, r := range all {
			if r.ApplicationUid == appUID {
				return &r, nil
			}
		}
	}
	return res, fmt.Errorf("unable to find ApplicationResource with requested Application UID: %s", appUID.String())
}

// subscribeApplicationResourceImpl adds a channel to the subscription list
func (d *Database) subscribeApplicationResourceImpl(_ context.Context, ch chan ApplicationResourceSubscriptionEvent) {
	subscribeHelper(d, &d.subsApplicationResource, ch)
}

// unsubscribeApplicationResourceImpl removes a channel from the subscription list
func (d *Database) unsubscribeApplicationResourceImpl(_ context.Context, ch chan ApplicationResourceSubscriptionEvent) {
	unsubscribeHelper(d, &d.subsApplicationResource, ch)
}
