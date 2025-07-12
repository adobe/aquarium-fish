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

func (d *Database) subscribeApplicationTaskImpl(_ context.Context, ch chan *typesv2.ApplicationTask) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	d.subsApplicationTask = append(d.subsApplicationTask, ch)
}

// unsubscribeApplicationTaskImpl removes a channel from the subscription list
func (d *Database) unsubscribeApplicationTaskImpl(_ context.Context, ch chan *typesv2.ApplicationTask) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	for i, existing := range d.subsApplicationTask {
		if existing == ch {
			// Remove channel from slice
			d.subsApplicationTask = append(d.subsApplicationTask[:i], d.subsApplicationTask[i+1:]...)
			break
		}
	}
}

// applicationTaskListImpl returns all known ApplicationTasks
func (d *Database) applicationTaskListImpl(_ context.Context) (at []typesv2.ApplicationTask, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationTask).List(&at)
	return at, err
}

// applicationTaskListByApplicationImpl allows to find all the ApplicationTasks by ApplicationUID
func (d *Database) applicationTaskListByApplicationImpl(ctx context.Context, appUID typesv2.ApplicationUID) (at []typesv2.ApplicationTask, err error) {
	all, err := d.ApplicationTaskList(ctx)
	if err == nil {
		for _, a := range all {
			if a.ApplicationUid == appUID {
				at = append(at, a)
			}
		}
	}
	return at, err
}

// applicationTaskCreateImpl makes a new ApplicationTask
func (d *Database) applicationTaskCreateImpl(_ context.Context, at *typesv2.ApplicationTask) error {
	if at.ApplicationUid == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if at.Task == "" {
		return fmt.Errorf("Fish: Task can't be empty")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	at.Uid = d.NewUID()
	at.CreatedAt = time.Now()
	at.UpdatedAt = at.CreatedAt

	err := d.be.Collection(ObjectApplicationTask).Add(at.Uid.String(), at)

	// Notifying the subscribers on change, doing that in goroutine to not block execution
	go func(appTask *typesv2.ApplicationTask) {
		d.subsMu.RLock()
		channels := make([]chan *typesv2.ApplicationTask, len(d.subsApplicationTask))
		copy(channels, d.subsApplicationTask)
		d.subsMu.RUnlock()

		for _, ch := range channels {
			// Use select with default to prevent panic if channel is closed
			select {
			case ch <- appTask:
				// Successfully sent notification
			default:
				// Channel is closed or full, skip this subscriber
				log.WithFunc("database", "applicationTaskCreateImpl").Debug("Failed to send ApplicationTask notification, channel closed or full")
			}
		}
	}(at)

	return err
}

// applicationTaskSaveImpl stores the ApplicationTask
func (d *Database) applicationTaskSaveImpl(_ context.Context, at *typesv2.ApplicationTask) error {
	if at.Uid == uuid.Nil {
		return fmt.Errorf("Fish: UID can't be unset")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationTask).Add(at.Uid.String(), at)
}

// applicationTaskGetImpl returns the ApplicationTask by ApplicationTaskUID
func (d *Database) applicationTaskGetImpl(_ context.Context, uid typesv2.ApplicationTaskUID) (at *typesv2.ApplicationTask, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationTask).Get(uid.String(), &at)
	return at, err
}

// applicationTaskDeleteImpl removes the ApplicationTask
func (d *Database) applicationTaskDeleteImpl(_ context.Context, uid typesv2.ApplicationTaskUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationTask).Delete(uid.String())
}

// applicationTaskListByApplicationAndWhenImpl returns list of ApplicationTasks by ApplicationUID and When it need to be executed
func (d *Database) applicationTaskListByApplicationAndWhenImpl(ctx context.Context, appUID typesv2.ApplicationUID, when typesv2.ApplicationState_Status) (at []typesv2.ApplicationTask, err error) {
	all, err := d.ApplicationTaskListByApplication(ctx, appUID)
	if err == nil {
		for _, a := range all {
			if a.When == when {
				at = append(at, a)
			}
		}
	}
	return at, err
}
