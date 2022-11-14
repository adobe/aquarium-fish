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
	"log"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (f *Fish) ApplicationTaskFindByApplication(uid types.ApplicationUID, filter *string) (at []types.ApplicationTask, err error) {
	db := f.db.Where("application_uid = ?", uid)
	if filter != nil {
		secured_filter, err := util.ExpressionSqlFilter(*filter)
		if err != nil {
			log.Println("Fish: SECURITY: weird SQL filter received", err)
			// We do not fail here because we should not give attacker more information
			return at, nil
		}
		// Adding parentheses to be sure we're have `application_uid AND (filter)`
		db = db.Where("(" + secured_filter + ")")
	}
	err = db.Find(&at).Error
	return at, err
}

func (f *Fish) ApplicationTaskCreate(at *types.ApplicationTask) error {
	if at.Task == "" {
		return fmt.Errorf("Fish: Task can't be empty")
	}
	if at.Options == "" {
		at.Options = util.UnparsedJson("{}")
	}
	if at.Result == "" {
		at.Result = util.UnparsedJson("{}")
	}

	at.UID = f.NewUID()
	return f.db.Create(at).Error
}

func (f *Fish) ApplicationTaskSave(at *types.ApplicationTask) error {
	return f.db.Save(at).Error
}

func (f *Fish) ApplicationTaskGet(uid types.ApplicationTaskUID) (at *types.ApplicationTask, err error) {
	at = &types.ApplicationTask{}
	err = f.db.First(at, uid).Error
	return at, err
}

func (f *Fish) ApplicationTaskListByApplicationAndWhen(app_uid types.ApplicationUID, when types.ApplicationStatus) (at []types.ApplicationTask, err error) {
	err = f.db.Where(`application_uid = ? AND "when" = ?`, app_uid, when).Order("created_at desc").Find(&at).Error
	return at, err
}
