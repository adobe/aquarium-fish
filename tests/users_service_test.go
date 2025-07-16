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

package tests

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_users_service verifies user service functionality including:
// 1. Hash is filtered for all user methods response
// 2. Password is set only when user created without password (and when with it should be set to auto-generated string)
// 3. User could be created with assigned roles or without
// 4. User roles could be updated
func Test_users_service(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA())
	adminUserClient := aquariumv2connect.NewUserServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	t.Run("Create user without password - should generate auto password", func(t *testing.T) {
		user := &aquariumv2.User{
			Name: "test-user-no-pass",
		}

		resp, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user without password:", err)
		}

		// Verify response
		if !resp.Msg.Status {
			t.Error("Expected successful user creation")
		}

		// Verify auto-generated password is returned
		if resp.Msg.Data.Password == nil || *resp.Msg.Data.Password == "" {
			t.Error("Expected auto-generated password to be returned when user created without password")
		}

		// Verify hash is filtered out
		if resp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Create response")
		}

		// Verify user was created
		if resp.Msg.Data.Name != user.Name {
			t.Error("Expected user name to match:", resp.Msg.Data.Name, "!=", user.Name)
		}
	})

	t.Run("Create user with password - should not return password", func(t *testing.T) {
		userPassword := "custom-password-123"
		user := &aquariumv2.User{
			Name:     "test-user-with-pass",
			Password: &userPassword,
		}

		resp, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user with password:", err)
		}

		// Verify response
		if !resp.Msg.Status {
			t.Error("Expected successful user creation")
		}

		// Verify password is not returned when provided
		if resp.Msg.Data.Password != nil {
			t.Error("Expected password to not be returned when user created with password")
		}

		// Verify hash is filtered out
		if resp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Create response")
		}

		// Verify user was created
		if resp.Msg.Data.Name != user.Name {
			t.Error("Expected user name to match:", resp.Msg.Data.Name, "!=", user.Name)
		}
	})

	t.Run("Create user with roles", func(t *testing.T) {
		user := &aquariumv2.User{
			Name:  "test-user-with-roles",
			Roles: []string{"User", "Administrator"},
		}

		resp, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user with roles:", err)
		}

		// Verify response
		if !resp.Msg.Status {
			t.Error("Expected successful user creation")
		}

		// Verify roles are assigned
		if len(resp.Msg.Data.Roles) != 2 {
			t.Error("Expected 2 roles to be assigned, got:", len(resp.Msg.Data.Roles))
		}

		expectedRoles := map[string]bool{"User": true, "Administrator": true}
		for _, role := range resp.Msg.Data.Roles {
			if !expectedRoles[role] {
				t.Error("Unexpected role assigned:", role)
			}
		}

		// Verify hash is filtered out
		if resp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Create response")
		}
	})

	t.Run("Create user without roles", func(t *testing.T) {
		user := &aquariumv2.User{
			Name: "test-user-no-roles",
		}

		resp, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user without roles:", err)
		}

		// Verify response
		if !resp.Msg.Status {
			t.Error("Expected successful user creation")
		}

		// Verify no roles are assigned
		if len(resp.Msg.Data.Roles) != 0 {
			t.Error("Expected no roles to be assigned, got:", len(resp.Msg.Data.Roles))
		}

		// Verify hash is filtered out
		if resp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Create response")
		}
	})

	t.Run("Update user roles", func(t *testing.T) {
		// First create a user
		user := &aquariumv2.User{
			Name: "test-user-update-roles",
		}

		createResp, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user for update test:", err)
		}

		// Update user with roles
		updateUser := &aquariumv2.User{
			Name:  user.Name,
			Roles: []string{"User"},
		}

		updateResp, err := adminUserClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceUpdateRequest{User: updateUser}),
		)
		if err != nil {
			t.Fatal("Failed to update user roles:", err)
		}

		// Verify response
		if !updateResp.Msg.Status {
			t.Error("Expected successful user update")
		}

		// Verify roles are updated
		if len(updateResp.Msg.Data.Roles) != 1 {
			t.Error("Expected 1 role to be assigned, got:", len(updateResp.Msg.Data.Roles))
		}

		if updateResp.Msg.Data.Roles[0] != "User" {
			t.Error("Expected 'User' role, got:", updateResp.Msg.Data.Roles[0])
		}

		// Verify hash is filtered out
		if updateResp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Update response")
		}

		// Verify we can see the user was created without roles initially
		if len(createResp.Msg.Data.Roles) != 0 {
			t.Error("Expected user to be created without roles initially")
		}
	})

	t.Run("Get user - hash should be filtered", func(t *testing.T) {
		// Create a user first
		user := &aquariumv2.User{
			Name:  "test-user-get",
			Roles: []string{"User"},
		}

		_, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user for get test:", err)
		}

		// Get the user
		getResp, err := adminUserClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceGetRequest{UserName: user.Name}),
		)
		if err != nil {
			t.Fatal("Failed to get user:", err)
		}

		// Verify response
		if !getResp.Msg.Status {
			t.Error("Expected successful user get")
		}

		// Verify hash is filtered out
		if getResp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Get response")
		}

		// Verify user data
		if getResp.Msg.Data.Name != user.Name {
			t.Error("Expected user name to match:", getResp.Msg.Data.Name, "!=", user.Name)
		}

		if len(getResp.Msg.Data.Roles) != 1 || getResp.Msg.Data.Roles[0] != "User" {
			t.Error("Expected user to have 'User' role")
		}
	})

	t.Run("List users - hash should be filtered", func(t *testing.T) {
		// List all users
		listResp, err := adminUserClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list users:", err)
		}

		// Verify response
		if !listResp.Msg.Status {
			t.Error("Expected successful user list")
		}

		// Verify at least one user exists (admin)
		if len(listResp.Msg.Data) == 0 {
			t.Error("Expected at least one user in the list")
		}

		// Verify hash is filtered out for all users
		for i, user := range listResp.Msg.Data {
			if user.Hash != nil {
				t.Errorf("Expected hash to be filtered out from List response for user %d", i)
			}
		}
	})

	t.Run("GetMe - hash should be filtered", func(t *testing.T) {
		// Get current user (admin)
		getMeResp, err := adminUserClient.GetMe(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to get current user:", err)
		}

		// Verify response
		if !getMeResp.Msg.Status {
			t.Error("Expected successful GetMe")
		}

		// Verify hash is filtered out
		if getMeResp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from GetMe response")
		}

		// Verify user data
		if getMeResp.Msg.Data.Name != "admin" {
			t.Error("Expected current user to be admin")
		}
	})

	t.Run("Update user roles to empty list", func(t *testing.T) {
		// Create a user with roles first
		user := &aquariumv2.User{
			Name:  "test-user-remove-roles",
			Roles: []string{"User", "Administrator"},
		}

		_, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
		)
		if err != nil {
			t.Fatal("Failed to create user with roles:", err)
		}

		// Update user to remove all roles
		updateUser := &aquariumv2.User{
			Name:  user.Name,
			Roles: []string{}, // Empty roles
		}

		updateResp, err := adminUserClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceUpdateRequest{User: updateUser}),
		)
		if err != nil {
			t.Fatal("Failed to update user to remove roles:", err)
		}

		// Verify response
		if !updateResp.Msg.Status {
			t.Error("Expected successful user update")
		}

		// Verify all roles are removed
		if len(updateResp.Msg.Data.Roles) != 0 {
			t.Error("Expected no roles after update, got:", len(updateResp.Msg.Data.Roles))
		}

		// Verify hash is filtered out
		if updateResp.Msg.Data.Hash != nil {
			t.Error("Expected hash to be filtered out from Update response")
		}
	})
}
