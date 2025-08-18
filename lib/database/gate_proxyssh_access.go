/**
 * Copyright 2024-2025 Adobe. All rights reserved.
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

// TODO: Move that to Raw and Gate rails, remove from Fish core since it's a part of ProxySSH gate

package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// GateProxySSHAccessList returns a list of all known GateProxySSHAccess objects
func (d *Database) GateProxySSHAccessList() (ra []typesv2.GateProxySSHAccess, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection("gate_proxyssh_access").List(&ra)
	return ra, err
}

// GateProxySSHAccessCreate makes new ResourceAccess
func (d *Database) GateProxySSHAccessCreate(ra *typesv2.GateProxySSHAccess) error {
	if ra.ApplicationResourceUid == uuid.Nil {
		return fmt.Errorf("application resource UID can't be nil")
	}
	if ra.Username == "" {
		return fmt.Errorf("username can't be empty")
	}
	if ra.Password == "" {
		return fmt.Errorf("password can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	ra.Uid = d.NewUID()
	ra.CreatedAt = time.Now()
	return d.be.Collection("gate_proxyssh_access").Add(ra.Uid.String(), ra)
}

// GateProxySSHAccessDelete removes ResourceAccess by UID
func (d *Database) GateProxySSHAccessDelete(uid typesv2.GateProxySSHAccessUID) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection("gate_proxyssh_access").Delete(uid.String())
}

// GateProxySSHAccessDeleteByResource removes ResourceAccess by ResourceUID
func (d *Database) GateProxySSHAccessDeleteByResource(appresUID typesv2.ApplicationResourceUID) error {
	all, err := d.GateProxySSHAccessList()
	if err != nil {
		return fmt.Errorf("unable to find any GateProxySSHAccess object to delete")
	}
	for _, a := range all {
		if a.ApplicationResourceUid == appresUID {
			return d.GateProxySSHAccessDelete(a.Uid)
		}
	}
	return fmt.Errorf("unable to find GateProxySSHAccess with ApplicationResourceUID: %s", appresUID.String())
}

// GateProxySSHAccessSingleUsePasswordHash retrieves the password hash from the database *AND* deletes
// it. Users must request a new Resource Access to connect again.
func (d *Database) GateProxySSHAccessSingleUsePasswordHash(username string, hash string) (*typesv2.GateProxySSHAccess, error) {
	all, err := d.GateProxySSHAccessList()
	if err != nil {
		return nil, fmt.Errorf("no available GateProxySSHAccess objects")
	}
	for _, ra := range all {
		if ra.Username == username && ra.Password == hash {
			if err = d.GateProxySSHAccessDelete(ra.Uid); err != nil {
				// NOTE: in rare occasions, `err` here could end up propagating to the
				// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
				// in the event that our deletion failed (but nothing else), the single
				// use connection ultimately gets rejected.
				log.WithFunc("proxyssh", "GateProxySSHAccessSingleUsePasswordHash").Error("Unable to remove GateProxySSHAccess", "access_uid", ra.Uid, "err", err)
			}
			return &ra, d.GateProxySSHAccessDelete(ra.Uid)
		}
	}
	return nil, fmt.Errorf("no GateProxySSHAccess found")
}

// GateProxySSHAccessSingleUseKey retrieves the key from the database *AND* deletes it.
// Users must request a new resource access to connect again.
func (d *Database) GateProxySSHAccessSingleUseKey(username string, key string) (*typesv2.GateProxySSHAccess, error) {
	all, err := d.GateProxySSHAccessList()
	if err != nil {
		return nil, fmt.Errorf("no available GateProxySSHAccess objects")
	}
	for _, ra := range all {
		if ra.Username == username && ra.Key == key {
			if err = d.GateProxySSHAccessDelete(ra.Uid); err != nil {
				// NOTE: in rare occasions, `err` here could end up propagating to the
				// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
				// in the event that our deletion failed (but nothing else), the single
				// use connection ultimately gets rejected.
				log.WithFunc("proxyssh", "GateProxySSHAccessSingleUseKey").Error("Unable to remove GateProxySSHAccess", "access_uid", ra.Uid, "err", err)
			}
			return &ra, d.GateProxySSHAccessDelete(ra.Uid)
		}
	}
	return nil, fmt.Errorf("no GateProxySSHAccess found")
}
