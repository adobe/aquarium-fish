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

	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// ServiceMappingFind returns list of ServiceMappings that fits the filter
func (f *Fish) ServiceMappingFind(filter *string) (sms []types.ServiceMapping, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return sms, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&sms).Error
	return sms, err
}

// ServiceMappingCreate makes new ServiceMapping
func (f *Fish) ServiceMappingCreate(sm *types.ServiceMapping) error {
	if err := sm.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate ServiceMapping: %v", err)
	}

	sm.UID = f.NewUID()
	return f.db.Create(sm).Error
}

// ServiceMappingSave stores ServiceMapping
func (f *Fish) ServiceMappingSave(sm *types.ServiceMapping) error {
	return f.db.Save(sm).Error
}

// ServiceMappingGet returns ServiceMapping by UID
func (f *Fish) ServiceMappingGet(uid types.ServiceMappingUID) (sm *types.ServiceMapping, err error) {
	sm = &types.ServiceMapping{}
	err = f.db.First(sm, uid).Error
	return sm, err
}

// ServiceMappingDelete removes ServiceMapping
func (f *Fish) ServiceMappingDelete(uid types.ServiceMappingUID) error {
	return f.db.Delete(&types.ServiceMapping{}, uid).Error
}

// Insert / update the service mapping directly from the data, without changing created_at and updated_at
func (f *Fish) ServiceMappingImport(sm *types.ServiceMapping) error {
	if err := sm.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate ServiceMapping: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(sm).Error
	if err != nil {
		err = tx.Save(sm).Error
	}

	return err
}
