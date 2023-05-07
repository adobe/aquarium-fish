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

// LabelFind returns list of Labels that fits filter
func (f *Fish) LabelFind(filter *string) (labels []types.Label, err error) {
	db := f.db
	if filter != nil {
		securedFilter, err := util.ExpressionSQLFilter(*filter)
		if err != nil {
			log.Warn("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return labels, nil
		}
		db = db.Where(securedFilter)
	}
	err = db.Find(&labels).Error
	return labels, err
}

// LabelCreate makes new Label
func (f *Fish) LabelCreate(l *types.Label) error {
	if err := l.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Label: %v", err)
	}

	l.UID = f.NewUID()
	return f.db.Create(l).Error
}

// Intentionally disabled - labels can be created once and can't be updated
// Create label with incremented version instead
/*func (f *Fish) LabelSave(label *types.Label) error {
	return f.db.Save(label).Error
}*/

// LabelGet returns Label by UID
func (f *Fish) LabelGet(uid types.LabelUID) (label *types.Label, err error) {
	label = &types.Label{}
	err = f.db.First(label, uid).Error
	return label, err
}

// LabelDelete deletes the Label by UID
func (f *Fish) LabelDelete(uid types.LabelUID) error {
	return f.db.Delete(&types.Label{}, uid).Error
}

// Insert / update the label directly from the data, without changing created_at and updated_at
func (f *Fish) LabelImport(l *types.Label) error {
	if err := l.Validate(); err != nil {
		return fmt.Errorf("Fish: Unable to validate Label: %v", err)
	}

	// The updated_at and created_at should stay the same so skipping the hooks
	tx := f.db.Session(&gorm.Session{SkipHooks: true})
	err := tx.Create(l).Error
	if err != nil {
		err = tx.Save(l).Error
	}

	return err
}
