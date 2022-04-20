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
	"fmt"
	"reflect"
)

// Simple serializer to get map as key.subkey=value with dot separation for the keys
func DotSerialize(prefix string, in interface{}) map[string]string {
	out := make(map[string]string)

	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Map {
		for _, k := range v.MapKeys() {
			prefix_key := fmt.Sprintf("%v", k.Interface())
			if len(prefix) > 0 {
				prefix_key = prefix + "." + prefix_key
			}
			int_out := DotSerialize(prefix_key, v.MapIndex(k).Interface())
			for key, val := range int_out {
				out[key] = val
			}
		}
	} else {
		out[prefix] = fmt.Sprintf("%v", in)
	}
	return out
}
