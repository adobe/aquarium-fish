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
	"github.com/adobe/aquarium-fish/lib/util"
)

// ApplicationTaskList returns all known ApplicationTasks
func (f *Fish) ApplicationTaskList() (at []types.ApplicationTask, err error) {
	err = f.db.Collection("application_task").List(&at)
	return at, err
}

// ApplicationTaskFindByApplication allows to find all the ApplicationTasks by ApplciationUID
func (f *Fish) ApplicationTaskListByApplication(uid types.ApplicationUID) (at []types.ApplicationTask, err error) {
	all, err := f.ApplicationTaskList()
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
func (f *Fish) ApplicationTaskCreate(at *types.ApplicationTask) error {
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

	at.UID = f.NewUID()
	at.CreatedAt = time.Now()
	at.UpdatedAt = at.CreatedAt
	return f.db.Collection("application_task").Add(at.UID.String(), at)
}

// ApplicationTaskSave stores the ApplicationTask
func (f *Fish) ApplicationTaskSave(at *types.ApplicationTask) error {
	if at.UID == uuid.Nil {
		return fmt.Errorf("Fish: UID can't be unset")
	}
	return f.db.Collection("application_task").Add(at.UID.String(), at)
}

// ApplicationTaskGet returns the ApplicationTask by ApplicationTaskUID
func (f *Fish) ApplicationTaskGet(uid types.ApplicationTaskUID) (at *types.ApplicationTask, err error) {
	err = f.db.Collection("application_task").Get(uid.String(), &at)
	return at, err
}

// ApplicationTaskDelete removes the ApplicationTask
func (f *Fish) ApplicationTaskDelete(uid types.ApplicationTaskUID) (err error) {
	return f.db.Collection("application_task").Delete(uid.String())
}

// ApplicationTaskListByApplicationAndWhen returns list of ApplicationTasks by ApplicationUID and When it need to be executed
func (f *Fish) ApplicationTaskListByApplicationAndWhen(appUID types.ApplicationUID, when types.ApplicationStatus) (at []types.ApplicationTask, err error) {
	all, err := f.ApplicationTaskListByApplication(appUID)
	if err == nil {
		for _, a := range all {
			if a.When == when {
				at = append(at, a)
			}
		}
	}
	return at, err
}
