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

func (f *Fish) LabelFind(filter *string) (labels []types.Label, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&labels).Error
	return labels, err
}

func (f *Fish) LabelCreate(l *types.Label) error {
	if l.Name == "" {
		return errors.New("Fish: Name can't be empty")
	}
	if l.Driver == "" {
		return errors.New("Fish: Driver can't be empty")
	}
	if l.Definition == "" {
		return errors.New("Fish: Definition can't be empty")
	}
	if l.Metadata == "" {
		l.Metadata = "{}"
	}

	return f.db.Create(l).Error
}

// Intentionally disabled - labels can be created once and can't be updated
// Create label with incremented version instead
/*func (f *Fish) LabelSave(label *types.Label) error {
	return f.db.Save(label).Error
}*/

func (f *Fish) LabelGet(id int64) (label *types.Label, err error) {
	label = &types.Label{}
	err = f.db.First(label, id).Error
	return label, err
}

func (f *Fish) LabelDelete(id int64) error {
	return f.db.Delete(&types.Label{}, id).Error
}
