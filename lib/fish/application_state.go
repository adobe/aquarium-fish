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

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ApplicationStateList() (ass []types.ApplicationState, err error) {
	err = f.db.Find(&ass).Error
	return ass, err
}

func (f *Fish) ApplicationStateCreate(as *types.ApplicationState) error {
	if as.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if as.Status == "" {
		return fmt.Errorf("Fish: Status can't be empty")
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
