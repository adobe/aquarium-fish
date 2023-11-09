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

func (f *Fish) ApplicationStateFind(filter *string) (as []types.ApplicationState, err error) {
	db := f.db
	if filter != nil {
		secured_filter, err := util.ExpressionSqlFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return as, nil
		}
		db = db.Where(secured_filter)
	}
	err = db.Find(&as).Error
	return as, err
}

func (f *Fish) ApplicationStateList() (ass []types.ApplicationState, err error) {
	err = f.db.Find(&ass).Error
	return ass, err
}

func (f *Fish) ApplicationStateCreate(as *types.ApplicationState) error {
	if err := as.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate ApplicationState: %v", err)
	}

	as.UID = f.NewUID()
	return f.db.Create(as).Error
}

// Intentionally disabled, application state can't be updated
/*func (f *Fish) ApplicationStateSave(as *types.ApplicationState) error {
	return f.db.Save(as).Error
}*/

func (f *Fish) ApplicationStateGet(uid types.ApplicationStateUID) (as *types.ApplicationState, err error) {
	as = &types.ApplicationState{}
	err = f.db.First(as, uid).Error
	return as, err
}

func (f *Fish) ApplicationStateGetByApplication(app_uid types.ApplicationUID) (as *types.ApplicationState, err error) {
	as = &types.ApplicationState{}
	err = f.db.Where("application_uid = ?", app_uid).Order("created_at desc").First(as).Error
	return as, err
}

// Return false if Status in ERROR, DEALLOCATE or DEALLOCATED state
func (f *Fish) ApplicationStateIsActive(status types.ApplicationStatus) bool {
	if status == types.ApplicationStatusERROR {
		return false
	}
	if status == types.ApplicationStatusDEALLOCATE {
		return false
	}
	if status == types.ApplicationStatusRECALLED {
		return false
	}
	if status == types.ApplicationStatusDEALLOCATED {
		return false
	}
	return true
}

// Insert / update the application state directly from the data, without changing created_at and updated_at
func (f *Fish) ApplicationStateImport(as *types.ApplicationState) error {
	if err := as.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate ApplicationState: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(as).Error
	if err != nil {
		err = tx.Save(as).Error
	}

	return err
}
