/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Ensure application task interface snapshot for user is working
func Test_application_task_snapshot_by_user(t *testing.T) {
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

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients for admin
	adminLabelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	adminUserClient := aquariumv2connect.NewUserServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Admin: Create Label", func(t *testing.T) {
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

		if labelUID == "" || labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	t.Run("Admin: Create User", func(t *testing.T) {
		userPassword := "test-user-password"
		_, err := adminUserClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceCreateRequest{
				User: &aquariumv2.User{
					Name:     "test-user",
					Password: &userPassword,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create user:", err)
		}
	})

	// Create user client
	userCli, userOpts := h.NewRPCClient("test-user", "test-user-password", h.RPCClientREST, afi.GetCA(t))

	userLabelClient := aquariumv2connect.NewLabelServiceClient(
		userCli,
		afi.APIAddress("grpc"),
		userOpts...,
	)
	userAppClient := aquariumv2connect.NewApplicationServiceClient(
		userCli,
		afi.APIAddress("grpc"),
		userOpts...,
	)

	// User-side of requests
	t.Run("User: List Label with name test-label should not be allowed by Auth", func(t *testing.T) {
		_, err := userLabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{
				Name: &[]string{"test-label"}[0],
			}),
		)
		if err == nil {
			t.Error("Expected access denied for label list")
		}
	})

	t.Run("User: Create Application should not be allowed by Auth", func(t *testing.T) {
		_, err := userAppClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err == nil {
			t.Error("Expected access denied for application create")
		}
	})

	t.Run("Admin: Put User role in place", func(t *testing.T) {
		_, err := adminUserClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceUpdateRequest{
				User: &aquariumv2.User{
					Name:  "test-user",
					Roles: []string{"User"},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to assign role:", err)
		}
	})

	t.Run("User: List Label with name test-label", func(t *testing.T) {
		resp, err := userLabelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{
				Name: &[]string{"test-label"}[0],
			}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}

		if len(resp.Msg.Data) != 1 || resp.Msg.Data[0].Uid == "" || resp.Msg.Data[0].Uid == uuid.Nil.String() {
			t.Fatalf("Label is incorrect")
		}
	})

	var appUID string
	t.Run("User: Create Application", func(t *testing.T) {
		resp, err := userAppClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == "" || appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("User: Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := userAppClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	t.Run("User: Resource should be created", func(t *testing.T) {
		resp, err := userAppClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	t.Run("User: Create ApplicationTask Snapshot should not be allowed by Auth", func(t *testing.T) {
		_, err := userAppClient.CreateTask(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateTaskRequest{
				Task: &aquariumv2.ApplicationTask{
					ApplicationUid: appUID,
					Task:           "snapshot",
					When:           aquariumv2.ApplicationState_ALLOCATED,
				},
			}),
		)
		if err == nil {
			t.Error("Expected access denied for task create")
		}
	})

	t.Run("Admin: Put Administrator role in place", func(t *testing.T) {
		_, err := adminUserClient.Update(
			context.Background(),
			connect.NewRequest(&aquariumv2.UserServiceUpdateRequest{
				User: &aquariumv2.User{
					Name:  "test-user",
					Roles: []string{"Administrator"},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to assign administrator role:", err)
		}
	})

	var taskUID string
	t.Run("User: Create ApplicationTask Snapshot should work with Administrator role", func(t *testing.T) {
		resp, err := userAppClient.CreateTask(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateTaskRequest{
				Task: &aquariumv2.ApplicationTask{
					ApplicationUid: appUID,
					Task:           "snapshot",
					When:           aquariumv2.ApplicationState_ALLOCATED,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create task:", err)
		}
		taskUID = resp.Msg.Data.Uid

		if taskUID == "" || taskUID == uuid.Nil.String() {
			t.Fatalf("Task UID is incorrect: %v", taskUID)
		}
	})

	t.Run("User: List ApplicationTask", func(t *testing.T) {
		resp, err := userAppClient.ListTask(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceListTaskRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to list tasks:", err)
		}

		if len(resp.Msg.Data) != 1 {
			t.Fatalf("Task list length is incorrect: %d != 1", len(resp.Msg.Data))
		}

		if resp.Msg.Data[0].Uid != taskUID {
			t.Fatalf("Task UID is incorrect: %s != %s", resp.Msg.Data[0].Uid, taskUID)
		}
	})

	t.Run("User: Get ApplicationTask", func(t *testing.T) {
		resp, err := userAppClient.GetTask(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetTaskRequest{
				ApplicationTaskUid: taskUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get task:", err)
		}

		if resp.Msg.Data.Uid != taskUID {
			t.Fatalf("Task UID is incorrect: %s != %s", resp.Msg.Data.Uid, taskUID)
		}
	})

	t.Run("User: ApplicationTask should have expected results in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := userAppClient.GetTask(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetTaskRequest{
					ApplicationTaskUid: taskUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get task:", err)
			}

			if resp.Msg.Data.Result == nil {
				r.Fatalf("Task result is not ready yet")
			}

			if !resp.Msg.GetStatus() {
				r.Fatalf("Task result status is incorrect: %v", resp.Msg)
			}

			if resp.Msg.GetData().GetResult().GetFields()["snapshots"].String() != `list_value:{values:{string_value:"test-snapshot"}}` {
				r.Fatalf("Task result snapshots are incorrect: %s", resp.Msg.GetData().GetResult().GetFields()["snapshots"].String())
			}
		})
	})

	// Create admin app client for cleanup
	adminAppClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := adminAppClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := adminAppClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}
