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
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_rbac_permissions_subscription verifies RBAC permissions for streaming subscriptions
// This test ensures that:
// 1. Users only receive subscription notifications for applications they own
// 2. Users with "All" permissions receive notifications for all applications
// 3. Permission cache works correctly and prevents unauthorized access
// 4. Multiple users can have separate subscription streams with proper filtering
func Test_rbac_permissions_subscription(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	// Create test users
	user1Pass := "user1-pass"
	user2Pass := "user2-pass"
	adminUserPass := "admin-user-pass"

	user1 := aquariumv2.User{
		Name:     "test-user-1",
		Password: &user1Pass,
	}
	user2 := aquariumv2.User{
		Name:     "test-user-2",
		Password: &user2Pass,
	}
	adminUser := aquariumv2.User{
		Name:     "admin-user",
		Password: &adminUserPass,
	}

	// Create clients for different users
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC, afi.GetCA(t))
	user1Cli, user1Opts := h.NewRPCClient(user1.Name, user1Pass, h.RPCClientGRPC, afi.GetCA(t))
	user2Cli, user2Opts := h.NewRPCClient(user2.Name, user2Pass, h.RPCClientGRPC, afi.GetCA(t))
	adminUserCli, adminUserOpts := h.NewRPCClient(adminUser.Name, adminUserPass, h.RPCClientGRPC, afi.GetCA(t))

	// Create streaming service clients
	adminStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	user1StreamingClient := aquariumv2connect.NewStreamingServiceClient(
		user1Cli,
		afi.APIAddress("grpc"),
		user1Opts...,
	)
	user2StreamingClient := aquariumv2connect.NewStreamingServiceClient(
		user2Cli,
		afi.APIAddress("grpc"),
		user2Opts...,
	)
	adminUserStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		adminUserCli,
		afi.APIAddress("grpc"),
		adminUserOpts...,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Create streaming helpers but don't setup streaming yet (users don't exist)
	adminHelper := h.NewStreamingTestHelper(ctx, t, "admin", adminStreamingClient)
	defer adminHelper.Close()

	user1Helper := h.NewStreamingTestHelper(ctx, t, "user1", user1StreamingClient)
	defer user1Helper.Close()

	user2Helper := h.NewStreamingTestHelper(ctx, t, "user2", user2StreamingClient)
	defer user2Helper.Close()

	adminUserHelper := h.NewStreamingTestHelper(ctx, t, "adminUser", adminUserStreamingClient)
	defer adminUserHelper.Close()

	// Setup streaming for admin (who already exists)
	subscriptionTypes := []aquariumv2.SubscriptionType{
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE,
	}

	if err := adminHelper.SetupFullStreaming(subscriptionTypes); err != nil {
		t.Fatalf("Failed to setup admin streaming: %v", err)
	}

	// Create test users through admin
	t.Run("Admin: Create test users", func(t *testing.T) {
		// Create test users
		users := []*aquariumv2.User{&user1, &user2, &adminUser}
		for _, user := range users {
			userReq := &aquariumv2.UserServiceCreateRequest{User: user}
			_, err := adminHelper.SendRequestAndExpectSuccess(
				fmt.Sprintf("create-user-%s", user.Name),
				"UserServiceCreateRequest",
				userReq,
				"UserServiceCreateResponse",
			)
			if err != nil {
				t.Fatalf("Failed to create user %s: %v", user.Name, err)
			}
		}
	})

	// Assign roles
	t.Run("Admin: Assign roles", func(t *testing.T) {
		// Assign User role to user1 and user2
		for _, userName := range []string{user1.Name, user2.Name} {
			userUpdateReq := &aquariumv2.UserServiceUpdateRequest{
				User: &aquariumv2.User{
					Name:  userName,
					Roles: []string{"User"},
				},
			}
			_, err := adminHelper.SendRequestAndExpectSuccess(
				fmt.Sprintf("assign-user-role-%s", userName),
				"UserServiceUpdateRequest",
				userUpdateReq,
				"UserServiceUpdateResponse",
			)
			if err != nil {
				t.Fatalf("Failed to assign User role to %s: %v", userName, err)
			}
		}

		// Assign Administrator role to adminUser
		adminUpdateReq := &aquariumv2.UserServiceUpdateRequest{
			User: &aquariumv2.User{
				Name:  adminUser.Name,
				Roles: []string{"Administrator"},
			},
		}
		_, err := adminHelper.SendRequestAndExpectSuccess(
			"assign-admin-role",
			"UserServiceUpdateRequest",
			adminUpdateReq,
			"UserServiceUpdateResponse",
		)
		if err != nil {
			t.Fatalf("Failed to assign Administrator role to %s: %v", adminUser.Name, err)
		}
	})

	// Setup streaming for newly created users (now that they exist and have roles)
	t.Run("Setup streaming for all users", func(t *testing.T) {
		// Setup streaming for user1
		if err := user1Helper.SetupFullStreaming(subscriptionTypes); err != nil {
			t.Fatalf("Failed to setup user1 streaming: %v", err)
		}

		// Setup streaming for user2
		if err := user2Helper.SetupFullStreaming(subscriptionTypes); err != nil {
			t.Fatalf("Failed to setup user2 streaming: %v", err)
		}

		// Setup streaming for adminUser
		if err := adminUserHelper.SetupFullStreaming(subscriptionTypes); err != nil {
			t.Fatalf("Failed to setup adminUser streaming: %v", err)
		}
	})

	// Create a test label
	var labelUID string
	t.Run("Admin: Create test label", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"test": "rbac-subscription"})
		labelCreateReq := &aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "rbac-test-label",
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
			"create-test-label",
			"LabelServiceCreateRequest",
			labelCreateReq,
			"LabelServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create test label:", err)
		}

		var labelResp aquariumv2.LabelServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&labelResp); err != nil {
			t.Fatal("Failed to unmarshal label response:", err)
		}
		labelUID = labelResp.Data.Uid
	})

	// Track received notifications for verification
	type NotificationRecord struct {
		UserName string
		AppUID   string
		Type     string
	}

	var mu sync.Mutex
	notifications := make([]NotificationRecord, 0)

	// Helper function to collect notifications
	collectNotifications := func(userName string, helper *h.StreamingTestHelper, collectCtx context.Context, wg *sync.WaitGroup) {
		defer wg.Done()

		for {
			select {
			case <-collectCtx.Done():
				t.Logf("Notification collection timeout for user %s", userName)
				return

			case stateNotification := <-helper.GetStreamingClient().GetStateNotifications():
				// Parse the state notification to get the application UID
				var appState aquariumv2.ApplicationState
				if err := stateNotification.ObjectData.UnmarshalTo(&appState); err != nil {
					t.Logf("Failed to unmarshal state notification: %v", err)
					continue
				}

				mu.Lock()
				notifications = append(notifications, NotificationRecord{
					UserName: userName,
					AppUID:   appState.ApplicationUid,
					Type:     "STATE",
				})
				mu.Unlock()
				t.Logf("User %s received STATE notification for app %s", userName, appState.ApplicationUid)

			case resourceNotification := <-helper.GetStreamingClient().GetResourceNotifications():
				// Parse the resource notification to get the application UID
				var appResource aquariumv2.ApplicationResource
				if err := resourceNotification.ObjectData.UnmarshalTo(&appResource); err != nil {
					t.Logf("Failed to unmarshal resource notification: %v", err)
					continue
				}

				mu.Lock()
				notifications = append(notifications, NotificationRecord{
					UserName: userName,
					AppUID:   appResource.ApplicationUid,
					Type:     "RESOURCE",
				})
				mu.Unlock()
				t.Logf("User %s received RESOURCE notification for app %s", userName, appResource.ApplicationUid)
			}
		}
	}

	// Create a separate context for notification collection (so we don't cancel main context)
	notificationCtx, notificationCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer notificationCancel()

	// Start collecting notifications from all users
	var wg sync.WaitGroup
	wg.Add(4)
	go collectNotifications("admin", adminHelper, notificationCtx, &wg)
	go collectNotifications("user1", user1Helper, notificationCtx, &wg)
	go collectNotifications("user2", user2Helper, notificationCtx, &wg)
	go collectNotifications("adminUser", adminUserHelper, notificationCtx, &wg)

	// Helper functions to count notifications
	countNotifications := func(userName, appUID string) int {
		count := 0
		mu.Lock()
		defer mu.Unlock()
		for _, notif := range notifications {
			if notif.UserName == userName && notif.AppUID == appUID {
				count++
			}
		}
		return count
	}

	var user1AppUID, user2AppUID, adminAppUID string

	t.Run("Create application as user1", func(t *testing.T) {
		// User1 creates an application
		md1, _ := structpb.NewStruct(map[string]any{"owner": "user1"})
		user1AppReq := &aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{
				LabelUid: labelUID,
				Metadata: md1,
			},
		}
		resp, err := user1Helper.SendRequestAndExpectSuccess(
			"create-user1-app",
			"ApplicationServiceCreateRequest",
			user1AppReq,
			"ApplicationServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create user1 application:", err)
		}

		var appResp aquariumv2.ApplicationServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
			t.Fatal("Failed to unmarshal user1 app response:", err)
		}
		user1AppUID = appResp.Data.Uid
		t.Logf("User1 created application: %s", user1AppUID)

		// Wait a bit for notifications to propagate
		time.Sleep(2 * time.Second)

		user1OwnNotifs := countNotifications("user1", user1AppUID)
		if user1OwnNotifs != 4 {
			t.Errorf("ERROR: User1 should receive notifications for its own application: (%d) %#v", user1OwnNotifs, notifications)
		}
	})

	t.Run("Create application as user2", func(t *testing.T) {
		// User2 creates an application
		md2, _ := structpb.NewStruct(map[string]any{"owner": "user2"})
		user2AppReq := &aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{
				LabelUid: labelUID,
				Metadata: md2,
			},
		}
		resp, err := user2Helper.SendRequestAndExpectSuccess(
			"create-user2-app",
			"ApplicationServiceCreateRequest",
			user2AppReq,
			"ApplicationServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create user2 application:", err)
		}

		var appResp aquariumv2.ApplicationServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
			t.Fatal("Failed to unmarshal user2 app response:", err)
		}
		user2AppUID = appResp.Data.Uid
		t.Logf("User2 created application: %s", user2AppUID)

		// Wait a bit for notifications to propagate
		time.Sleep(2 * time.Second)

		user2OwnNotifs := countNotifications("user2", user2AppUID)
		if user2OwnNotifs != 4 {
			t.Errorf("ERROR: User2 should receive notifications for its own application: (%d) %#v", user2OwnNotifs, notifications)
		}
	})

	t.Run("Create application as admin", func(t *testing.T) {
		// Admin creates an application
		mdAdmin, _ := structpb.NewStruct(map[string]any{"owner": "admin"})
		adminAppReq := &aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{
				LabelUid: labelUID,
				Metadata: mdAdmin,
			},
		}
		resp, err := adminHelper.SendRequestAndExpectSuccess(
			"create-admin-app",
			"ApplicationServiceCreateRequest",
			adminAppReq,
			"ApplicationServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create admin application:", err)
		}

		var appResp aquariumv2.ApplicationServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
			t.Fatal("Failed to unmarshal admin app response:", err)
		}
		adminAppUID = appResp.Data.Uid
		t.Logf("Admin created application: %s", adminAppUID)

		// Wait for notifications to complete
		time.Sleep(5 * time.Second)

		adminOwnNotifs := countNotifications("admin", adminAppUID)
		if adminOwnNotifs != 4 {
			t.Errorf("ERROR: Admin should receive notifications for its own application: (%d) %#v", adminOwnNotifs, notifications)
		}
	})

	// Stop notification collection
	notificationCancel()
	wg.Wait()

	t.Run("Verify overall RBAC subscription filtering", func(t *testing.T) {
		mu.Lock()
		t.Logf("Total notifications received: %d", len(notifications))
		mu.Unlock()

		// Verify user1 only receives notifications for their own application
		user1OwnNotifs := countNotifications("user1", user1AppUID)
		user1User2Notifs := countNotifications("user1", user2AppUID)
		user1AdminNotifs := countNotifications("user1", adminAppUID)

		t.Logf("User1 notifications: own=%d, user2=%d, admin=%d", user1OwnNotifs, user1User2Notifs, user1AdminNotifs)

		if user1OwnNotifs == 0 {
			t.Error("ERROR: User1 should receive notifications for their own application")
		}
		if user1User2Notifs > 0 {
			t.Errorf("ERROR: User1 should NOT receive notifications for user2's application, but got %d", user1User2Notifs)
		}
		if user1AdminNotifs > 0 {
			t.Errorf("ERROR: User1 should NOT receive notifications for admin's application, but got %d", user1AdminNotifs)
		}

		// Verify user2 only receives notifications for their own application
		user2OwnNotifs := countNotifications("user2", user2AppUID)
		user2User1Notifs := countNotifications("user2", user1AppUID)
		user2AdminNotifs := countNotifications("user2", adminAppUID)

		t.Logf("User2 notifications: own=%d, user1=%d, admin=%d", user2OwnNotifs, user2User1Notifs, user2AdminNotifs)

		if user2OwnNotifs == 0 {
			t.Error("ERROR: User2 should receive notifications for their own application")
		}
		if user2User1Notifs > 0 {
			t.Errorf("ERROR: User2 should NOT receive notifications for user1's application, but got %d", user2User1Notifs)
		}
		if user2AdminNotifs > 0 {
			t.Errorf("ERROR: User2 should NOT receive notifications for admin's application, but got %d", user2AdminNotifs)
		}

		// Verify adminUser (with Administrator role) receives notifications for ALL applications
		adminUserUser1Notifs := countNotifications("adminUser", user1AppUID)
		adminUserUser2Notifs := countNotifications("adminUser", user2AppUID)
		adminUserAdminNotifs := countNotifications("adminUser", adminAppUID)

		t.Logf("AdminUser notifications: user1=%d, user2=%d, admin=%d", adminUserUser1Notifs, adminUserUser2Notifs, adminUserAdminNotifs)

		if adminUserUser1Notifs == 0 {
			t.Error("ERROR: AdminUser should receive notifications for user1's application (has Administrator role)")
		}
		if adminUserUser2Notifs == 0 {
			t.Error("ERROR: AdminUser should receive notifications for user2's application (has Administrator role)")
		}
		if adminUserAdminNotifs == 0 {
			t.Error("ERROR: AdminUser should receive notifications for admin's application (has Administrator role)")
		}

		// Verify admin (system admin) receives notifications for ALL applications
		adminUser1Notifs := countNotifications("admin", user1AppUID)
		adminUser2Notifs := countNotifications("admin", user2AppUID)
		adminAdminNotifs := countNotifications("admin", adminAppUID)

		t.Logf("Admin notifications: user1=%d, user2=%d, admin=%d", adminUser1Notifs, adminUser2Notifs, adminAdminNotifs)

		if adminUser1Notifs == 0 {
			t.Error("ERROR: Admin should receive notifications for user1's application (system admin)")
		}
		if adminUser2Notifs == 0 {
			t.Error("ERROR: Admin should receive notifications for user2's application (system admin)")
		}
		if adminAdminNotifs == 0 {
			t.Error("ERROR: Admin should receive notifications for their own application (system admin)")
		}
	})

	t.Run("Cleanup: Deallocate applications", func(t *testing.T) {
		// Deallocate all applications
		applications := []struct {
			uid    string
			helper *h.StreamingTestHelper
			name   string
		}{
			{user1AppUID, user1Helper, "user1-app"},
			{user2AppUID, user2Helper, "user2-app"},
			{adminAppUID, adminHelper, "admin-app"},
		}

		for _, app := range applications {
			deallocateReq := &aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: app.uid,
			}
			_, err := app.helper.SendRequestAndExpectSuccess(
				fmt.Sprintf("deallocate-%s", app.name),
				"ApplicationServiceDeallocateRequest",
				deallocateReq,
				"ApplicationServiceDeallocateResponse",
			)
			if err != nil {
				t.Errorf("Failed to deallocate %s: %v", app.name, err)
			}
		}
	})
}
