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
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_rbac_permissions verifies RBAC permissions through bidirectional streaming
// This test ensures that:
// 1. A new user without any role cannot access any resources through streaming
// 2. A user with User role can create and manage their own applications through streaming
// 3. A user with Administrator role can access and manage all resources through streaming
// 4. Users cannot access other users' applications unless they have admin privileges
func Test_rbac_permissions(t *testing.T) {
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

	// Create test users
	regularUserPass := "regular-pass"
	powerUserPass := "power-pass"
	regularUser := aquariumv2.User{
		Name:     "regular-user",
		Password: &regularUserPass,
	}
	powerUser := aquariumv2.User{
		Name:     "power-user",
		Password: &powerUserPass,
	}

	// Create clients for different users
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC, afi.GetCA(t))
	regularCli, regularOpts := h.NewRPCClient(regularUser.Name, regularUserPass, h.RPCClientGRPC, afi.GetCA(t))
	powerCli, powerOpts := h.NewRPCClient(powerUser.Name, powerUserPass, h.RPCClientGRPC, afi.GetCA(t))

	// Create streaming service clients
	adminStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	regularStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		regularCli,
		afi.APIAddress("grpc"),
		regularOpts...,
	)
	powerStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		powerCli,
		afi.APIAddress("grpc"),
		powerOpts...,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Setup streaming helpers for each user
	adminHelper := h.NewStreamingTestHelper(ctx, t, "admin", adminStreamingClient)
	defer adminHelper.Close()

	regularHelper := h.NewStreamingTestHelper(ctx, t, "regularUser", regularStreamingClient)
	defer regularHelper.Close()

	powerHelper := h.NewStreamingTestHelper(ctx, t, "powerUser", powerStreamingClient)
	defer powerHelper.Close()

	// Setup bidirectional streaming for admin only (others will be set up after role assignment)
	if err := adminHelper.SetupFullStreaming(nil); err != nil {
		t.Fatalf("Failed to setup admin streaming: %v", err)
	}

	t.Run("Admin: Create test users through streaming", func(t *testing.T) {
		// Create regular user
		regularUserReq := &aquariumv2.UserServiceCreateRequest{User: &regularUser}
		_, err := adminHelper.SendRequestAndExpectSuccess(
			"create-regular-user",
			"UserServiceCreateRequest",
			regularUserReq,
			"UserServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create regular user through streaming:", err)
		}

		// Create power user
		powerUserReq := &aquariumv2.UserServiceCreateRequest{User: &powerUser}
		_, err = adminHelper.SendRequestAndExpectSuccess(
			"create-power-user",
			"UserServiceCreateRequest",
			powerUserReq,
			"UserServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create power user through streaming:", err)
		}
	})

	t.Run("Regular user without role: Cannot access streaming at all", func(t *testing.T) {
		// User without role should not be able to establish streaming connection
		// This is expected behavior - streaming requires proper RBAC permissions
		getMeReq := &aquariumv2.UserServiceGetMeRequest{}
		resp, err := regularHelper.GetStreamingClient().SendRequest(
			"get-me",
			"UserServiceGetMeRequest",
			getMeReq,
		)

		// We expect this to fail because user doesn't have streaming access
		if err == nil {
			t.Error("Expected streaming to fail for user without role")
		} else {
			t.Logf("Expected failure for user without role: %v", err)
		}

		// If somehow we got a response, it should be an error
		if resp != nil && resp.Error == nil {
			t.Error("Expected error response for user without role")
		}
	})

	// Note: We skip testing individual permission denials for users without roles
	// since they can't establish streaming connections at all

	// Assign User role to regular user
	t.Run("Admin: Assign User role to regular user through streaming", func(t *testing.T) {
		userUpdateReq := &aquariumv2.UserServiceUpdateRequest{
			User: &aquariumv2.User{
				Name:  regularUser.Name,
				Roles: []string{"User"},
			},
		}
		_, err := adminHelper.SendRequestAndExpectSuccess(
			"assign-user-role",
			"UserServiceUpdateRequest",
			userUpdateReq,
			"UserServiceUpdateResponse",
		)
		if err != nil {
			t.Fatal("Failed to assign role through streaming:", err)
		}

		// Now setup streaming for regular user since they have role
		if err := regularHelper.SetupFullStreaming(nil); err != nil {
			t.Fatalf("Failed to setup regular user streaming after role assignment: %v", err)
		}
	})

	// Create a test label
	var labelUID string
	t.Run("Admin: Create test label through streaming", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"test": "value"})
		labelCreateReq := &aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "test-label",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver: "test",
					Resources: &aquariumv2.Resources{
						Cpu: 1,
						Ram: 2,
					},
				}},
				Metadata: md,
			},
		}
		resp, err := adminHelper.SendRequestAndExpectSuccess(
			"create-label",
			"LabelServiceCreateRequest",
			labelCreateReq,
			"LabelServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create label through streaming:", err)
		}

		var labelResp aquariumv2.LabelServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&labelResp); err != nil {
			t.Fatal("Failed to unmarshal label response:", err)
		}
		labelUID = labelResp.Data.Uid
	})

	// Test regular user with User role
	var regularUserAppUID string
	t.Run("Regular user with User role: Basic operations through streaming", func(t *testing.T) {
		// Should be able to list labels
		labelListReq := &aquariumv2.LabelServiceListRequest{}
		_, err := regularHelper.SendRequestAndExpectSuccess(
			"list-labels",
			"LabelServiceListRequest",
			labelListReq,
			"LabelServiceListResponse",
		)
		if err != nil {
			t.Error("Expected to be able to list labels through streaming:", err)
		}

		// Should be able to create application
		md, _ := structpb.NewStruct(map[string]any{"user": "regular"})
		appCreateReq := &aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{
				LabelUid: labelUID,
				Metadata: md,
			},
		}
		resp, err := regularHelper.SendRequestAndExpectSuccess(
			"create-app",
			"ApplicationServiceCreateRequest",
			appCreateReq,
			"ApplicationServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Expected to be able to create application through streaming:", err)
		}

		var appResp aquariumv2.ApplicationServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
			t.Fatal("Failed to unmarshal application response:", err)
		}
		regularUserAppUID = appResp.Data.Uid

		// Should be able to view own application
		appGetReq := &aquariumv2.ApplicationServiceGetRequest{
			ApplicationUid: regularUserAppUID,
		}
		_, err = regularHelper.SendRequestAndExpectSuccess(
			"get-own-app",
			"ApplicationServiceGetRequest",
			appGetReq,
			"ApplicationServiceGetResponse",
		)
		if err != nil {
			t.Error("Expected to be able to view own application through streaming:", err)
		}
	})

	// Create another application as admin
	var adminAppUID string
	t.Run("Admin: Create another application through streaming", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"user": "admin"})
		appCreateReq := &aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{
				LabelUid: labelUID,
				Metadata: md,
			},
		}
		resp, err := adminHelper.SendRequestAndExpectSuccess(
			"create-admin-app",
			"ApplicationServiceCreateRequest",
			appCreateReq,
			"ApplicationServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create admin application through streaming:", err)
		}

		var appResp aquariumv2.ApplicationServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
			t.Fatal("Failed to unmarshal application response:", err)
		}
		adminAppUID = appResp.Data.Uid
	})

	t.Run("Regular user: Cannot access admin's application through streaming", func(t *testing.T) {
		// Should NOT be able to view admin's application
		appGetReq := &aquariumv2.ApplicationServiceGetRequest{
			ApplicationUid: adminAppUID,
		}
		resp, err := regularHelper.GetStreamingClient().SendRequest(
			"get-admin-app",
			"ApplicationServiceGetRequest",
			appGetReq,
		)
		if err != nil {
			t.Fatal("Failed to send get admin app request:", err)
		}

		if resp.Error == nil {
			t.Error("Expected to be denied access to admin's application through streaming")
		}
		if !strings.Contains(resp.Error.Message, "Permission denied") {
			t.Error("Expected Permission denied error, got:", resp.Error.Message)
		}
	})

	// Assign Administrator role to power user
	t.Run("Admin: Assign Administrator role to power user through streaming", func(t *testing.T) {
		userUpdateReq := &aquariumv2.UserServiceUpdateRequest{
			User: &aquariumv2.User{
				Name:  powerUser.Name,
				Roles: []string{"Administrator"},
			},
		}
		_, err := adminHelper.SendRequestAndExpectSuccess(
			"assign-admin-role",
			"UserServiceUpdateRequest",
			userUpdateReq,
			"UserServiceUpdateResponse",
		)
		if err != nil {
			t.Fatal("Failed to assign Administrator role through streaming:", err)
		}

		// Now setup streaming for power user since they have Administrator role
		if err := powerHelper.SetupFullStreaming(nil); err != nil {
			t.Fatalf("Failed to setup power user streaming after role assignment: %v", err)
		}
	})

	t.Run("Power user with Administrator role: Full access through streaming", func(t *testing.T) {
		// Should be able to list users
		userListReq := &aquariumv2.UserServiceListRequest{}
		_, err := powerHelper.SendRequestAndExpectSuccess(
			"list-users",
			"UserServiceListRequest",
			userListReq,
			"UserServiceListResponse",
		)
		if err != nil {
			t.Error("Expected to be able to list users through streaming:", err)
		}

		// Should be able to create labels
		md, _ := structpb.NewStruct(map[string]any{"created-by": "power-user"})
		labelCreateReq := &aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "admin-label",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver: "test",
					Resources: &aquariumv2.Resources{
						Cpu: 1,
						Ram: 2,
					},
				}},
				Metadata: md,
			},
		}
		_, err = powerHelper.SendRequestAndExpectSuccess(
			"create-admin-label",
			"LabelServiceCreateRequest",
			labelCreateReq,
			"LabelServiceCreateResponse",
		)
		if err != nil {
			t.Error("Expected to be able to create labels through streaming:", err)
		}

		// Should be able to view regular user's application
		appGetReq := &aquariumv2.ApplicationServiceGetRequest{
			ApplicationUid: regularUserAppUID,
		}
		_, err = powerHelper.SendRequestAndExpectSuccess(
			"get-regular-app",
			"ApplicationServiceGetRequest",
			appGetReq,
			"ApplicationServiceGetResponse",
		)
		if err != nil {
			t.Error("Expected to be able to view regular user's application through streaming:", err)
		}

		// Should be able to view admin's application
		appGetReq = &aquariumv2.ApplicationServiceGetRequest{
			ApplicationUid: adminAppUID,
		}
		_, err = powerHelper.SendRequestAndExpectSuccess(
			"get-admin-app",
			"ApplicationServiceGetRequest",
			appGetReq,
			"ApplicationServiceGetResponse",
		)
		if err != nil {
			t.Error("Expected to be able to view admin's application through streaming:", err)
		}
	})

	// Test application cleanup
	t.Run("Cleanup: Deallocate applications through streaming", func(t *testing.T) {
		// Regular user deallocates their application
		deallocateReq := &aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: regularUserAppUID,
		}
		_, err := regularHelper.SendRequestAndExpectSuccess(
			"deallocate-regular-app",
			"ApplicationServiceDeallocateRequest",
			deallocateReq,
			"ApplicationServiceDeallocateResponse",
		)
		if err != nil {
			t.Error("Failed to deallocate regular user's application through streaming:", err)
		}

		// Admin deallocates their application
		deallocateReq = &aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: adminAppUID,
		}
		_, err = adminHelper.SendRequestAndExpectSuccess(
			"deallocate-admin-app",
			"ApplicationServiceDeallocateRequest",
			deallocateReq,
			"ApplicationServiceDeallocateResponse",
		)
		if err != nil {
			t.Error("Failed to deallocate admin's application through streaming:", err)
		}
	})
}
