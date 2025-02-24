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
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ApplicationFind lists Applications by filter
func (f *Fish) ApplicationList() (as []types.Application, err error) {
	err = f.db.Collection("application").List(&as)
	return as, err
}

// ApplicationCreate makes new Applciation
func (f *Fish) ApplicationCreate(a *types.Application) error {
	if a.LabelUID == uuid.Nil {
		return fmt.Errorf("Fish: LabelUID can't be unset")
	}
	if a.Metadata == "" {
		a.Metadata = "{}"
	}

	a.UID = f.NewUID()
	a.CreatedAt = time.Now()
	err := f.db.Collection("application").Add(a.UID.String(), a)

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
	err = f.db.Collection("application").Get(uid.String(), &a)
	return a, err
}

// ApplicationListGetStatusNew returns new Applications
func (f *Fish) ApplicationListGetStatusNew() (as []types.Application, err error) {
	if states, err := f.ApplicationStatesGetLatest(); err == nil {
		for _, stat := range states {
			if stat.Status == types.ApplicationStatusNEW {
				if app, err := f.ApplicationGet(stat.ApplicationUID); err == nil && app != nil {
					as = append(as, *app)
				}
			}
		}
	} else {
		return as, err
	}
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
