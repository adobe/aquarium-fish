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
func (d *Database) Has(prefix, key string) (bool, error) {
	fullkey := fmt.Sprintf("%s:%s", prefix, key)
	if strings.Contains(fullkey, "/") {
		return false, fmt.Errorf("DB: Has can't use '/' in key: %s", fullkey)
	}
	return d.be.Has(bitcask.Key(fullkey)), nil
}

// GetValue returns data by key
func (d *Database) Get(prefix, key string, obj any) error {
	fullkey := fmt.Sprintf("%s:%s", prefix, key)
	if strings.Contains(fullkey, "/") {
		return fmt.Errorf("DB: Get can't use '/' in key: %s", fullkey)
	}
	data, err := d.be.Get(bitcask.Key(fullkey))
	if err != nil {
		if err == bitcask.ErrKeyNotFound {
			return ErrObjectNotFound
		}
		return err
	}
	return json.Unmarshal(data, obj)
}

// SetValue puts value in database
func (d *Database) Set(prefix, key string, obj any) error {
	fullkey := fmt.Sprintf("%s:%s", prefix, key)
	if strings.Contains(fullkey, "/") {
		return fmt.Errorf("DB: Set can't use '/' in key: %s", fullkey)
	}
	// Serialize value to json
	v, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("DB: Set can't serialize value to json: %v", err)
	}
	return d.be.Put(bitcask.Key(fullkey), v)
}

// DelValue removes key from DB
func (d *Database) Del(prefix, key string) error {
	fullkey := fmt.Sprintf("%s:%s", prefix, key)
	if strings.Contains(fullkey, "/") {
		return fmt.Errorf("DB: Del can't use '/' in key: %s", fullkey)
	}
	return d.be.Delete(bitcask.Key(fullkey))
}

// ScanValue iterates over key prefixes and executing the provided function for each one
func (d *Database) Scan(prefix string, f func(string) error) error {
	if strings.Contains(prefix, "/") {
		return fmt.Errorf("DB: Scan can't use '/' in prefix: %s", prefix)
	}
	return d.be.Scan(bitcask.Key(prefix+":"), func(bkey bitcask.Key) error {
		key := strings.SplitN(string(bkey), ":", 2)[1]
		// Skipping keys with "/"
		if strings.Contains(key, "/") {
			return nil
		}
		return f(key)
	})
}
