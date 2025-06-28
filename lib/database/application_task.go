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

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func (d *Database) SubscribeApplicationTask(ch chan *typesv2.ApplicationTask) {
	d.subsMu.Lock()
	defer d.subsMu.Unlock()
	d.subsApplicationTask = append(d.subsApplicationTask, ch)
}

// UnsubscribeApplicationTask removes a channel from the subscription list
func (d *Database) UnsubscribeApplicationTask(ch chan *typesv2.ApplicationTask) {
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

// ApplicationTaskList returns all known ApplicationTasks
func (d *Database) ApplicationTaskList() (at []typesv2.ApplicationTask, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationTask).List(&at)
	return at, err
}

// ApplicationTaskFindByApplication allows to find all the ApplicationTasks by ApplicationUID
func (d *Database) ApplicationTaskListByApplication(appUID typesv2.ApplicationUID) (at []typesv2.ApplicationTask, err error) {
	all, err := d.ApplicationTaskList()
	if err == nil {
		for _, a := range all {
			if a.ApplicationUid == appUID {
				at = append(at, a)
			}
		}
	}
	return at, err
}

// ApplicationTaskCreate makes a new ApplicationTask
func (d *Database) ApplicationTaskCreate(at *typesv2.ApplicationTask) error {
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
			ch <- appTask
		}
	}(at)

	return err
}

// ApplicationTaskSave stores the ApplicationTask
func (d *Database) ApplicationTaskSave(at *typesv2.ApplicationTask) error {
	if at.Uid == uuid.Nil {
		return fmt.Errorf("Fish: UID can't be unset")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationTask).Add(at.Uid.String(), at)
}

// ApplicationTaskGet returns the ApplicationTask by ApplicationTaskUID
func (d *Database) ApplicationTaskGet(uid typesv2.ApplicationTaskUID) (at *typesv2.ApplicationTask, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(ObjectApplicationTask).Get(uid.String(), &at)
	return at, err
}

// ApplicationTaskDelete removes the ApplicationTask
func (d *Database) ApplicationTaskDelete(uid typesv2.ApplicationTaskUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(ObjectApplicationTask).Delete(uid.String())
}

// ApplicationTaskListByApplicationAndWhen returns list of ApplicationTasks by ApplicationUID and When it need to be executed
func (d *Database) ApplicationTaskListByApplicationAndWhen(appUID typesv2.ApplicationUID, when typesv2.ApplicationState_Status) (at []typesv2.ApplicationTask, err error) {
	all, err := d.ApplicationTaskListByApplication(appUID)
	if err == nil {
		for _, a := range all {
			if a.When == when {
				at = append(at, a)
			}
		}
	}
	return at, err
}
