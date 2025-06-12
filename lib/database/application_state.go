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
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (d *Database) SubscribeApplicationState(ch chan *types.ApplicationState) {
	d.subsApplicationState = append(d.subsApplicationState, ch)
}

// ApplicationStateList returns list of ApplicationStates
func (d *Database) ApplicationStateList() (ass []types.ApplicationState, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationState).List(&ass)
	return ass, err
}

// ApplicationStateCreate makes new ApplicationState
func (d *Database) ApplicationStateCreate(as *types.ApplicationState) error {
	if as.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if as.Status == "" {
		return fmt.Errorf("Fish: Status can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	as.UID = d.NewUID()
	as.CreatedAt = time.Now()
	err := d.be.Collection(ObjectApplicationState).Add(as.UID.String(), as)

	// Notifying the subscribers on change, doing that in goroutine to not block execution
	go func(appState *types.ApplicationState) {
		for _, ch := range d.subsApplicationState {
			ch <- appState
		}
	}(as)

	return err
}

// Intentionally disabled, application state can't be updated
/*func (d *Database) ApplicationStateSave(as *types.ApplicationState) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Save(as).Error
}*/

// ApplicationStateGet returns specific ApplicationState
func (d *Database) ApplicationStateGet(uid types.ApplicationStateUID) (as *types.ApplicationState, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationState).Get(uid.String(), &as)
	return as, err
}

// ApplicationStateDelete removes the ApplicationState
func (d *Database) ApplicationStateDelete(uid types.ApplicationStateUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationState).Delete(uid.String())
}

// ApplicationStateListByApplication returns all ApplicationStates with ApplicationUID
func (d *Database) ApplicationStateListByApplication(appUID types.ApplicationUID) (states []types.ApplicationState, err error) {
	all, err := d.ApplicationStateList()
	if err != nil {
		return states, err
	}
	for _, as := range all {
		if as.ApplicationUID == appUID {
			states = append(states, as)
		}
	}
	return states, err
}

// ApplicationStateNewCount returns amount of NEW states of the Application, to get amount of tries
func (d *Database) ApplicationStateNewCount(appUID types.ApplicationUID) (count uint) {
	all, err := d.ApplicationStateList()
	if err != nil {
		log.Errorf("Unable to get ApplicationState list: %v", err)
		return count
	}
	for _, as := range all {
		if as.ApplicationUID == appUID && as.Status == types.ApplicationStatusNEW {
			count++
		}
	}
	return count
}

// ApplicationStateListLatest returns list of latest ApplicationState per Application
func (d *Database) ApplicationStateListLatest() (out []types.ApplicationState, err error) {
	states := make(map[types.ApplicationUID]types.ApplicationState)
	all, err := d.ApplicationStateList()
	if err != nil {
		return out, err
	}
	for _, as := range all {
		if stat, ok := states[as.ApplicationUID]; !ok || stat.CreatedAt.Before(as.CreatedAt) {
			states[as.ApplicationUID] = as
		}
	}
	for _, as := range states {
		out = append(out, as)
	}
	return out, nil
}

// ApplicationStateListNewElected returns new and elected Applications
func (d *Database) ApplicationStateListNewElected() (ass []types.ApplicationState, err error) {
	states, err := d.ApplicationStateListLatest()
	if err != nil {
		return ass, err
	}
	for _, stat := range states {
		if stat.Status == types.ApplicationStatusNEW || stat.Status == types.ApplicationStatusELECTED {
			ass = append(ass, stat)
		}
	}
	return ass, err
}

// ApplicationStateGetByApplication returns latest ApplicationState of requested ApplicationUID
func (d *Database) ApplicationStateGetByApplication(appUID types.ApplicationUID) (state *types.ApplicationState, err error) {
	all, err := d.ApplicationStateListByApplication(appUID)
	if err != nil {
		return nil, err
	}
	for _, as := range all {
		if state == nil || state.CreatedAt.Before(as.CreatedAt) {
			state = &as
		}
	}
	if state == nil {
		err = fmt.Errorf("Fish: Unable to find any state with ApplicationUID %s", appUID)
	}
	return state, err
}

// ApplicationStateIsActive returns false if Status in ERROR, RECALLED, DEALLOCATE or DEALLOCATED state
func (d *Database) ApplicationStateIsActive(status types.ApplicationStatus) bool {
	if status == types.ApplicationStatusDEALLOCATE {
		return false
	}
	return !d.ApplicationStateIsDead(status)
}

// ApplicationStateIsDead returns false if Status in ERROR, RECALLED or DEALLOCATED state
func (*Database) ApplicationStateIsDead(status types.ApplicationStatus) bool {
	if status == types.ApplicationStatusERROR {
		return true
	}
	if status == types.ApplicationStatusRECALLED {
		return true
	}
	if status == types.ApplicationStatusDEALLOCATED {
		return true
	}
	return false
}
