/**
 * Copyright 2021-2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// ApplicationFind lists Applications by filter
func (d *Database) ApplicationList() (as []typesv2.Application, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplication).List(&as)
	return as, err
}

// ApplicationCreate makes new Application
func (d *Database) ApplicationCreate(a *typesv2.Application) error {
	if a.LabelUid == uuid.Nil {
		return fmt.Errorf("Fish: LabelUID can't be unset")
	}

	a.Uid = d.NewUID()
	a.CreatedAt = time.Now()

	d.beMu.RLock()
	err := d.be.Collection(ObjectApplication).Add(a.Uid.String(), a)
	d.beMu.RUnlock()

	if err != nil {
		return err
	}

	// Create ApplicationState NEW too
	return d.ApplicationStateCreate(&typesv2.ApplicationState{
		ApplicationUid: a.Uid, Status: typesv2.ApplicationState_NEW,
		Description: "Just created by Fish " + d.node.Name,
	})
}

// Intentionally disabled, application can't be updated
/*func (d *Database) ApplicationSave(app *typesv2.Application) error {
	return d.be.Save(app).Error
}*/

// ApplicationGet returns Application by UID
func (d *Database) ApplicationGet(uid typesv2.ApplicationUID) (a *typesv2.Application, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplication).Get(uid.String(), &a)
	return a, err
}

// ApplicationDelete removes the Application
func (d *Database) ApplicationDelete(uid typesv2.ApplicationUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplication).Delete(uid.String())
}

// ApplicationIsAllocated returns if specific Application is allocated
func (d *Database) ApplicationIsAllocated(appUID typesv2.ApplicationUID) (err error) {
	state, err := d.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return err
	} else if state.Status != typesv2.ApplicationState_ALLOCATED {
		return fmt.Errorf("Fish: The Application is not allocated")
	}
	return nil
}

// ApplicationDeallocate helps with creating deallocate/recalled state for the Application
func (d *Database) ApplicationDeallocate(appUID typesv2.ApplicationUID, requestor string) (*typesv2.ApplicationState, error) {
	out, err := d.ApplicationStateGetByApplication(appUID)
	if err != nil {
		return nil, fmt.Errorf("Unable to find status for the Application: %s, %w", appUID, err)
	}
	if !d.ApplicationStateIsActive(out.Status) {
		// Since app can't be deallocated - it's not really an error, treating as precaution
		log.Warnf("DB: Unable to deallocate the Application %q with status: %s", appUID, out.Status)
		return out, nil
	}

	newStatus := typesv2.ApplicationState_DEALLOCATE
	if out.Status == typesv2.ApplicationState_NEW {
		// The Application is still NEW so just mark it as DEALLOCATED
		newStatus = typesv2.ApplicationState_DEALLOCATED
	}
	as := &typesv2.ApplicationState{ApplicationUid: appUID, Status: newStatus,
		Description: fmt.Sprintf("Requested by %s", requestor),
	}

	if err = d.ApplicationStateCreate(as); err != nil {
		return nil, fmt.Errorf("Unable to deallocate the Application: %s, %w", appUID, err)
	}

	return as, nil
}
