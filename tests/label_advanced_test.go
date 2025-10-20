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
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_label_ownership_and_visibility tests label ownership and visibility with different users
// This test ensures:
// * Labels can be filtered by owner and visible_for fields
// * Users can only see labels they own or are visible to them/their groups
// * Admin can see all labels
// * Get operation respects ownership and visibility
func Test_label_ownership_and_visibility(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create test users
	user1Pass := "user1-pass"
	user2Pass := "user2-pass"
	user3Pass := "user3-pass"
	user1 := aquariumv2.User{
		Name:     "user1",
		Password: &user1Pass,
		Roles:    []string{"User"},
	}
	user2 := aquariumv2.User{
		Name:     "user2",
		Password: &user2Pass,
		Roles:    []string{"User"},
	}
	user3 := aquariumv2.User{
		Name:     "user3",
		Password: &user3Pass,
		Roles:    []string{"User"},
	}

	// Create clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	user1Cli, user1Opts := h.NewRPCClient(user1.Name, user1Pass, h.RPCClientREST, afi.GetCA(t))
	user2Cli, user2Opts := h.NewRPCClient(user2.Name, user2Pass, h.RPCClientREST, afi.GetCA(t))
	user3Cli, user3Opts := h.NewRPCClient(user3.Name, user3Pass, h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	adminUserClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	user1LabelClient := aquariumv2connect.NewLabelServiceClient(user1Cli, afi.APIAddress("grpc"), user1Opts...)
	user2LabelClient := aquariumv2connect.NewLabelServiceClient(user2Cli, afi.APIAddress("grpc"), user2Opts...)
	user3LabelClient := aquariumv2connect.NewLabelServiceClient(user3Cli, afi.APIAddress("grpc"), user3Opts...)

	// Create users and a user group
	t.Run("Admin: Create test users", func(t *testing.T) {
		for _, user := range []*aquariumv2.User{&user1, &user2, &user3} {
			_, err := adminUserClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
			)
			if err != nil {
				t.Fatalf("Failed to create user %s: %v", user.Name, err)
			}
		}
	})

	t.Run("Admin: Create user group with user1 and user2", func(t *testing.T) {
		_, err := adminUserClient.CreateGroup(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateGroupRequest{
				Usergroup: &aquariumv2.UserGroup{
					Name:  "team-alpha",
					Users: []string{user1.Name, user2.Name},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create user group:", err)
		}
	})

	// Create labels with different ownership and visibility
	var (
		adminPublicLabelUID   string
		user1PrivateLabelUID  string
		user1GroupLabelUID    string
		user2ForUser3LabelUID string
		user1SelfOnlyLabelUID string
	)

	// Admin creates a public versioned label (visible to everyone)
	t.Run("Admin: Create public versioned label", func(t *testing.T) {
		resp, err := adminLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "public-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
					// No visible_for means visible to everyone
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create public label:", err)
		}
		adminPublicLabelUID = resp.Msg.Data.Uid
	})

	// User1 creates a temporary label visible only to themselves
	removeAt1 := time.Now().Add(1 * time.Hour)
	t.Run("User1: Create private temporary label", func(t *testing.T) {
		resp, err := user1LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user1-private",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt1),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create private label:", err)
		}
		user1PrivateLabelUID = resp.Msg.Data.Uid
	})

	// User1 creates a temporary label visible to their group
	removeAt2 := time.Now().Add(2 * time.Hour)
	t.Run("User1: Create group-visible temporary label", func(t *testing.T) {
		resp, err := user1LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user1-group",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt2),
					VisibleFor: []string{"team-alpha"},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create group label:", err)
		}
		user1GroupLabelUID = resp.Msg.Data.Uid
	})

	// User2 creates a temporary label visible to user2 only (not user3, as user3 is not in user2's groups)
	removeAt3 := time.Now().Add(3 * time.Hour)
	t.Run("User2: Create label visible to user2 only", func(t *testing.T) {
		resp, err := user2LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user2-self-label",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt3),
					VisibleFor: []string{user2.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create user2-only label:", err)
		}
		user2ForUser3LabelUID = resp.Msg.Data.Uid
	})

	// User1 creates a temporary label visible only to self (not group)
	removeAt4 := time.Now().Add(4 * time.Hour)
	t.Run("User1: Create self-only temporary label", func(t *testing.T) {
		resp, err := user1LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user1-self-only",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt4),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create self-only label:", err)
		}
		user1SelfOnlyLabelUID = resp.Msg.Data.Uid
	})

	// Test List operations with different users
	t.Run("Admin: List all labels (should see 5 labels)", func(t *testing.T) {
		resp, err := adminLabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}
		if len(resp.Msg.Data) != 5 {
			t.Errorf("Expected 5 labels, got %d", len(resp.Msg.Data))
		}
	})

	t.Run("User1: List labels (should see 4: public, own private, own group, own self-only)", func(t *testing.T) {
		resp, err := user1LabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}
		if len(resp.Msg.Data) != 4 {
			t.Errorf("Expected 4 labels, got %d", len(resp.Msg.Data))
		}

		// Verify user1 sees correct labels
		seenUIDs := make(map[string]bool)
		for _, label := range resp.Msg.Data {
			seenUIDs[label.Uid] = true
		}
		if !seenUIDs[adminPublicLabelUID] {
			t.Error("User1 should see public label")
		}
		if !seenUIDs[user1PrivateLabelUID] {
			t.Error("User1 should see own private label")
		}
		if !seenUIDs[user1GroupLabelUID] {
			t.Error("User1 should see own group label")
		}
		// Note: user1SelfOnlyLabelUID is the same as user1PrivateLabelUID in terms of visibility
		if seenUIDs[user2ForUser3LabelUID] {
			t.Error("User1 should not see user2's label")
		}
	})

	t.Run("User2: List labels (should see 3: public, user1 group label, own label)", func(t *testing.T) {
		resp, err := user2LabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}
		if len(resp.Msg.Data) != 3 {
			t.Errorf("Expected 3 labels, got %d", len(resp.Msg.Data))
		}

		// Verify user2 sees correct labels
		seenUIDs := make(map[string]bool)
		for _, label := range resp.Msg.Data {
			seenUIDs[label.Uid] = true
		}
		if !seenUIDs[adminPublicLabelUID] {
			t.Error("User2 should see public label")
		}
		if !seenUIDs[user1GroupLabelUID] {
			t.Error("User2 should see user1's group label (same group)")
		}
		// user2ForUser3LabelUID is actually user2's own label now
		if seenUIDs[user1PrivateLabelUID] {
			t.Error("User2 should not see user1's private label")
		}
		if seenUIDs[user1SelfOnlyLabelUID] {
			t.Error("User2 should not see user1's self-only label")
		}
	})

	t.Run("User3: List labels (should see 1: public only)", func(t *testing.T) {
		resp, err := user3LabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}
		if len(resp.Msg.Data) != 1 {
			t.Errorf("Expected 1 label, got %d", len(resp.Msg.Data))
		}

		// Verify user3 sees correct labels
		seenUIDs := make(map[string]bool)
		for _, label := range resp.Msg.Data {
			seenUIDs[label.Uid] = true
		}
		if !seenUIDs[adminPublicLabelUID] {
			t.Error("User3 should see public label")
		}
		// user2ForUser3LabelUID is not visible to user3 as user2 can only share with own groups
		if seenUIDs[user1PrivateLabelUID] {
			t.Error("User3 should not see user1's private label")
		}
		if seenUIDs[user1GroupLabelUID] {
			t.Error("User3 should not see user1's group label (not in group)")
		}
		if seenUIDs[user1SelfOnlyLabelUID] {
			t.Error("User3 should not see user1's self-only label")
		}
		if seenUIDs[user2ForUser3LabelUID] {
			t.Error("User3 should not see user2's label (not in user2's groups)")
		}
	})

	// Test Get operations with different users
	t.Run("User1: Get own private label (should succeed)", func(t *testing.T) {
		resp, err := user1LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user1PrivateLabelUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get own label:", err)
		}
		if resp.Msg.Data.Uid != user1PrivateLabelUID {
			t.Error("Got wrong label")
		}
		if resp.Msg.Data.OwnerName != user1.Name {
			t.Errorf("Expected owner %s, got %s", user1.Name, resp.Msg.Data.OwnerName)
		}
	})

	t.Run("User2: Get user1's group label (should succeed - same group)", func(t *testing.T) {
		resp, err := user2LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user1GroupLabelUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get group label:", err)
		}
		if resp.Msg.Data.Uid != user1GroupLabelUID {
			t.Error("Got wrong label")
		}
	})

	t.Run("User2: Get user1's private label (should fail - permission denied)", func(t *testing.T) {
		_, err := user2LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user1PrivateLabelUID,
			}),
		)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "Permission denied") {
			t.Errorf("Expected permission denied error, got: %v", err)
		}
	})

	t.Run("User3: Get user2's label (should fail - not in user2's groups)", func(t *testing.T) {
		_, err := user3LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user2ForUser3LabelUID,
			}),
		)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "Permission denied") && !strings.Contains(err.Error(), "not found") {
			t.Errorf("Expected permission denied error, got: %v", err)
		}
	})

	t.Run("User3: Get user1's group label (should fail - not in group)", func(t *testing.T) {
		_, err := user3LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user1GroupLabelUID,
			}),
		)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if !strings.Contains(err.Error(), "permission denied") && !strings.Contains(err.Error(), "Permission denied") {
			t.Errorf("Expected permission denied error, got: %v", err)
		}
	})

	t.Run("Admin: Get any label (should always succeed)", func(t *testing.T) {
		for _, uid := range []string{adminPublicLabelUID, user1PrivateLabelUID, user1GroupLabelUID, user1SelfOnlyLabelUID} {
			resp, err := adminLabelClient.Get(
				context.Background(),
				connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
					LabelUid: uid,
				}),
			)
			if err != nil {
				t.Fatalf("Admin failed to get label %s: %v", uid, err)
			}
			if resp.Msg.Data.Uid != uid {
				t.Errorf("Got wrong label, expected %s, got %s", uid, resp.Msg.Data.Uid)
			}
		}
	})
}

