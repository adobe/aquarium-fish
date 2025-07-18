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
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pbTypes "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
)

// Permission is a data for Permission without internal locks
type Permission struct {
	Action   string `json:"action,omitempty"`
	Resource string `json:"resource,omitempty"`
}

// FromPermission creates a Permission from Permission
func FromPermission(src *pbTypes.Permission) Permission {
	if src == nil {
		return Permission{}
	}

	result := Permission{}
	result.Action = src.GetAction()
	result.Resource = src.GetResource()
	return result
}

// ToPermission converts Permission to Permission
func (p Permission) ToPermission() *pbTypes.Permission {
	result := &pbTypes.Permission{}

	result.Action = p.Action
	result.Resource = p.Resource
	return result
}

// Role is a data for Role without internal locks
type Role struct {
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	Name        string       `json:"name,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at,omitempty"`
}

// FromRole creates a Role from Role
func FromRole(src *pbTypes.Role) Role {
	if src == nil {
		return Role{}
	}

	result := Role{}
	if src.GetCreatedAt() != nil {
		result.CreatedAt = src.GetCreatedAt().AsTime()
	}
	result.Name = src.GetName()
	for _, item := range src.GetPermissions() {
		if item != nil {
			result.Permissions = append(result.Permissions, FromPermission(item))
		}
	}
	if src.GetUpdatedAt() != nil {
		result.UpdatedAt = src.GetUpdatedAt().AsTime()
	}
	return result
}

// ToRole converts Role to Role
func (r Role) ToRole() *pbTypes.Role {
	result := &pbTypes.Role{}

	result.CreatedAt = timestamppb.New(r.CreatedAt)
	result.Name = r.Name
	for _, item := range r.Permissions {
		result.Permissions = append(result.Permissions, item.ToPermission())
	}
	result.UpdatedAt = timestamppb.New(r.UpdatedAt)
	return result
}
