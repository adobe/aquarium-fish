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
package db

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ApplicationFind lists Applications by filter
func (d *Database) ApplicationList() (as []types.Application, err error) {
	err = d.be.Collection("application").List(&as)
	return as, err
}

// ApplicationCreate makes new Applciation
func (d *Database) ApplicationCreate(a *types.Application) error {
	if a.LabelUID == uuid.Nil {
		return fmt.Errorf("Fish: LabelUID can't be unset")
	}
	if a.Metadata == "" {
		a.Metadata = "{}"
	}

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
	err = d.be.Collection("application").Get(uid.String(), &a)
	return a, err
}

// ApplicationDelete removes the Application
func (d *Database) ApplicationDelete(uid types.ApplicationUID) (err error) {
	return d.be.Collection("application").Delete(uid.String())
}

// ApplicationListGetStatusNew returns new Applications
func (d *Database) ApplicationListGetStatusNew() (as []types.Application, err error) {
	states, err := d.ApplicationStateListLatest()
	if err != nil {
		return as, err
	}
	for _, stat := range states {
		if stat.Status == types.ApplicationStatusNEW {
			if app, err := d.ApplicationGet(stat.ApplicationUID); err == nil && app != nil {
				as = append(as, *app)
			}
		}
	}
	return as, err
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