// Test_label_regular_user_create_restrictions tests that regular users without CreateAll permission
// can only create labels with version=0 and proper visibility
func Test_label_regular_user_create_restrictions(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create test user
	userPass := "user-pass"
	user := aquariumv2.User{
		Name:     "testuser",
		Password: &userPass,
		Roles:    []string{"User"},
	}

	// Create clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	userCli, userOpts := h.NewRPCClient(user.Name, userPass, h.RPCClientREST, afi.GetCA(t))

	adminUserClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	userLabelClient := aquariumv2connect.NewLabelServiceClient(userCli, afi.APIAddress("grpc"), userOpts...)

	t.Run("Admin: Create test user", func(t *testing.T) {
		_, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: &user}),
		)
		if err != nil {
			t.Fatal("Failed to create user:", err)
		}
	})

	t.Run("Admin: Create user group", func(t *testing.T) {
		_, err := adminUserClient.CreateGroup(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateGroupRequest{
				Usergroup: &aquariumv2.UserGroup{
					Name:  "test-group",
					Users: []string{user.Name},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create user group:", err)
		}
	})

	// Test: Regular user cannot create versioned label
	t.Run("User: Cannot create versioned label (version != 0)", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		_, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-versioned",
					Version:    1, // Non-zero version
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when creating versioned label")
		}
		if !strings.Contains(err.Error(), "version=0") {
			t.Errorf("Expected version=0 error, got: %v", err)
		}
	})

	// Test: Regular user cannot create label without remove_at
	t.Run("User: Cannot create label without remove_at", func(t *testing.T) {
		_, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-no-removeat",
					Version:    0,
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when creating label without remove_at")
		}
		if !strings.Contains(err.Error(), "remove_at") {
			t.Errorf("Expected remove_at error, got: %v", err)
		}
	})

	// Test: Regular user cannot create label with empty visible_for (world-visible)
	t.Run("User: Cannot create label with empty visible_for", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		_, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-world-visible",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{}, // Empty means world-visible
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when creating label with empty visible_for")
		}
		if !strings.Contains(err.Error(), "visibility") {
			t.Errorf("Expected visibility error, got: %v", err)
		}
	})

	// Test: Regular user cannot create label visible to group they don't belong to
	t.Run("User: Cannot create label visible to non-member group", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		_, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-other-group",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{"other-group"}, // User not in this group
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when creating label visible to non-member group")
		}
		if !strings.Contains(err.Error(), "other-group") {
			t.Errorf("Expected group visibility error, got: %v", err)
		}
	})

	// Test: Regular user CAN create valid temporary label visible to themselves
	t.Run("User: Can create valid temporary label visible to self", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		resp, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-valid-self",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create valid label:", err)
		}
		if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
			t.Error("Invalid label UID")
		}
		if resp.Msg.Data.OwnerName != user.Name {
			t.Errorf("Expected owner %s, got %s", user.Name, resp.Msg.Data.OwnerName)
		}
	})

	// Test: Regular user CAN create valid temporary label visible to their group
	t.Run("User: Can create valid temporary label visible to group", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		resp, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-valid-group",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{"test-group"},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create group-visible label:", err)
		}
		if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
			t.Error("Invalid label UID")
		}
	})

	// Test: Regular user CAN create label visible to both self and group
	t.Run("User: Can create label visible to self and group", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		resp, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-valid-multi",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name, "test-group"},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create multi-visible label:", err)
		}
		if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
			t.Error("Invalid label UID")
		}
	})

	// Test: Admin CAN create versioned label (with CreateAll permission)
	t.Run("Admin: Can create versioned label without restrictions", func(t *testing.T) {
		resp, err := adminLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "admin-versioned",
					Version: 1,
					// No remove_at, no visible_for - all allowed for admin
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create admin label:", err)
		}
		if resp.Msg.Data.Version != 1 {
			t.Errorf("Expected version 1, got %d", resp.Msg.Data.Version)
		}
	})
}

