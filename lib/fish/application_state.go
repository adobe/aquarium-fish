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
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// ApplicationStateList returns list of ApplicationStates
func (f *Fish) ApplicationStateList() (ass []types.ApplicationState, err error) {
	err = f.db.Collection("application_state").List(&ass)
	return ass, err
}

// ApplicationStateCreate makes new ApplicationState
func (f *Fish) ApplicationStateCreate(as *types.ApplicationState) error {
	if as.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if as.Status == "" {
		return fmt.Errorf("Fish: Status can't be empty")
	}

	as.UID = f.NewUID()
	as.CreatedAt = time.Now()
	return f.db.Collection("application_state").Add(as.UID.String(), as)
}

// Intentionally disabled, application state can't be updated
/*func (f *Fish) ApplicationStateSave(as *types.ApplicationState) error {
	return f.db.Save(as).Error
}*/

// ApplicationStateGet returns specific ApplicationState
func (f *Fish) ApplicationStateGet(uid types.ApplicationStateUID) (as *types.ApplicationState, err error) {
	err = f.db.Collection("application_state").Get(uid.String(), &as)
	return as, err
}

// ApplicationStatesGetByApplication returns all ApplicationStates with ApplicationUID
func (f *Fish) ApplicationStatesGetByApplication(appUID types.ApplicationUID) (states []types.ApplicationState, err error) {
	if all, err := f.ApplicationStateList(); err == nil {
		for _, as := range all {
			if as.ApplicationUID == appUID {
				states = append(states, as)
			}
		}
	} else {
		return states, err
	}
	return states, err
}

// ApplicationStatesGetLatest returns latest ApplicationState per Application
func (f *Fish) ApplicationStatesGetLatest() (out []types.ApplicationState, err error) {
	states := make(map[types.ApplicationUID]*types.ApplicationState)
	if all, err := f.ApplicationStateList(); err == nil {
		for _, as := range all {
			if stat, ok := states[as.ApplicationUID]; !ok || stat.CreatedAt.Before(as.CreatedAt) {
				states[as.ApplicationUID] = &as
			}
		}
	} else {
		return out, err
	}
	for _, as := range states {
		out = append(out, *as)
	}
	return out, nil
}

// ApplicationStateGetByApplication returns latest ApplicationState of requested ApplicationUID
func (f *Fish) ApplicationStateGetByApplication(appUID types.ApplicationUID) (state *types.ApplicationState, err error) {
	if all, err := f.ApplicationStatesGetByApplication(appUID); err == nil {
		for _, as := range all {
			if state == nil || state.CreatedAt.Before(as.CreatedAt) {
				state = &as
			}
		}
	} else {
		return nil, err
	}
	return state, err
}

// ApplicationStateIsActive returns false if Status in ERROR, DEALLOCATE or DEALLOCATED state
func (*Fish) ApplicationStateIsActive(status types.ApplicationStatus) bool {
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
