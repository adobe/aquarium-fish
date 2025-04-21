/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package database

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.mills.io/bitcask/v2"
)

// Raw operations for drivers that has no special functions to operate
// They are protecting regular Fish keys by restricting using of "/" in key string

// HasValue checks if the key exists
func (d *Database) HasValue(prefix, key string) (bool, error) {
	if strings.Contains("/", key) {
		return false, fmt.Errorf("DB: HasValue can't use '/' in key: %s", key)
	}
	return d.be.Has(bitcask.Key(fmt.Sprintf("%s:%s", prefix, key))), nil
}

// GetValue returns data by key
func (d *Database) GetValue(prefix, key string, obj any) error {
	if strings.Contains("/", key) {
		return fmt.Errorf("DB: GetValue can't use '/' in key: %s", key)
	}
	data, err := d.be.Get(bitcask.Key(fmt.Sprintf("%s:%s", prefix, key)))
	if err != nil {
		if err == bitcask.ErrKeyNotFound {
			return ErrObjectNotFound
		}
		return err
	}
	return json.Unmarshal(data, obj)
}

// SetValue puts value in database
func (d *Database) SetValue(prefix, key string, obj any) error {
	if strings.Contains(key, "/") {
		return fmt.Errorf("DB: SetValue can't use '/' in key: %s", key)
	}
	// Serialize value to json
	v, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("DB: SetValue can't serialize value to json: %v", err)
	}
	return d.be.Put(bitcask.Key(fmt.Sprintf("%s:%s", prefix, key)), v)
}

// DelValue removes key from DB
func (d *Database) DelValue(prefix, key string) error {
	if strings.Contains("/", key) {
		return fmt.Errorf("DB: DelValue can't use '/' in key: %s", key)
	}
	return d.be.Delete(bitcask.Key(fmt.Sprintf("%s:%s", prefix, key)))
}
