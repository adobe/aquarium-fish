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

// ApplicationTaskFind looks for ApplicationTask with filter
func (f *Fish) ApplicationTaskFind(filter *string) (at []types.ApplicationTask, err error) {
	db := f.db
	if filter != nil {
		secured_filter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return at, nil
		}
		db = db.Where(secured_filter)
	}
	err = db.Find(&at).Error
	return at, err
}

// ApplicationTaskFindByApplication allows to find all the ApplicationTasks by ApplciationUID
func (f *Fish) ApplicationTaskFindByApplication(uid types.ApplicationUID, filter *string) (at []types.ApplicationTask, err error) {
	db := f.db.Where("application_uid = ?", uid)
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return at, nil
		}
		// Adding parentheses to be sure we're have `application_uid AND (filter)`
		db = db.Where("(" + securedFilter + ")")
	}
	err = db.Find(&at).Error
	return at, err
}

// ApplicationTaskCreate makes a new ApplicationTask
func (f *Fish) ApplicationTaskCreate(at *types.ApplicationTask) error {
	if err := at.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate ApplicationTask: %v", err)
	}

	at.UID = f.NewUID()
	return f.db.Create(at).Error
}

// ApplicationTaskSave stores the ApplicationTask
func (f *Fish) ApplicationTaskSave(at *types.ApplicationTask) error {
	return f.db.Save(at).Error
}

// ApplicationTaskGet returns the ApplicationTask by ApplicationTaskUID
func (f *Fish) ApplicationTaskGet(uid types.ApplicationTaskUID) (at *types.ApplicationTask, err error) {
	at = &types.ApplicationTask{}
	err = f.db.First(at, uid).Error
	return at, err
}

// ApplicationTaskListByApplicationAndWhen returns list of ApplicationTasks by ApplicationUID and When it need to be executed
func (f *Fish) ApplicationTaskListByApplicationAndWhen(appUID types.ApplicationUID, when types.ApplicationStatus) (at []types.ApplicationTask, err error) {
	err = f.db.Where(`application_uid = ? AND "when" = ?`, appUID, when).Order("created_at desc").Find(&at).Error
	return at, err
}

// Insert / update the application task directly from the data, without changing created_at and updated_at
func (f *Fish) ApplicationTaskImport(at *types.ApplicationTask) error {
	if err := at.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate ApplicationTask: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(at).Error
	if err != nil {
		err = tx.Save(at).Error
	}

	return err
}