// Test_label_remove_at_validation tests remove_at field validation
func Test_label_remove_at_validation(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

label_remove_at_max: 48h

drivers:
  gates: {}
  providers:
    test:`)

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create test user
	userPass := "user-pass"
	user := aquariumv2.User{
		Name:     "testuser",
		Password: &userPass,
		Roles:    []string{"User"},
	}

	// Create clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	userCli, userOpts := h.NewRPCClient(user.Name, userPass, h.RPCClientREST, afi.GetCA(t))

	adminUserClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	userLabelClient := aquariumv2connect.NewLabelServiceClient(userCli, afi.APIAddress("grpc"), userOpts...)

	t.Run("Admin: Create test user", func(t *testing.T) {
		_, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: &user}),
		)
		if err != nil {
			t.Fatal("Failed to create user:", err)
		}
	})

	// Test: remove_at cannot be set on versioned labels
	t.Run("Admin: Cannot set remove_at on versioned label", func(t *testing.T) {
		removeAt := time.Now().Add(1 * time.Hour)
		_, err := adminLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:     "test-versioned-removeat",
					Version:  1,
					RemoveAt: timestamppb.New(removeAt), // Should not be allowed on versioned label
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when setting remove_at on versioned label")
		}
		if !strings.Contains(err.Error(), "remove_at") && !strings.Contains(err.Error(), "non-editable") {
			t.Errorf("Expected remove_at validation error, got: %v", err)
		}
	})

	// Test: remove_at must be at least 30 seconds in the future
	t.Run("User: Cannot create label with remove_at less than 30 seconds", func(t *testing.T) {
		removeAt := time.Now().Add(10 * time.Second) // Too soon
		_, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-short-removeat",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when remove_at is less than 30 seconds")
		}
		if !strings.Contains(err.Error(), "30 seconds") && !strings.Contains(err.Error(), "below") {
			t.Errorf("Expected 30 seconds validation error, got: %v", err)
		}
	})

	// Test: remove_at cannot exceed configured maximum (48h in this test)
	t.Run("User: Cannot create label with remove_at exceeding maximum", func(t *testing.T) {
		removeAt := time.Now().Add(49 * time.Hour) // Exceeds 48h limit
		_, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-long-removeat",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when remove_at exceeds maximum")
		}
		if !strings.Contains(err.Error(), "longer than duration limit") && !strings.Contains(err.Error(), "duration") {
			t.Errorf("Expected duration limit error, got: %v", err)
		}
	})

	// Test: Valid remove_at within range (30s to 48h)
	t.Run("User: Can create label with valid remove_at", func(t *testing.T) {
		removeAt := time.Now().Add(24 * time.Hour) // Within 48h limit
		resp, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-valid-removeat",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label with valid remove_at:", err)
		}
		if resp.Msg.Data.RemoveAt == nil {
			t.Error("Expected remove_at to be set")
		}
	})

	// Test: Minimum valid remove_at (just over 30 seconds)
	t.Run("User: Can create label with minimum valid remove_at", func(t *testing.T) {
		removeAt := time.Now().Add(35 * time.Second) // Just over 30s
		resp, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-min-removeat",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label with minimum remove_at:", err)
		}
		if resp.Msg.Data.RemoveAt == nil {
			t.Error("Expected remove_at to be set")
		}
	})

	// Test: Maximum valid remove_at (just under 48h)
	t.Run("User: Can create label with maximum valid remove_at", func(t *testing.T) {
		removeAt := time.Now().Add(47*time.Hour + 59*time.Minute) // Just under 48h
		resp, err := userLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "test-max-removeat",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label with maximum remove_at:", err)
		}
		if resp.Msg.Data.RemoveAt == nil {
			t.Error("Expected remove_at to be set")
		}
	})
}

// Test_label_update_restrictions tests label update restrictions
func Test_label_update_restrictions(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create test users
	user1Pass := "user1-pass"
	user2Pass := "user2-pass"
	user1 := aquariumv2.User{
		Name:     "user1",
		Password: &user1Pass,
		Roles:    []string{"User"},
	}
	user2 := aquariumv2.User{
		Name:     "user2",
		Password: &user2Pass,
		Roles:    []string{"User"},
	}

	// Create clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	user1Cli, user1Opts := h.NewRPCClient(user1.Name, user1Pass, h.RPCClientREST, afi.GetCA(t))
	user2Cli, user2Opts := h.NewRPCClient(user2.Name, user2Pass, h.RPCClientREST, afi.GetCA(t))

	adminUserClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	user1LabelClient := aquariumv2connect.NewLabelServiceClient(user1Cli, afi.APIAddress("grpc"), user1Opts...)
	user2LabelClient := aquariumv2connect.NewLabelServiceClient(user2Cli, afi.APIAddress("grpc"), user2Opts...)

	t.Run("Admin: Create test users", func(t *testing.T) {
		for _, user := range []*aquariumv2.User{&user1, &user2} {
			_, err := adminUserClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
			)
			if err != nil {
				t.Fatalf("Failed to create user %s: %v", user.Name, err)
			}
		}
	})

	// Create editable and non-editable labels
	var (
		user1EditableLabelUID    string
		user1NonEditableLabelUID string
	)

	t.Run("User1: Create editable label (version=0)", func(t *testing.T) {
		removeAt := time.Now().Add(2 * time.Hour)
		resp, err := user1LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user1-editable",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create editable label:", err)
		}
		user1EditableLabelUID = resp.Msg.Data.Uid
	})

	t.Run("Admin: Create non-editable label (version=1)", func(t *testing.T) {
		resp, err := adminLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "admin-non-editable",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create non-editable label:", err)
		}
		user1NonEditableLabelUID = resp.Msg.Data.Uid
	})

	// Test: User can update their own editable label
	t.Run("User1: Can update own editable label", func(t *testing.T) {
		removeAt := time.Now().Add(3 * time.Hour)
		resp, err := user1LabelClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceUpdateRequest{
				Label: &aquariumv2.Label{
					Uid:        user1EditableLabelUID,
					Name:       "user1-editable",
					OwnerName:  user1.Name,
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 2, // Changed
							Ram: 4, // Changed
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to update own label:", err)
		}
		if resp.Msg.Data.Definitions[0].Resources.Cpu != 2 {
			t.Error("Label was not updated")
		}
	})

	// Test: User cannot update non-editable label (version != 0) - should fail because not owner
	t.Run("User1: Cannot update admin's non-editable label", func(t *testing.T) {
		_, err := user1LabelClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceUpdateRequest{
				Label: &aquariumv2.Label{
					Uid:     user1NonEditableLabelUID,
					Name:    "admin-non-editable",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 2,
							Ram: 4,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when updating non-editable label")
		}
		if !strings.Contains(err.Error(), "Permission") && !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "version") && !strings.Contains(err.Error(), "Version") {
			t.Errorf("Expected permission or version error, got: %v", err)
		}
	})

	// Test: User cannot update another user's label
	t.Run("User2: Cannot update user1's label", func(t *testing.T) {
		removeAt := time.Now().Add(3 * time.Hour)
		_, err := user2LabelClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceUpdateRequest{
				Label: &aquariumv2.Label{
					Uid:        user1EditableLabelUID,
					Name:       "user1-editable",
					OwnerName:  user1.Name,
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 2,
							Ram: 4,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "Permission") {
			t.Errorf("Expected permission denied error, got: %v", err)
		}
	})

	// Test: User cannot change label name
	t.Run("User1: Cannot change label name", func(t *testing.T) {
		removeAt := time.Now().Add(3 * time.Hour)
		_, err := user1LabelClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceUpdateRequest{
				Label: &aquariumv2.Label{
					Uid:        user1EditableLabelUID,
					Name:       "different-name", // Changed name
					OwnerName:  user1.Name,
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 2,
							Ram: 4,
						},
					}},
				},
			}),
		)
		if err == nil {
			t.Error("Expected error when changing label name")
		}
		if !strings.Contains(err.Error(), "name") && !strings.Contains(err.Error(), "Name") {
			t.Errorf("Expected name change error, got: %v", err)
		}
	})

	// Test: Admin can update any label with UpdateAll permission
	t.Run("Admin: Can update user1's label", func(t *testing.T) {
		removeAt := time.Now().Add(4 * time.Hour)
		resp, err := adminLabelClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceUpdateRequest{
				Label: &aquariumv2.Label{
					Uid:        user1EditableLabelUID,
					Name:       "user1-editable",
					OwnerName:  user1.Name,
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 4, // Changed by admin
							Ram: 8, // Changed by admin
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to update label as admin:", err)
		}
		if resp.Msg.Data.Definitions[0].Resources.Cpu != 4 {
			t.Error("Label was not updated by admin")
		}
	})
}

// Test_label_remove_operations tests label removal with different permission levels
func Test_label_remove_operations(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create test users
	user1Pass := "user1-pass"
	user2Pass := "user2-pass"
	user1 := aquariumv2.User{
		Name:     "user1",
		Password: &user1Pass,
		Roles:    []string{"User"},
	}
	user2 := aquariumv2.User{
		Name:     "user2",
		Password: &user2Pass,
		Roles:    []string{"User"},
	}

	// Create clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	user1Cli, user1Opts := h.NewRPCClient(user1.Name, user1Pass, h.RPCClientREST, afi.GetCA(t))
	user2Cli, user2Opts := h.NewRPCClient(user2.Name, user2Pass, h.RPCClientREST, afi.GetCA(t))

	adminUserClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	user1LabelClient := aquariumv2connect.NewLabelServiceClient(user1Cli, afi.APIAddress("grpc"), user1Opts...)
	user2LabelClient := aquariumv2connect.NewLabelServiceClient(user2Cli, afi.APIAddress("grpc"), user2Opts...)

	t.Run("Admin: Create test users", func(t *testing.T) {
		for _, user := range []*aquariumv2.User{&user1, &user2} {
			_, err := adminUserClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: user}),
			)
			if err != nil {
				t.Fatalf("Failed to create user %s: %v", user.Name, err)
			}
		}
	})

	// Create labels for removal tests
	var (
		user1LabelUID string
		user2LabelUID string
	)

	t.Run("User1: Create label", func(t *testing.T) {
		removeAt := time.Now().Add(2 * time.Hour)
		resp, err := user1LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user1-label",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user1.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create user1 label:", err)
		}
		user1LabelUID = resp.Msg.Data.Uid
	})

	t.Run("User2: Create label", func(t *testing.T) {
		removeAt := time.Now().Add(2 * time.Hour)
		resp, err := user2LabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:       "user2-label",
					Version:    0,
					RemoveAt:   timestamppb.New(removeAt),
					VisibleFor: []string{user2.Name},
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create user2 label:", err)
		}
		user2LabelUID = resp.Msg.Data.Uid
	})

	// Test: User can remove their own label
	t.Run("User1: Can remove own label", func(t *testing.T) {
		resp, err := user1LabelClient.Remove(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceRemoveRequest{
				LabelUid: user1LabelUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to remove own label:", err)
		}
		if !resp.Msg.Status {
			t.Error("Expected successful removal")
		}

		// Verify label is gone
		_, err = user1LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user1LabelUID,
			}),
		)
		if err == nil {
			t.Error("Expected label to be removed")
		}
	})

	// Test: User cannot remove another user's label
	t.Run("User1: Cannot remove user2's label", func(t *testing.T) {
		_, err := user1LabelClient.Remove(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceRemoveRequest{
				LabelUid: user2LabelUID,
			}),
		)
		if err == nil {
			t.Error("Expected permission denied error")
		}
		if !strings.Contains(err.Error(), "permission") && !strings.Contains(err.Error(), "Permission") && !strings.Contains(err.Error(), "allowed") {
			t.Errorf("Expected permission error, got: %v", err)
		}

		// Verify label still exists
		resp, err := user2LabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user2LabelUID,
			}),
		)
		if err != nil {
			t.Error("Expected label to still exist:", err)
		}
		if resp.Msg.Data.Uid != user2LabelUID {
			t.Error("Label was incorrectly removed")
		}
	})

	// Test: Admin can remove any label
	t.Run("Admin: Can remove any user's label", func(t *testing.T) {
		resp, err := adminLabelClient.Remove(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceRemoveRequest{
				LabelUid: user2LabelUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to remove label as admin:", err)
		}
		if !resp.Msg.Status {
			t.Error("Expected successful removal")
		}

		// Verify label is gone
		_, err = adminLabelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: user2LabelUID,
			}),
		)
		if err == nil {
			t.Error("Expected label to be removed")
		}
	})
}
