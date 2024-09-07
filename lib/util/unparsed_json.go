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

package util

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// UnparsedJSON is used to store json as is and not parse it until the right time
type UnparsedJSON string

// MarshalJSON represents UnparsedJson as bytes
func (r UnparsedJSON) MarshalJSON() ([]byte, error) {
	return []byte(r), nil
}

// UnmarshalJSON converts bytes to UnparsedJson
func (r *UnparsedJSON) UnmarshalJSON(b []byte) error {
	// Store json as string
	*r = UnparsedJSON(b)
	return nil
}

// UnmarshalYAML is needed to properly convert incoming yaml requests into json
func (r *UnparsedJSON) UnmarshalYAML(node *yaml.Node) error {
	var value any
	if err := node.Decode(&value); err != nil {
		return err
	}
	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r.UnmarshalJSON(jsonData)
	return nil
}
