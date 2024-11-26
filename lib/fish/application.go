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

// Package fish core defines all the internals of the Fish processes
package fish

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// ApplicationFind lists Applications by filter
func (f *Fish) ApplicationFind(filter *string) (as []types.Application, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return as, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&as).Error
	return as, err
}

// ApplicationCreate makes new Applciation
func (f *Fish) ApplicationCreate(a *types.Application) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Application: %v", err)
	}

	a.UID = f.NewUID()
	err := f.db.Create(a).Error

	// Create ApplicationState NEW too
	f.ApplicationStateCreate(&types.ApplicationState{
		ApplicationUID: a.UID, Status: types.ApplicationStatusNEW,
		Description: "Just created by Fish " + f.node.Name,
	})
	return err
}

// Intentionally disabled, application can't be updated
/*func (f *Fish) ApplicationSave(app *types.Application) error {
	return f.db.Save(app).Error
}*/

// ApplicationGet returns Application by UID
func (f *Fish) ApplicationGet(uid types.ApplicationUID) (a *types.Application, err error) {
	a = &types.Application{}
	err = f.db.First(a, uid).Error
	return a, err
}

// ApplicationListGetStatusNew returns new Applications
func (f *Fish) ApplicationListGetStatusNew() (as []types.Application, err error) {
	// SELECT * FROM applications WHERE UID in (
	//    SELECT application_uid FROM (
	//        SELECT application_uid, status, max(created_at) FROM application_states GROUP BY application_uid
	//    ) WHERE status = "NEW"
	// ) ORDER BY created_at
	err = f.db.Order("created_at").Where("UID in (?)",
		f.db.Select("application_uid").Table("(?)",
			f.db.Model(&types.ApplicationState{}).Select("application_uid, status, max(created_at)").Group("application_uid"),
		).Where("Status = ?", types.ApplicationStatusNEW),
	).Find(&as).Error
	return as, err
}

// ApplicationIsAllocated returns if specific Application is allocated
func (f *Fish) ApplicationIsAllocated(appUID types.ApplicationUID) (err error) {
	state, err := f.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return err
	} else if state.Status != types.ApplicationStatusALLOCATED {
		return fmt.Errorf("Fish: The Application is not allocated")
	}
	return nil
}

// Insert / update the application directly from the data, without changing created_at and updated_at
func (f *Fish) ApplicationImport(a *types.Application) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Application: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(a).Error
	if err != nil {
		err = tx.Save(a).Error
	}

	return err
}
