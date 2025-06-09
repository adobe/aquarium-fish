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
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2/aquariumv2connect"
)

// UserService implements the User service
type UserService struct {
	fish *fish.Fish
	aquariumv2connect.UnimplementedUserServiceHandler
}

// GetMe implements the GetMe RPC
func (s *UserService) GetMe(ctx context.Context, req *connect.Request[aquariumv2.UserServiceGetMeRequest]) (*connect.Response[aquariumv2.UserServiceGetMeResponse], error) {
	user := GetUserFromContext(ctx)
	if user == nil {
		return connect.NewResponse(&aquariumv2.UserServiceGetMeResponse{
			Status: false, Message: "User not authenticated",
		}), connect.NewError(connect.CodeUnauthenticated, nil)
	}

	return connect.NewResponse(&aquariumv2.UserServiceGetMeResponse{
		Status: true, Message: "User details retrieved successfully",
		Data: &aquariumv2.User{
			Name:      user.Name,
			CreatedAt: timestamppb.New(user.CreatedAt),
			UpdatedAt: timestamppb.New(user.UpdatedAt),
			Roles:     user.Roles,
		},
	}), nil
}

// List implements the List RPC
func (s *UserService) List(ctx context.Context, req *connect.Request[aquariumv2.UserServiceListRequest]) (*connect.Response[aquariumv2.UserServiceListResponse], error) {
	users, err := s.fish.DB().UserList()
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceListResponse{
			Status: false, Message: "Failed to list users: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	resp := &aquariumv2.UserServiceListResponse{
		Status: true, Message: "Users listed successfully",
		Data: make([]*aquariumv2.User, len(users)),
	}

	for i, user := range users {
		resp.Data[i] = &aquariumv2.User{
			Name:      user.Name,
			CreatedAt: timestamppb.New(user.CreatedAt),
			UpdatedAt: timestamppb.New(user.UpdatedAt),
			Roles:     user.Roles,
		}
	}

	return connect.NewResponse(resp), nil
}

// Get implements the Get RPC
func (s *UserService) Get(ctx context.Context, req *connect.Request[aquariumv2.UserServiceGetRequest]) (*connect.Response[aquariumv2.UserServiceGetResponse], error) {
	user, err := s.fish.DB().UserGet(req.Msg.Name)
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceGetResponse{
			Status: false, Message: "User not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.UserServiceGetResponse{
		Status: true, Message: "User retrieved successfully",
		Data: &aquariumv2.User{
			Name:      user.Name,
			CreatedAt: timestamppb.New(user.CreatedAt),
			UpdatedAt: timestamppb.New(user.UpdatedAt),
			Roles:     user.Roles,
		},
	}), nil
}

// Create implements the Create RPC
func (s *UserService) Create(ctx context.Context, req *connect.Request[aquariumv2.UserServiceCreateRequest]) (*connect.Response[aquariumv2.UserServiceCreateResponse], error) {
	password := req.Msg.GetPassword()
	if password == "" {
		// Generate random password if not provided
		password = GenerateRandomPassword()
	}

	// Create new user
	password, user, err := s.fish.DB().UserNew(req.Msg.Name, password)
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceCreateResponse{
			Status: false, Message: "Failed to create user: " + err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	return connect.NewResponse(&aquariumv2.UserServiceCreateResponse{
		Status: true, Message: "User created successfully",
		Data: &aquariumv2.UserWithPassword{
			Name:      user.Name,
			CreatedAt: timestamppb.New(user.CreatedAt),
			UpdatedAt: timestamppb.New(user.UpdatedAt),
			Password:  password,
			Roles:     user.Roles,
		},
	}), nil
}

// Update implements the Update RPC
func (s *UserService) Update(ctx context.Context, req *connect.Request[aquariumv2.UserServiceUpdateRequest]) (*connect.Response[aquariumv2.UserServiceUpdateResponse], error) {
	user, err := s.fish.DB().UserGet(req.Msg.Name)
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
			Status: false, Message: "User not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	password := req.Msg.GetPassword()
	if password != "" {
		user.Hash = GeneratePasswordHash(password)
	}

	user.Roles = req.Msg.Roles
	if err := s.fish.DB().UserSave(user); err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
			Status: false, Message: "Failed to update user: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
		Status: true, Message: "User updated successfully",
		Data: &aquariumv2.UserWithPassword{
			Name:      user.Name,
			CreatedAt: timestamppb.New(user.CreatedAt),
			UpdatedAt: timestamppb.New(user.UpdatedAt),
			Password:  password,
			Roles:     user.Roles,
		},
	}), nil
}

// Delete implements the Delete RPC
func (s *UserService) Delete(ctx context.Context, req *connect.Request[aquariumv2.UserServiceDeleteRequest]) (*connect.Response[aquariumv2.UserServiceDeleteResponse], error) {
	if err := s.fish.DB().UserDelete(req.Msg.Name); err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceDeleteResponse{
			Status: false, Message: "Failed to delete user: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.UserServiceDeleteResponse{
		Status: true, Message: "User deleted successfully",
	}), nil
}

// AssignRoles implements the AssignRoles RPC
func (s *UserService) AssignRoles(ctx context.Context, req *connect.Request[aquariumv2.UserServiceAssignRolesRequest]) (*connect.Response[aquariumv2.UserServiceAssignRolesResponse], error) {
	user, err := s.fish.DB().UserGet(req.Msg.Name)
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceAssignRolesResponse{
			Status: false, Message: "User not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	user.Roles = req.Msg.Roles
	if err := s.fish.DB().UserSave(user); err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceAssignRolesResponse{
			Status: false, Message: "Failed to assign roles: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.UserServiceAssignRolesResponse{
		Status: true, Message: "User roles updated successfully",
	}), nil
}
