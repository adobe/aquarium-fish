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

package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ApplicationFind lists Applications by filter
func (d *Database) ApplicationList() (as []types.Application, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection("application").List(&as)
	return as, err
}

// ApplicationCreate makes new Application
func (d *Database) ApplicationCreate(a *types.Application) error {
	if a.LabelUID == uuid.Nil {
		return fmt.Errorf("Fish: LabelUID can't be unset")
	}
	if a.Metadata == "" {
		a.Metadata = "{}"
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	a.UID = d.NewUID()
	a.CreatedAt = time.Now()
	err := d.be.Collection("application").Add(a.UID.String(), a)

	// Create ApplicationState NEW too
	d.ApplicationStateCreate(&types.ApplicationState{
		ApplicationUID: a.UID, Status: types.ApplicationStatusNEW,
		Description: "Just created by Fish " + d.node.Name,
	})
	return err
}

// Intentionally disabled, application can't be updated
/*func (d *Database) ApplicationSave(app *types.Application) error {
	return d.be.Save(app).Error
}*/

// ApplicationGet returns Application by UID
func (d *Database) ApplicationGet(uid types.ApplicationUID) (a *types.Application, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection("application").Get(uid.String(), &a)
	return a, err
}

// ApplicationDelete removes the Application
func (d *Database) ApplicationDelete(uid types.ApplicationUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection("application").Delete(uid.String())
}

// ApplicationIsAllocated returns if specific Application is allocated
func (d *Database) ApplicationIsAllocated(appUID types.ApplicationUID) (err error) {
	state, err := d.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return err
	} else if state.Status != types.ApplicationStatusALLOCATED {
		return fmt.Errorf("Fish: The Application is not allocated")
	}
	return nil
}

// ApplicationDeallocate helps with creating deallocate/recalled state for the Application
func (d *Database) ApplicationDeallocate(appUID types.ApplicationUID, requestor string) (*types.ApplicationState, error) {
	out, err := d.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return nil, fmt.Errorf("Unable to find status for the Application: %s, %w", appUID, err)
	}
	if !d.ApplicationStateIsActive(out.Status) {
		// Since app can't be deallocated - it's not really an error, treating as precaution
		log.Warnf("DB: Unable to deallocate the Application %q with status: %s", appUID, out.Status)
		return out, nil
	}

	newStatus := types.ApplicationStatusDEALLOCATE
	if out.Status != types.ApplicationStatusALLOCATED {
		// The Application was not yet Allocated so just mark it as Recalled
		newStatus = types.ApplicationStatusRECALLED
	}
	as := &types.ApplicationState{ApplicationUID: appUID, Status: newStatus,
		Description: fmt.Sprintf("Requested by %s", requestor),
	}

	if err = d.ApplicationStateCreate(as); err != nil {
		return nil, fmt.Errorf("Unable to deallocate the Application: %s, %w", appUID, err)
	}

	return as, nil
}
