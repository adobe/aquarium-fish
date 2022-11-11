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

func (f *Fish) LocationFind(filter *string) (ls []types.Location, err error) {
	db := f.db
	if filter != nil {
		secured_filter, err := util.ExpressionSqlFilter(*filter)
		if err != nil {
			log.Println("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return ls, nil
		}
		db = db.Where(secured_filter)
	}
	err = db.Find(&ls).Error
	return ls, err
}

func (f *Fish) LocationCreate(l *types.Location) error {
	if l.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}

	return f.db.Create(l).Error
}

func (f *Fish) LocationSave(l *types.Location) error {
	return f.db.Save(l).Error
}

func (f *Fish) LocationGet(name types.LocationName) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.First(l, name).Error
	return l, err
}

func (f *Fish) LocationGetByName(name string) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.Where("name = ?", name).First(l).Error
	return l, err
}

func (f *Fish) LocationDelete(name types.LocationName) error {
	return f.db.Delete(&types.Location{}, name).Error
}
