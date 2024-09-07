/**
 * Copyright 2024 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Package types stores generated types and their special functions
package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// GormDataType describes how to store Authentication in database
func (Authentication) GormDataType() string {
	return "blob"
}

// Scan converts the Authentication to json bytes
func (auth *Authentication) Scan(value any) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("Failed to unmarshal JSONB value: %s", value)
	}

	err := json.Unmarshal(bytes, auth)
	return err
}

// Value converts json bytes to Authentication
func (auth Authentication) Value() (driver.Value, error) {
	return json.Marshal(auth)
}
