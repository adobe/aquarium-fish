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
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
)

// UserService implements the User service
type UserService struct {
	fish *fish.Fish
	aquariumv2connect.UnimplementedUserServiceHandler
}

// GetMe implements the GetMe RPC
func (*UserService) GetMe(ctx context.Context, _ /*req*/ *connect.Request[aquariumv2.UserServiceGetMeRequest]) (*connect.Response[aquariumv2.UserServiceGetMeResponse], error) {
	user := rpcutil.GetUserFromContext(ctx)
	if user == nil {
		return connect.NewResponse(&aquariumv2.UserServiceGetMeResponse{
			Status: false, Message: "User not authenticated",
		}), connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// Need to filter out Hash for security reasons
	result := user.ToUser()
	result.Hash = nil

	return connect.NewResponse(&aquariumv2.UserServiceGetMeResponse{
		Status: true, Message: "User details retrieved successfully",
		Data: result,
	}), nil
}

// List implements the List RPC
func (s *UserService) List(ctx context.Context, _ /*req*/ *connect.Request[aquariumv2.UserServiceListRequest]) (*connect.Response[aquariumv2.UserServiceListResponse], error) {
	users, err := s.fish.DB().UserList(ctx)
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
	user, err := s.fish.DB().UserGet(ctx, req.Msg.GetUserName())
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceGetResponse{
			Status: false, Message: "User not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Need to filter out Hash for security reasons
	result := user.ToUser()
	result.Hash = nil

	return connect.NewResponse(&aquariumv2.UserServiceGetResponse{
		Status: true, Message: "User retrieved successfully",
		Data: result,
	}), nil
}

// Create implements the Create RPC
func (s *UserService) Create(ctx context.Context, req *connect.Request[aquariumv2.UserServiceCreateRequest]) (*connect.Response[aquariumv2.UserServiceCreateResponse], error) {
	msgUser := req.Msg.GetUser()
	if msgUser == nil {
		return connect.NewResponse(&aquariumv2.UserServiceCreateResponse{
			Status: false, Message: "User not provided",
		}), connect.NewError(connect.CodeInvalidArgument, nil)
	}
	password := msgUser.GetPassword()
	if password == "" {
		// Generate random password if not provided
		password = generateRandomPassword()
	}

	// Create new user
	password, user, err := s.fish.DB().UserNew(ctx, msgUser.GetName(), password)
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceCreateResponse{
			Status: false, Message: "Failed to create user: " + err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Assigning roles if they are defined
	if msgUser.Roles != nil {
		user.Roles = msgUser.GetRoles()

		if err := s.fish.DB().UserSave(ctx, user); err != nil {
			return connect.NewResponse(&aquariumv2.UserServiceCreateResponse{
				Status: false, Message: "Failed to update user: " + err.Error(),
			}), connect.NewError(connect.CodeInternal, err)
		}
	}

	if msgUser.GetPassword() == "" {
		// Showing the generated password to requestor
		user.Password = &password
	}

	// Need to filter out Hash for security reasons
	result := user.ToUser()
	result.Hash = nil

	return connect.NewResponse(&aquariumv2.UserServiceCreateResponse{
		Status: true, Message: "User created successfully",
		Data: result,
	}), nil
}

// Update implements the Update RPC
func (s *UserService) Update(ctx context.Context, req *connect.Request[aquariumv2.UserServiceUpdateRequest]) (*connect.Response[aquariumv2.UserServiceUpdateResponse], error) {
	msgUser := req.Msg.GetUser()
	if msgUser == nil {
		return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
			Status: false, Message: "User not provided",
		}), connect.NewError(connect.CodeInvalidArgument, nil)
	}
	// Update of User allowed for the User itself & the one who has UpdateAll action
	if !rpcutil.IsUserName(ctx, msgUser.GetName()) && !rpcutil.CheckUserPermission(ctx, auth.UserServiceUpdateAll) {
		return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
			Status: false, Message: "Permission denied",
		}), connect.NewError(connect.CodePermissionDenied, fmt.Errorf("Permission denied"))
	}
	user, err := s.fish.DB().UserGet(ctx, msgUser.GetName())
	if err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
			Status: false, Message: "User not found: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	// Attempt to change password
	if rpcutil.CheckUserPermission(ctx, auth.UserServiceUpdatePassword) {
		password := msgUser.GetPassword()
		if password != "" {
			user.SetHash(generatePasswordHash(password))
		}
	}

	// Attempt to assign user roles
	if rpcutil.CheckUserPermission(ctx, auth.UserServiceUpdateRoles) {
		user.Roles = msgUser.GetRoles()
	}

	if err := s.fish.DB().UserSave(ctx, user); err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
			Status: false, Message: "Failed to update user: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Need to filter out Hash for security reasons
	result := user.ToUser()
	result.Hash = nil

	return connect.NewResponse(&aquariumv2.UserServiceUpdateResponse{
		Status: true, Message: "User updated successfully",
		Data: result,
	}), nil
}

// Remove implements the Remove RPC
func (s *UserService) Remove(ctx context.Context, req *connect.Request[aquariumv2.UserServiceRemoveRequest]) (*connect.Response[aquariumv2.UserServiceRemoveResponse], error) {
	if err := s.fish.DB().UserDelete(ctx, req.Msg.GetUserName()); err != nil {
		return connect.NewResponse(&aquariumv2.UserServiceRemoveResponse{
			Status: false, Message: "Failed to remove user: " + err.Error(),
		}), connect.NewError(connect.CodeNotFound, err)
	}

	return connect.NewResponse(&aquariumv2.UserServiceRemoveResponse{
		Status: true, Message: "User removed successfully",
	}), nil
}
