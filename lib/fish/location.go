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
	"errors"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) LocationFind(filter *string) (ls []types.Location, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&ls).Error
	return ls, err
}

func (f *Fish) LocationCreate(l *types.Location) error {
	if l.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}

	return f.db.Create(l).Error
}

func (f *Fish) LocationSave(l *types.Location) error {
	return f.db.Save(l).Error
}

func (f *Fish) LocationGet(id int64) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.First(l, id).Error
	return l, err
}

func (f *Fish) LocationGetByName(name string) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.Where("name = ?", name).First(l).Error
	return l, err
}

func (f *Fish) LocationDelete(id int64) error {
	return f.db.Delete(&types.Location{}, id).Error
}
