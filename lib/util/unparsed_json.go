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

type UnparsedJson string

func (r UnparsedJson) MarshalJSON() ([]byte, error) {
	return []byte(r), nil
}

func (r *UnparsedJson) UnmarshalJSON(b []byte) error {
	// Store json as string
	*r = UnparsedJson(b)
	return nil
}

// To properly convert incoming yaml requests into json
func (r *UnparsedJson) UnmarshalYAML(node *yaml.Node) error {
	var value any
	if err := node.Decode(&value); err != nil {
		return err
	}
	json_data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	r.UnmarshalJSON(json_data)
	return nil
}
