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

// Author: Sergei Parshev (@sparshev)

package rpc

import (
	"context"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// RoleService implements the Role service
type RoleService struct {
	fish *fish.Fish
	aquariumv2connect.UnimplementedRoleServiceHandler
}

// List implements the List RPC
func (s *RoleService) List(_ /*ctx*/ context.Context, _ /*req*/ *connect.Request[aquariumv2.RoleServiceListRequest]) (*connect.Response[aquariumv2.RoleServiceListResponse], error) {
	roles, err := s.fish.DB().RoleList()
	if err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceListResponse{
			Status: false, Message: "Failed to list roles: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	resp := &aquariumv2.RoleServiceListResponse{
		Status: true, Message: "Roles listed successfully",
		Data: make([]*aquariumv2.Role, len(roles)),
	}

	for i, role := range roles {
		resp.Data[i] = role.ToRole()
	}

	return connect.NewResponse(resp), nil
}

// Get implements the Get RPC
func (s *RoleService) Get(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.RoleServiceGetRequest]) (*connect.Response[aquariumv2.RoleServiceGetResponse], error) {
	role, err := s.fish.DB().RoleGet(req.Msg.GetRoleName())
	if err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceGetResponse{
			Status: false, Message: "Role not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceGetResponse{
		Status: true, Message: "Role retrieved successfully",
		Data: role.ToRole(),
	}), nil
}

// Create implements the Create RPC
func (s *RoleService) Create(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.RoleServiceCreateRequest]) (*connect.Response[aquariumv2.RoleServiceCreateResponse], error) {
	msgRole := req.Msg.GetRole()
	if msgRole == nil {
		return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
			Status: false, Message: "Role not provided",
		}), connect.NewError(connect.CodeInvalidArgument, nil)
	}
	// Check if role already exists
	if _, err := s.fish.DB().RoleGet(msgRole.GetName()); err == nil {
		return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
			Status: false, Message: "Role already exists",
		}), connect.NewError(connect.CodeAlreadyExists, nil)
	}

	role := &typesv2.Role{
		Name:        msgRole.GetName(),
		Permissions: make([]typesv2.Permission, len(msgRole.GetPermissions())),
	}

	for i, p := range msgRole.GetPermissions() {
		role.Permissions[i] = typesv2.Permission{
			Resource: p.GetResource(),
			Action:   p.GetAction(),
		}
	}

	if err := s.fish.DB().RoleCreate(role); err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
			Status: false, Message: "Failed to create role: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
		Status: true, Message: "Role created successfully",
		Data: role.ToRole(),
	}), nil
}

// Update implements the Update RPC
func (s *RoleService) Update(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.RoleServiceUpdateRequest]) (*connect.Response[aquariumv2.RoleServiceUpdateResponse], error) {
	msgRole := req.Msg.GetRole()
	if msgRole == nil {
		return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
			Status: false, Message: "Role not provided",
		}), connect.NewError(connect.CodeInvalidArgument, nil)
	}
	role, err := s.fish.DB().RoleGet(msgRole.GetName())
	if err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
			Status: false, Message: "Role not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	role.Permissions = make([]typesv2.Permission, len(msgRole.GetPermissions()))
	for i, p := range msgRole.GetPermissions() {
		role.Permissions[i] = typesv2.Permission{
			Resource: p.GetResource(),
			Action:   p.GetAction(),
		}
	}

	if err := s.fish.DB().RoleSave(role); err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
			Status: false, Message: "Failed to update role: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
		Status: true, Message: "Role updated successfully",
		Data: role.ToRole(),
	}), nil
}

// Delete implements the Delete RPC
func (s *RoleService) Delete(_ /*ctx*/ context.Context, req *connect.Request[aquariumv2.RoleServiceDeleteRequest]) (*connect.Response[aquariumv2.RoleServiceDeleteResponse], error) {
	if err := s.fish.DB().RoleDelete(req.Msg.GetRoleName()); err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceDeleteResponse{
			Status: false, Message: "Failed to delete role: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceDeleteResponse{
		Status: true, Message: "Role deleted successfully",
	}), nil
}
