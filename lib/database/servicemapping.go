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

// TODO: Move to Raw and Gate rails, remove from Fish core since it's a part of ProxySocks gate

package database

import (
	"fmt"
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ServiceMappingFind returns list of ServiceMappings that fits the filter
func (d *Database) ServiceMappingList() (sms []types.ServiceMapping, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection("service_mapping").List(&sms)
	return sms, err
}

// ServiceMappingCreate makes new ServiceMapping
func (d *Database) ServiceMappingCreate(sm *types.ServiceMapping) error {
	if sm.Service == "" {
		return fmt.Errorf("Fish: Service can't be empty")
	}
	if sm.Redirect == "" {
		return fmt.Errorf("Fish: Redirect can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	sm.UID = d.NewUID()
	sm.CreatedAt = time.Now()
	return d.be.Collection("service_mapping").Add(sm.UID.String(), sm)
}

// ServiceMappingSave stores ServiceMapping
func (d *Database) ServiceMappingSave(sm *types.ServiceMapping) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection("service_mapping").Add(sm.UID.String(), sm)
}

// ServiceMappingGet returns ServiceMapping by UID
func (d *Database) ServiceMappingGet(uid types.ServiceMappingUID) (sm *types.ServiceMapping, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection("service_mapping").Get(uid.String(), &sm)
	return sm, err
}

// ServiceMappingDelete removes ServiceMapping
func (d *Database) ServiceMappingDelete(uid types.ServiceMappingUID) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection("service_mapping").Delete(uid.String())
}

// ServiceMappingByApplication returns ServiceMapping list with specified ApplicationUID
func (d *Database) ServiceMappingListByApplication(appUID types.ApplicationUID) (sms []types.ServiceMapping, err error) {
	if all, err := d.ServiceMappingList(); err == nil {
		for _, sm := range all {
			if sm.ApplicationUID == appUID {
				sms = append(sms, sm)
			}
		}
	}
	return sms, err
}

// ServiceMappingByApplicationAndDest is trying to find the ServiceMapping record with Application and Location
// The application in priority, location - secondary priority, if no such records found - default will be used.
func (d *Database) ServiceMappingByApplicationAndDest(appUID types.ApplicationUID, dest string) string {
	if all, err := d.ServiceMappingList(); err == nil {
		for _, sm := range all {
			if sm.ApplicationUID == appUID && sm.Location == d.GetNodeLocation() && sm.Service == dest {
				return sm.Redirect
			}
		}
	}
	return ""
}
