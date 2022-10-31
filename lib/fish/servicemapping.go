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

	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

func (f *Fish) ServiceMappingFind(filter *string) (sms []types.ServiceMapping, err error) {
	db := f.db
	if filter != nil {
		db = db.Where(*filter)
	}
	err = db.Find(&sms).Error
	return sms, err
}

func (f *Fish) ServiceMappingCreate(sm *types.ServiceMapping) error {
	if sm.Service == "" {
		return fmt.Errorf("Fish: Service can't be empty")
	}
	if sm.Redirect == "" {
		return fmt.Errorf("Fish: Redirect can't be empty")
	}

	sm.UID = f.NewUID()
	return f.db.Create(sm).Error
}

func (f *Fish) ServiceMappingSave(sm *types.ServiceMapping) error {
	return f.db.Save(sm).Error
}

func (f *Fish) ServiceMappingGet(uid types.ServiceMappingUID) (sm *types.ServiceMapping, err error) {
	sm = &types.ServiceMapping{}
	err = f.db.First(sm, uid).Error
	return sm, err
}

func (f *Fish) ServiceMappingDelete(uid types.ServiceMappingUID) error {
	return f.db.Delete(&types.ServiceMapping{}, uid).Error
}
