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
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ServiceMappingFind returns list of ServiceMappings that fits the filter
func (f *Fish) ServiceMappingList() (sms []types.ServiceMapping, err error) {
	err = f.db.Collection("application_task").List(&sms)
	return sms, err
}

// ServiceMappingCreate makes new ServiceMapping
func (f *Fish) ServiceMappingCreate(sm *types.ServiceMapping) error {
	if sm.Service == "" {
		return fmt.Errorf("Fish: Service can't be empty")
	}
	if sm.Redirect == "" {
		return fmt.Errorf("Fish: Redirect can't be empty")
	}

	sm.UID = f.NewUID()
	sm.CreatedAt = time.Now()
	return f.db.Collection("service_mapping").Add(sm.UID.String(), sm)
}

// ServiceMappingSave stores ServiceMapping
func (f *Fish) ServiceMappingSave(sm *types.ServiceMapping) error {
	return f.db.Collection("service_mapping").Add(sm.UID.String(), sm)
}

// ServiceMappingGet returns ServiceMapping by UID
func (f *Fish) ServiceMappingGet(uid types.ServiceMappingUID) (sm *types.ServiceMapping, err error) {
	err = f.db.Collection("service_mapping").Get(uid.String(), &sm)
	return sm, err
}

// ServiceMappingDelete removes ServiceMapping
func (f *Fish) ServiceMappingDelete(uid types.ServiceMappingUID) error {
	return f.db.Collection("service_mapping").Delete(uid.String())
}

// ServiceMappingByApplication returns ServiceMapping list with specified ApplicationUID
func (f *Fish) ServiceMappingListByApplication(appUID types.ApplicationUID) (sms []types.ServiceMapping, err error) {
	if all, err := f.ServiceMappingList(); err == nil {
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
func (f *Fish) ServiceMappingByApplicationAndDest(appUID types.ApplicationUID, dest string) string {
	if all, err := f.ServiceMappingList(); err == nil {
		for _, sm := range all {
			if sm.ApplicationUID == appUID && sm.Location == f.GetLocation() && sm.Service == dest {
				return sm.Redirect
			}
		}
	}
	return ""
}
