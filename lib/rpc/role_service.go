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

package rpc

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2/aquariumv2connect"
)

// RoleService implements the Role service
type RoleService struct {
	fish *fish.Fish
	aquariumv2connect.UnimplementedRoleServiceHandler
}

// List implements the List RPC
func (s *RoleService) List(ctx context.Context, req *connect.Request[aquariumv2.RoleServiceListRequest]) (*connect.Response[aquariumv2.RoleServiceListResponse], error) {
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
		resp.Data[i] = convertRole(&role)
	}

	return connect.NewResponse(resp), nil
}

// Get implements the Get RPC
func (s *RoleService) Get(ctx context.Context, req *connect.Request[aquariumv2.RoleServiceGetRequest]) (*connect.Response[aquariumv2.RoleServiceGetResponse], error) {
	role, err := s.fish.DB().RoleGet(req.Msg.Name)
	if err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceGetResponse{
			Status: false, Message: "Role not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceGetResponse{
		Status: true, Message: "Role retrieved successfully",
		Data: convertRole(role),
	}), nil
}

// Create implements the Create RPC
func (s *RoleService) Create(ctx context.Context, req *connect.Request[aquariumv2.RoleServiceCreateRequest]) (*connect.Response[aquariumv2.RoleServiceCreateResponse], error) {
	// Check if role already exists
	if _, err := s.fish.DB().RoleGet(req.Msg.Name); err == nil {
		return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
			Status: false, Message: "Role already exists",
		}), connect.NewError(connect.CodeAlreadyExists, nil)
	}

	role := &types.Role{
		Name:        req.Msg.Name,
		Permissions: make([]types.Permission, len(req.Msg.Permissions)),
	}

	for i, p := range req.Msg.Permissions {
		role.Permissions[i] = types.Permission{
			Resource: p.Resource,
			Action:   p.Action,
		}
	}

	if err := s.fish.DB().RoleCreate(role); err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
			Status: false, Message: "Failed to create role: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceCreateResponse{
		Status: true, Message: "Role created successfully",
		Data: convertRole(role),
	}), nil
}

// Update implements the Update RPC
func (s *RoleService) Update(ctx context.Context, req *connect.Request[aquariumv2.RoleServiceUpdateRequest]) (*connect.Response[aquariumv2.RoleServiceUpdateResponse], error) {
	role, err := s.fish.DB().RoleGet(req.Msg.Name)
	if err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
			Status: false, Message: "Role not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	role.Permissions = make([]types.Permission, len(req.Msg.Permissions))
	for i, p := range req.Msg.Permissions {
		role.Permissions[i] = types.Permission{
			Resource: p.Resource,
			Action:   p.Action,
		}
	}

	if err := s.fish.DB().RoleSave(role); err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
			Status: false, Message: "Failed to update role: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceUpdateResponse{
		Status: true, Message: "Role updated successfully",
		Data: convertRole(role),
	}), nil
}

// Delete implements the Delete RPC
func (s *RoleService) Delete(ctx context.Context, req *connect.Request[aquariumv2.RoleServiceDeleteRequest]) (*connect.Response[aquariumv2.RoleServiceDeleteResponse], error) {
	if err := s.fish.DB().RoleDelete(req.Msg.Name); err != nil {
		return connect.NewResponse(&aquariumv2.RoleServiceDeleteResponse{
			Status: false, Message: "Failed to delete role: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.RoleServiceDeleteResponse{
		Status: true, Message: "Role deleted successfully",
	}), nil
}

// Helper function to convert types.Role to aquariumv2.Role
func convertRole(role *types.Role) *aquariumv2.Role {
	if role == nil {
		return nil
	}

	protoRole := &aquariumv2.Role{
		Name:        role.Name,
		CreatedAt:   timestamppb.New(role.CreatedAt),
		UpdatedAt:   timestamppb.New(role.UpdatedAt),
		Permissions: make([]*aquariumv2.Permission, len(role.Permissions)),
	}

	for i, p := range role.Permissions {
		protoRole.Permissions[i] = &aquariumv2.Permission{
			Resource: p.Resource,
			Action:   p.Action,
		}
	}

	return protoRole
}
