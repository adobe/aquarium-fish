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

	"gorm.io/gorm"

	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

// LocationFind returns list of Locations fits filter
func (f *Fish) LocationFind(filter *string) (ls []types.Location, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return ls, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&ls).Error
	return ls, err
}

// LocationCreate makes new Location
func (f *Fish) LocationCreate(l *types.Location) error {
	if err := l.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Location: %v", err)
	}

	return f.db.Create(l).Error
}

// LocationSave stores the Location
func (f *Fish) LocationSave(l *types.Location) error {
	return f.db.Save(l).Error
}

// LocationGet returns Location by it's unique name
func (f *Fish) LocationGet(name types.LocationName) (l *types.Location, err error) {
	l = &types.Location{}
	err = f.db.First(l, name).Error
	return l, err
}

// LocationDelete removes location
func (f *Fish) LocationDelete(name types.LocationName) error {
	return f.db.Delete(&types.Location{}, name).Error
}

// Insert / update the location directly from the data, without changing created_at and updated_at
func (f *Fish) LocationImport(l *types.Location) error {
	if err := l.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Location: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(l).Error
	if err != nil {
		err = tx.Save(l).Error
	}

	return err
}
