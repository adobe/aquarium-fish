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

package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (d *Database) SubscribeApplicationTask(ch chan *types.ApplicationTask) {
	d.subsApplicationTask = append(d.subsApplicationTask, ch)
}

// ApplicationTaskList returns all known ApplicationTasks
func (d *Database) ApplicationTaskList() (at []types.ApplicationTask, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(types.ObjectApplicationTask).List(&at)
	return at, err
}

// ApplicationTaskFindByApplication allows to find all the ApplicationTasks by ApplicationUID
func (d *Database) ApplicationTaskListByApplication(uid types.ApplicationUID) (at []types.ApplicationTask, err error) {
	all, err := d.ApplicationTaskList()
	if err == nil {
		for _, a := range all {
			if a.ApplicationUID == uid {
				at = append(at, a)
			}
		}
	}
	return at, err
}

// ApplicationTaskCreate makes a new ApplicationTask
func (d *Database) ApplicationTaskCreate(at *types.ApplicationTask) error {
	if at.ApplicationUID == uuid.Nil {
		return fmt.Errorf("Fish: ApplicationUID can't be unset")
	}
	if at.Task == "" {
		return fmt.Errorf("Fish: Task can't be empty")
	}
	if at.Options == "" {
		at.Options = util.UnparsedJSON("{}")
	}
	if at.Result == "" {
		at.Result = util.UnparsedJSON("{}")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	at.UID = d.NewUID()
	at.CreatedAt = time.Now()
	at.UpdatedAt = at.CreatedAt

	err := d.be.Collection(types.ObjectApplicationTask).Add(at.UID.String(), at)

	// Notifying the subscribers on change, doing that in goroutine to not block execution
	go func(appTask *types.ApplicationTask) {
		for _, ch := range d.subsApplicationTask {
			ch <- appTask
		}
	}(at)

	return err
}

// ApplicationTaskSave stores the ApplicationTask
func (d *Database) ApplicationTaskSave(at *types.ApplicationTask) error {
	if at.UID == uuid.Nil {
		return fmt.Errorf("Fish: UID can't be unset")
	}

	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(types.ObjectApplicationTask).Add(at.UID.String(), at)
}

// ApplicationTaskGet returns the ApplicationTask by ApplicationTaskUID
func (d *Database) ApplicationTaskGet(uid types.ApplicationTaskUID) (at *types.ApplicationTask, err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	err = d.be.Collection(types.ObjectApplicationTask).Get(uid.String(), &at)
	return at, err
}

// ApplicationTaskDelete removes the ApplicationTask
func (d *Database) ApplicationTaskDelete(uid types.ApplicationTaskUID) (err error) {
	d.beMu.RLock()
	defer d.beMu.RUnlock()

	return d.be.Collection(types.ObjectApplicationTask).Delete(uid.String())
}

// ApplicationTaskListByApplicationAndWhen returns list of ApplicationTasks by ApplicationUID and When it need to be executed
func (d *Database) ApplicationTaskListByApplicationAndWhen(appUID types.ApplicationUID, when types.ApplicationStatus) (at []types.ApplicationTask, err error) {
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
