/**
 * Copyright 2024 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ApplicationResourceAccessList returns a list of all known ApplicationResourceAccess objects
func (d *Database) ApplicationResourceAccessList() (ra []types.ApplicationResourceAccess, err error) {
	err = d.be.Collection("application_resource_access").List(&ra)
	return ra, err
}

// ApplicationResourceAccessCreate makes new ResourceAccess
func (d *Database) ApplicationResourceAccessCreate(ra *types.ApplicationResourceAccess) error {
	if ra.ApplicationResourceUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationResourceUID can't be nil")
	}
	if ra.Username == "" {
		return fmt.Errorf("Fish: Username can't be empty")
	}
	if ra.Password == "" {
		return fmt.Errorf("Fish: Password can't be empty")
	}

	ra.UID = d.NewUID()
	ra.CreatedAt = time.Now()
	return d.be.Collection("application_resource_access").Add(ra.UID.String(), ra)
}

// ApplicationResourceAccessDelete removes ResourceAccess by UID
func (d *Database) ApplicationResourceAccessDelete(uid types.ApplicationResourceAccessUID) error {
	return d.be.Collection("application_resource_access").Delete(uid.String())
}

// ApplicationResourceAccessDeleteByResource removes ResourceAccess by ResourceUID
func (d *Database) ApplicationResourceAccessDeleteByResource(appresUID types.ApplicationResourceUID) error {
	all, err := d.ApplicationResourceAccessList()
	if err != nil {
		return fmt.Errorf("Fish: Unable to find any ApplicationResourceAccess object to delete")
	}
	for _, a := range all {
		if a.ApplicationResourceUID == appresUID {
			return d.ApplicationResourceAccessDelete(a.UID)
		}
	}
	return fmt.Errorf("Fish: Unable to find ApplicationResourceAccess with ApplicationResourceUID: %s", appresUID.String())
}

// ApplicationResourceAccessSingleUsePasswordHash retrieves the password hash from the database *AND* deletes
// it. Users must request a new Resource Access to connect again.
func (d *Database) ApplicationResourceAccessSingleUsePasswordHash(username string, hash string) (*types.ApplicationResourceAccess, error) {
	all, err := d.ApplicationResourceAccessList()
	if err != nil {
		return nil, fmt.Errorf("Fish: No available ApplicationResourceAccess objects")
	}
	for _, ra := range all {
		if ra.Username == username && ra.Password == hash {
			if err = d.ApplicationResourceAccessDelete(ra.UID); err != nil {
				// NOTE: in rare occasions, `err` here could end up propagating to the
				// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
				// in the event that our deletion failed (but nothing else), the single
				// use connection ultimately gets rejected.
				log.Errorf("Fish: Unable to remove ApplicationResourceAccess %s: %v", ra.UID.String(), err)
			}
			return &ra, d.ApplicationResourceAccessDelete(ra.UID)
		}
	}
	return nil, fmt.Errorf("Fish: No ApplicationResourceAccess found")
}

// ApplicationResourceAccessSingleUseKey retrieves the key from the database *AND* deletes it.
// Users must request a new resource access to connect again.
func (d *Database) ApplicationResourceAccessSingleUseKey(username string, key string) (*types.ApplicationResourceAccess, error) {
	all, err := d.ApplicationResourceAccessList()
	if err != nil {
		return nil, fmt.Errorf("Fish: No available ApplicationResourceAccess objects")
	}
	for _, ra := range all {
		if ra.Username == username && ra.Key == key {
			if err = d.ApplicationResourceAccessDelete(ra.UID); err != nil {
				// NOTE: in rare occasions, `err` here could end up propagating to the
				// caller with a valid `ra`.  However, see ssh_proxy/proxy.go usage,
				// in the event that our deletion failed (but nothing else), the single
				// use connection ultimately gets rejected.
				log.Errorf("Fish: Unable to remove ApplicationResourceAccess %s: %v", ra.UID.String(), err)
			}
			return &ra, d.ApplicationResourceAccessDelete(ra.UID)
		}
	}
	return nil, fmt.Errorf("Fish: No ApplicationResourceAccess found")
}
