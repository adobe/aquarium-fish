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

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ApplicationFind(filter *string) (as []types.Application, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&as).Error
	return as, err
}

func (f *Fish) ApplicationCreate(a *types.Application) error {
	if a.Metadata == "" {
		a.Metadata = "{}"
	}

	a.UID = f.NewUID()
	err := f.db.Create(a).Error

	// Create ApplicationState NEW too
	f.ApplicationStateCreate(&types.ApplicationState{
		ApplicationUID: a.UID, Status: types.ApplicationStateStatusNEW,
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
	// )
	err = f.db.Where("UID in (?)",
		f.db.Select("application_uid").Table("(?)",
			f.db.Model(&types.ApplicationState{}).Select("application_uid, status, max(created_at)").Group("application_uid"),
		).Where("Status = ?", types.ApplicationStateStatusNEW),
	).Find(&as).Error
	return as, err
}

func (f *Fish) ApplicationIsAllocated(app_uid types.ApplicationUID) (err error) {
	state, err := f.ApplicationStateGetByApplication(app_uid)
	if err != nil {
		return err
	} else if state.Status != types.ApplicationStateStatusALLOCATED {
		return fmt.Errorf("Fish: The Application is not allocated")
	}
	return nil
}

func (f *Fish) ApplicationSnapshot(app *types.Application, full bool) (string, error) {
	// Get application label to choose the right driver
	label, err := f.LabelGet(app.LabelUID)
	if err != nil {
		return "", fmt.Errorf("Fish: Label not found: %w", err)
	}

	driver := f.DriverGet(label.Driver)
	if driver == nil {
		return "", fmt.Errorf("Fish: Driver not available: %s", label.Driver)
	}

	// Get resource to locate hwaddr
	res, err := f.ResourceGetByApplication(app.UID)
	if err != nil {
		return "", fmt.Errorf("Fish: Resource not found: %w", err)
	}

	out, err := driver.Snapshot(res.HwAddr, full)
	if err != nil {
		return "", fmt.Errorf("Fish: Unable to create snapshot: %w", err)
	}

	return out, nil
}
