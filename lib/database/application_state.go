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
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func (d *Database) subscribeApplicationStateImpl(ctx context.Context, ch chan *typesv2.ApplicationState) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	d.subsApplicationState = append(d.subsApplicationState, ch)
}

// unsubscribeApplicationStateImpl removes a channel from the subscription list
func (d *Database) unsubscribeApplicationStateImpl(ctx context.Context, ch chan *typesv2.ApplicationState) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	for i, existing := range d.subsApplicationState {
		if existing == ch {
			// Remove channel from slice
			d.subsApplicationState = append(d.subsApplicationState[:i], d.subsApplicationState[i+1:]...)
			break
		}
	}
}

// applicationStateListImpl returns list of ApplicationStates
func (d *Database) applicationStateListImpl(ctx context.Context) (ass []typesv2.ApplicationState, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationState).List(&ass)
	return ass, err
}

// applicationStateCreateImpl makes new ApplicationState
func (d *Database) applicationStateCreateImpl(ctx context.Context, as *typesv2.ApplicationState) error {
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
		d.subsMu.RLock()
		channels := make([]chan *typesv2.ApplicationState, len(d.subsApplicationState))
		copy(channels, d.subsApplicationState)
		d.subsMu.RUnlock()

		for _, ch := range channels {
			// Use select with default to prevent panic if channel is closed
			select {
			case ch <- appState:
				// Successfully sent notification
			default:
				// Channel is closed or full, skip this subscriber
				log.Debug().Msgf("Database: Failed to send ApplicationState notification, channel closed or full")
			}
		}
	}(as)

	return err
}

// Intentionally disabled, application state can't be updated
/*func (d *Database) applicationStateSaveImpl(ctx context.Context, as *typesv2.ApplicationState) error {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Save(as).Error
}*/

// applicationStateGetImpl returns specific ApplicationState
func (d *Database) applicationStateGetImpl(ctx context.Context, uid typesv2.ApplicationStateUID) (as *typesv2.ApplicationState, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationState).Get(uid.String(), &as)
	return as, err
}

// applicationStateDeleteImpl removes the ApplicationState
func (d *Database) applicationStateDeleteImpl(ctx context.Context, uid typesv2.ApplicationStateUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationState).Delete(uid.String())
}

// applicationStateListByApplicationImpl returns all ApplicationStates with ApplicationUID
func (d *Database) applicationStateListByApplicationImpl(ctx context.Context, appUID typesv2.ApplicationUID) (states []typesv2.ApplicationState, err error) {
	all, err := d.ApplicationStateList(ctx)
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

// applicationStateNewCountImpl returns amount of NEW states of the Application, to get amount of tries
func (d *Database) applicationStateNewCountImpl(ctx context.Context, appUID typesv2.ApplicationUID) (count uint) {
	all, err := d.ApplicationStateList(ctx)
	if err != nil {
		log.Error().Msgf("Unable to get ApplicationState list: %v", err)
		return count
	}
	for _, as := range all {
		if as.ApplicationUid == appUID && as.Status == typesv2.ApplicationState_NEW {
			count++
		}
	}
	return count
}

// applicationStateListLatestImpl returns list of latest ApplicationState per Application
func (d *Database) applicationStateListLatestImpl(ctx context.Context) (out []typesv2.ApplicationState, err error) {
	states := make(map[string]typesv2.ApplicationState)
	all, err := d.ApplicationStateList(ctx)
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

// applicationStateListNewElectedImpl returns new and elected Applications
func (d *Database) applicationStateListNewElectedImpl(ctx context.Context) (ass []typesv2.ApplicationState, err error) {
	states, err := d.ApplicationStateListLatest(ctx)
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

// applicationStateGetByApplicationImpl returns latest ApplicationState of requested ApplicationUID
func (d *Database) applicationStateGetByApplicationImpl(ctx context.Context, appUID typesv2.ApplicationUID) (state *typesv2.ApplicationState, err error) {
	all, err := d.ApplicationStateListByApplication(ctx, appUID)
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
