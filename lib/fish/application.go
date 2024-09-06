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

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (f *Fish) ApplicationFind(filter *string) (as []types.Application, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSqlFilter(*filter)
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

func (f *Fish) ApplicationCreate(a *types.Application) error {
	if a.LabelUID == uuid.Nil {
		return fmt.Errorf("Fish: LabelUID can't be unset")
	}
	if a.Metadata == "" {
		a.Metadata = "{}"
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

func (f *Fish) ApplicationGet(uid types.ApplicationUID) (a *types.Application, err error) {
	a = &types.Application{}
	err = f.db.First(a, uid).Error
	return a, err
}

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

func (f *Fish) ApplicationIsAllocated(appUid types.ApplicationUID) (err error) {
	state, err := f.ApplicationStateGetByApplication(appUid)
	if err != nil {
		return err
	} else if state.Status != types.ApplicationStatusALLOCATED {
		return fmt.Errorf("Fish: The Application is not allocated")
	}
	return nil
}
