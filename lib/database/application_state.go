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

func (d *Database) SubscribeApplicationState(ch chan *typesv2.ApplicationState) {
	d.subsApplicationState = append(d.subsApplicationState, ch)
}

// ApplicationStateList returns list of ApplicationStates
func (d *Database) ApplicationStateList() (ass []typesv2.ApplicationState, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationState).List(&ass)
	return ass, err
}

// ApplicationStateCreate makes new ApplicationState
func (d *Database) ApplicationStateCreate(as *typesv2.ApplicationState) error {
	if as.ApplicationUid == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if as.Status == typesv2.ApplicationState_UNSPECIFIED {
		return fmt.Errorf("Fish: Status can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	as.Uid = d.NewUID()
	as.CreatedAt = time.Now()
	err := d.be.Collection(ObjectApplicationState).Add(as.Uid.String(), as)

	// Notifying the subscribers on change, doing that in goroutine to not block execution
	go func(appState *typesv2.ApplicationState) {
		for _, ch := range d.subsApplicationState {
			ch <- appState
		}
	}(as)

	return err
}

// Intentionally disabled, application state can't be updated
/*func (d *Database) ApplicationStateSave(as *typesv2.ApplicationState) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Save(as).Error
}*/

// ApplicationStateGet returns specific ApplicationState
func (d *Database) ApplicationStateGet(uid typesv2.ApplicationStateUID) (as *typesv2.ApplicationState, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationState).Get(uid.String(), &as)
	return as, err
}

// ApplicationStateDelete removes the ApplicationState
func (d *Database) ApplicationStateDelete(uid typesv2.ApplicationStateUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationState).Delete(uid.String())
}

// ApplicationStateListByApplication returns all ApplicationStates with ApplicationUID
func (d *Database) ApplicationStateListByApplication(appUID typesv2.ApplicationUID) (states []typesv2.ApplicationState, err error) {
	all, err := d.ApplicationStateList()
	if err != nil {
		return states, err
	}
	for _, as := range all {
		if as.ApplicationUid == appUID {
			states = append(states, as)
		}
	}
	return states, err
}

// ApplicationStateNewCount returns amount of NEW states of the Application, to get amount of tries
func (d *Database) ApplicationStateNewCount(appUID typesv2.ApplicationUID) (count uint) {
	all, err := d.ApplicationStateList()
	if err != nil {
		log.Errorf("Unable to get ApplicationState list: %v", err)
		return count
	}
	for _, as := range all {
		if as.ApplicationUid == appUID && as.Status == typesv2.ApplicationState_NEW {
			count++
		}
	}
	return count
}

// ApplicationStateListLatest returns list of latest ApplicationState per Application
func (d *Database) ApplicationStateListLatest() (out []typesv2.ApplicationState, err error) {
	states := make(map[string]typesv2.ApplicationState)
	all, err := d.ApplicationStateList()
	if err != nil {
		return out, err
	}
	for _, as := range all {
		if stat, ok := states[as.ApplicationUid.String()]; !ok || stat.CreatedAt.Before(as.CreatedAt) {
			states[as.ApplicationUid.String()] = as
		}
	}
	for _, as := range states {
		out = append(out, as)
	}
	return out, nil
}

// ApplicationStateListNewElected returns new and elected Applications
func (d *Database) ApplicationStateListNewElected() (ass []typesv2.ApplicationState, err error) {
	states, err := d.ApplicationStateListLatest()
	if err != nil {
		return ass, err
	}
	for _, stat := range states {
		if stat.Status == typesv2.ApplicationState_NEW || stat.Status == typesv2.ApplicationState_ELECTED {
			ass = append(ass, stat)
		}
	}
	return ass, err
}

// ApplicationStateGetByApplication returns latest ApplicationState of requested ApplicationUID
func (d *Database) ApplicationStateGetByApplication(appUID typesv2.ApplicationUID) (state *typesv2.ApplicationState, err error) {
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

// ApplicationStateIsActive returns false if Status in ERROR, DEALLOCATE or DEALLOCATED state
func (d *Database) ApplicationStateIsActive(status typesv2.ApplicationState_Status) bool {
	if status == typesv2.ApplicationState_DEALLOCATE {
		return false
	}
	return !d.ApplicationStateIsDead(status)
}

// ApplicationStateIsDead returns false if Status in ERROR or DEALLOCATED state
func (*Database) ApplicationStateIsDead(status typesv2.ApplicationState_Status) bool {
	if status == typesv2.ApplicationState_ERROR {
		return true
	}
	if status == typesv2.ApplicationState_DEALLOCATED {
		return true
	}
	return false
}
