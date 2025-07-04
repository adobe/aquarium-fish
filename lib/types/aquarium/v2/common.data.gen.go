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

// Code generated by Aquarium buf-gen-pb-data. DO NOT EDIT.

package aquariumv2

import (
	pbTypes "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
)

// Authentication is a data for Authentication without internal locks
type Authentication struct {
	Key      string `json:"key,omitempty"`
	Password string `json:"password,omitempty"`
	Port     int32  `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
}

// FromAuthentication creates a Authentication from Authentication
func FromAuthentication(src *pbTypes.Authentication) Authentication {
	if src == nil {
		return Authentication{}
	}

	result := Authentication{}
	result.Key = src.GetKey()
	result.Password = src.GetPassword()
	result.Port = src.GetPort()
	result.Username = src.GetUsername()
	return result
}

// ToAuthentication converts Authentication to Authentication
func (a Authentication) ToAuthentication() *pbTypes.Authentication {
	result := &pbTypes.Authentication{}

	result.Key = a.Key
	result.Password = a.Password
	result.Port = a.Port
	result.Username = a.Username
	return result
}
