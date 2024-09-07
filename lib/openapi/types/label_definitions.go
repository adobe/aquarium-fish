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

package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// GormDataType describes how to store LabelDefinitions in database
func (LabelDefinitions) GormDataType() string {
	return "blob"
}

// Scan converts the LabelDefinitions to json bytes
func (ld *LabelDefinitions) Scan(value any) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("Failed to unmarshal JSONB value: %s", value)
	}

	err := json.Unmarshal(bytes, ld)
	// Need to make sure the array node filter will not be nil
	for i, r := range *ld {
		if r.Resources.NodeFilter == nil {
			(*ld)[i].Resources.NodeFilter = []string{}
		}
	}
	return err
}

// Value converts json bytes to LabelDefinitions
func (ld LabelDefinitions) Value() (driver.Value, error) {
	// Need to make sure the array node filter will not be nil
	for i, r := range ld {
		if r.Resources.NodeFilter == nil {
			ld[i].Resources.NodeFilter = []string{}
		}
	}
	return json.Marshal(ld)
}
