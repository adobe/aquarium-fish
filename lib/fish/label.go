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
	"time"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/util"
)

func (f *Fish) LabelFind(filter *string) (labels []types.Label, err error) {
	db := f.db
	if filter != nil {
		secured_filter, err := util.ExpressionSqlFilter(*filter)
		if err != nil {
			log.Println("Fish: SECURITY: weird SQL filter received:", err)
			// We do not fail here because we should not give attacker more information
			return labels, nil
		}
		db = db.Where(secured_filter)
	}
	err = db.Find(&labels).Error
	return labels, err
}

func (f *Fish) LabelCreate(l *types.Label) error {
	if l.Name == "" {
		return fmt.Errorf("Fish: Name can't be empty")
	}
	for i, def := range l.Definitions {
		if def.Driver == "" {
			return fmt.Errorf("Fish: Driver can't be empty in Label Definition %d", i)
		}
		if def.Resources.Cpu < 1 {
			return fmt.Errorf("Fish: Resources CPU can't be less than 1 in Label Definition %d", i)
		}
		if def.Resources.Ram < 1 {
			return fmt.Errorf("Fish: Resources RAM can't be less than 1 in Label Definition %d", i)
		}
		_, err := time.ParseDuration(def.Resources.Lifetime)
		if def.Resources.Lifetime != "" && err != nil {
			return fmt.Errorf("Fish: Resources Lifetime parse error in Label Definition %d: %v", i, err)
		}
		if def.Options == "" {
			l.Definitions[i].Options = "{}"
		}
	}
	if l.Metadata == "" {
		l.Metadata = "{}"
	}

	l.UID = f.NewUID()
	return f.db.Create(l).Error
}

// Intentionally disabled - labels can be created once and can't be updated
// Create label with incremented version instead
/*func (f *Fish) LabelSave(label *types.Label) error {
	return f.db.Save(label).Error
}*/

func (f *Fish) LabelGet(uid types.LabelUID) (label *types.Label, err error) {
	label = &types.Label{}
	err = f.db.First(label, uid).Error
	return label, err
}

func (f *Fish) LabelDelete(uid types.LabelUID) error {
	return f.db.Delete(&types.Label{}, uid).Error
}
