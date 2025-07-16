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

// Checks the complete fill of the node with one Application, so the next one can't be executed:
// * First Application is allocated
// * Second Application can't be allocated
// * Destroying first Application
// * Second Application is allocated
// * Destroy second Application
func Test_two_apps_with_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 4
      ram_limit: 8`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA())

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
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 4,
							Ram: 8,
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

	var app1UID string
	t.Run("Create Application 1", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application 1:", err)
		}
		app1UID = resp.Msg.Data.Uid

		if app1UID == "" || app1UID == uuid.Nil.String() {
			t.Fatalf("Application 1 UID is incorrect: %v", app1UID)
		}
	})

	var app2UID string
	t.Run("Create Application 2", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application 2:", err)
		}
		app2UID = resp.Msg.Data.Uid

		if app2UID == "" || app2UID == uuid.Nil.String() {
			t.Fatalf("Application 2 UID is incorrect: %v", app2UID)
		}
	})

	t.Run("Application 1 should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: app1UID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application 1 state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application 1 Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	t.Run("Application 2 should have state NEW", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: app2UID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application 2 state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("Application 2 Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	t.Run("Resource 1 should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: app1UID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application 1 resource:", err)
		}

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource 1 identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	t.Run("Deallocate the Application 1", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: app1UID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application 1:", err)
		}
	})

	t.Run("Application 1 should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: app1UID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application 1 state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application 1 Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	t.Run("Application 2 should get ALLOCATED in 40 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 40 * time.Second, Wait: 5 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: app2UID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application 2 state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application 2 Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	t.Run("Resource 2 should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: app2UID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application 2 resource:", err)
		}

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource 2 identifier is incorrect: %v", resp.Msg.Data.Identifier)
		}
	})

	t.Run("Deallocate the Application 2", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: app2UID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate application 2:", err)
		}
	})

	t.Run("Application 2 should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: app2UID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application 2 state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application 2 Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}
