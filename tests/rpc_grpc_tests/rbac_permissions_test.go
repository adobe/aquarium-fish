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

// Test_connect_rbac_permissions_grpc verifies that:
// 1. A new user without any role cannot access any resources
// 2. A user with User role can:
//   - List labels
//   - Create and manage their own applications
//   - Cannot see or manage other users' applications
//
// 3. A user with Administrator role can access and manage all resources
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
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC)
	regularCli, regularOpts := h.NewRPCClient(regularUser.Name, regularUserPass, h.RPCClientGRPC)
	powerCli, powerOpts := h.NewRPCClient(powerUser.Name, powerUserPass, h.RPCClientGRPC)

	// Create service clients for admin
	adminUserClient := aquariumv2connect.NewUserServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	adminAppClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	// Create service clients for regular user
	regularUserClient := aquariumv2connect.NewUserServiceClient(
		regularCli,
		afi.APIAddress("grpc"),
		regularOpts...,
	)
	regularLabelClient := aquariumv2connect.NewLabelServiceClient(
		regularCli,
		afi.APIAddress("grpc"),
		regularOpts...,
	)
	regularAppClient := aquariumv2connect.NewApplicationServiceClient(
		regularCli,
		afi.APIAddress("grpc"),
		regularOpts...,
	)

	// Create service clients for power user
	powerUserClient := aquariumv2connect.NewUserServiceClient(
		powerCli,
		afi.APIAddress("grpc"),
		powerOpts...,
	)
	powerLabelClient := aquariumv2connect.NewLabelServiceClient(
		powerCli,
		afi.APIAddress("grpc"),
		powerOpts...,
	)
	powerAppClient := aquariumv2connect.NewApplicationServiceClient(
		powerCli,
		afi.APIAddress("grpc"),
		powerOpts...,
	)

	t.Run("Admin: Create test users", func(t *testing.T) {
		// Create regular user
		_, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: &regularUser}),
		)
		if err != nil {
			t.Fatal("Failed to create regular user:", err)
		}

		// Create power user
		_, err = adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{User: &powerUser}),
		)
		if err != nil {
			t.Fatal("Failed to create power user:", err)
		}
	})

	t.Run("Regular user without role: Can access small subset of RPC", func(t *testing.T) {
		// Try to get own user info
		resp, err := regularUserClient.GetMe(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to get user info:", err)
		}

		if resp.Msg.Data.Name != regularUser.Name {
			t.Error("Expected to see myself:", resp.Msg.Data.Name, "!=", regularUser.Name)
		}
	})
	t.Run("Regular user without role: Access denied", func(t *testing.T) {
		// Try to list labels
		_, err := regularLabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err == nil {
			t.Error("Expected access denied for label list")
		}

		// Try to create label
		_, err = regularLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
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
				},
			}),
		)
		if err == nil {
			t.Error("Expected access denied for label create")
		}

		// Try to list applications
		_, err = regularAppClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceListRequest{}),
		)
		if err == nil {
			t.Error("Expected access denied for application list")
		}
	})

	// Assign User role to regular user
	t.Run("Admin: Assign User role to regular user", func(t *testing.T) {
		_, err := adminUserClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceUpdateRequest{
				User: &aquariumv2.User{
					Name:  regularUser.Name,
					Roles: []string{"User"},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to assign role:", err)
		}

		// Try to get own user info
		resp, err := regularUserClient.GetMe(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to get user info:", err)
		}

		if resp.Msg.Data.Name != regularUser.Name {
			t.Error("Expected to see myself:", resp.Msg.Data.Name, "!=", regularUser.Name)
		}
	})

	// Create a test label
	var labelUID string
	t.Run("Admin: Create test label", func(t *testing.T) {
		resp, err := adminLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
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
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	// Test regular user with User role
	var regularUserAppUID string
	t.Run("Regular user with User role: Basic operations", func(t *testing.T) {
		// Should be able to list labels
		_, err := regularLabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err != nil {
			t.Error("Expected to be able to list labels:", err)
		}

		// Should be able to create application
		resp, err := regularAppClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Expected to be able to create application:", err)
		}
		regularUserAppUID = resp.Msg.Data.Uid

		// Should be able to view own application
		_, err = regularAppClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetRequest{
				ApplicationUid: regularUserAppUID,
			}),
		)
		if err != nil {
			t.Error("Expected to be able to view own application:", err)
		}
	})

	// Create another application as admin
	var adminAppUID string
	t.Run("Admin: Create another application", func(t *testing.T) {
		resp, err := adminAppClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create admin application:", err)
		}
		adminAppUID = resp.Msg.Data.Uid
	})

	t.Run("Regular user: Cannot access admin's application", func(t *testing.T) {
		// Should NOT be able to view admin's application
		_, err := regularAppClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetRequest{
				ApplicationUid: adminAppUID,
			}),
		)
		if err == nil {
			t.Error("Expected to be denied access to admin's application")
		}
	})

	// Assign Administrator role to power user
	t.Run("Admin: Assign Administrator role to power user", func(t *testing.T) {
		_, err := adminUserClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceUpdateRequest{
				User: &aquariumv2.User{
					Name:  powerUser.Name,
					Roles: []string{"Administrator"},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to assign role:", err)
		}
	})

	t.Run("Power user with Administrator role: Full access", func(t *testing.T) {
		// Should be able to list users
		_, err := powerUserClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceListRequest{}),
		)
		if err != nil {
			t.Error("Expected to be able to list users:", err)
		}

		// Should be able to create labels
		_, err = powerLabelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
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
				},
			}),
		)
		if err != nil {
			t.Error("Expected to be able to create labels:", err)
		}

		// Should be able to view regular user's application
		_, err = powerAppClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetRequest{
				ApplicationUid: regularUserAppUID,
			}),
		)
		if err != nil {
			t.Error("Expected to be able to view regular user's application:", err)
		}

		// Should be able to view admin's application
		_, err = powerAppClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetRequest{
				ApplicationUid: adminAppUID,
			}),
		)
		if err != nil {
			t.Error("Expected to be able to view admin's application:", err)
		}
	})

	// Test application cleanup
	t.Run("Cleanup: Deallocate applications", func(t *testing.T) {
		// Regular user deallocates their application
		_, err := regularAppClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: regularUserAppUID,
			}),
		)
		if err != nil {
			t.Error("Failed to deallocate regular user's application:", err)
		}

		// Admin deallocates their application
		_, err = adminAppClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: adminAppUID,
			}),
		)
		if err != nil {
			t.Error("Failed to deallocate admin's application:", err)
		}
	})
}
