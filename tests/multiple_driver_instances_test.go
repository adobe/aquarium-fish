/**
 * Copyright 2024-2025 Adobe. All rights reserved.
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

// Check if driver instance with low limits will not run the Application and high limits will
// * Setup a number of drivers with different restrictions
// * Fail to Allocate Application with exceeding requirements
// * Success to Allocate application with fit requirements
func Test_multiple_driver_instances(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test/dev:
      cpu_limit: 4
      ram_limit: 8
    test/prod:
      cpu_limit: 8
      ram_limit: 16`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create bad Label with test/dev driver", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test/dev",
						Resources: &aquariumv2.Resources{
							Cpu: 5,
							Ram: 9,
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

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
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

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 10 sec", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 20 sec", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 30 sec", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 40 sec", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}
	})

	t.Run("Application should get DEALLOCATED in 5 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 5 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
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

	t.Run("Create good Label with test/prod driver", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 2,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test/prod",
						Resources: &aquariumv2.Resources{
							Cpu: 5,
							Ram: 9,
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

	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
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

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
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

	t.Run("Deallocate the Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
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
			resp, err := appClient.GetState(
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
