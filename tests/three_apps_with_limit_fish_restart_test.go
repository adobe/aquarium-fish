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

// Will allocate 2 Applications and restart the fish node to check if they will be picked up after
// * 2 apps allocated simultaneously and third one waits
// * Fish node restarts
// * Checks that 2 Apps are still ALLOCATED and third one is NEW
// * Destroying first 2 apps and third should become allocated
// * Destroy the third app
func Test_three_apps_with_limit_fish_restart(t *testing.T) {
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
							Cpu: 2,
							Ram: 4,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var app1UID, app2UID, app3UID string
	t.Run("Create 3 Applications", func(t *testing.T) {
		// Create first application
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

		// Create second application
		resp, err = appClient.Create(
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

		// Create third application
		resp, err = appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application 3:", err)
		}
		app3UID = resp.Msg.Data.Uid

		if app1UID == uuid.Nil.String() || app2UID == uuid.Nil.String() || app3UID == uuid.Nil.String() {
			t.Fatalf("One or more Application UIDs are incorrect: %v, %v, %v", app1UID, app2UID, app3UID)
		}
	})

	t.Run("2 Applications should get ALLOCATED in 10 sec", func(t *testing.T) {
		allocatedCount := 0
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			allocatedCount = 0
			appUIDs := []string{app1UID, app2UID, app3UID}

			for i, appUID := range appUIDs {
				resp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: appUID,
					}),
				)
				if err != nil {
					r.Fatalf("Failed to get application %d state: %v", i+1, err)
				}

				if resp.Msg.Data.Status == aquariumv2.ApplicationState_ALLOCATED {
					allocatedCount++
				}
			}

			if allocatedCount != 2 {
				r.Fatalf("Expected 2 applications to be allocated, but got %d", allocatedCount)
			}
		})
	})

	t.Run("1 Application should remain NEW", func(t *testing.T) {
		newCount := 0
		appUIDs := []string{app1UID, app2UID, app3UID}

		for i, appUID := range appUIDs {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				t.Fatalf("Failed to get application %d state: %v", i+1, err)
			}

			if resp.Msg.Data.Status == aquariumv2.ApplicationState_NEW {
				newCount++
			}
		}

		if newCount != 1 {
			t.Fatalf("Expected 1 application to remain NEW, but got %d", newCount)
		}
	})

	// Restart the fish app node
	afi.Restart(t)

	var allocatedUIDs []string
	var notAllocatedUID string
	t.Run("2 of 3 Applications should be ALLOCATED right after restart", func(t *testing.T) {
		// Need to recreate clients after restart
		adminCli, adminOpts = h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
		appClient = aquariumv2connect.NewApplicationServiceClient(
			adminCli,
			afi.APIAddress("grpc"),
			adminOpts...,
		)

		allocatedUIDs = []string{}
		appUIDs := []string{app1UID, app2UID, app3UID}

		for i, appUID := range appUIDs {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				t.Fatalf("Failed to get application %d state after restart: %v", i+1, err)
			}

			if resp.Msg.Data.Status == aquariumv2.ApplicationState_ALLOCATED {
				allocatedUIDs = append(allocatedUIDs, appUID)
			} else {
				notAllocatedUID = appUID
			}
		}

		if len(allocatedUIDs) != 2 {
			t.Fatalf("Expected 2 applications to be allocated after restart, but got %d", len(allocatedUIDs))
		}
	})

	t.Run("3rd Application still should have state NEW", func(t *testing.T) {
		resp, err := appClient.GetState(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: notAllocatedUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get 3rd application state:", err)
		}

		if resp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("3rd Application Status is incorrect: %v", resp.Msg.Data.Status)
		}
	})

	t.Run("Deallocate the first 2 Applications", func(t *testing.T) {
		for i, appUID := range allocatedUIDs {
			_, err := appClient.Deallocate(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				t.Fatalf("Failed to deallocate application %d: %v", i+1, err)
			}
		}
	})

	t.Run("3rd Application should get ALLOCATED in 30 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 5 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: notAllocatedUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get 3rd application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})

	t.Run("Deallocate the 3rd Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: notAllocatedUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate 3rd application:", err)
		}
	})

	t.Run("3rd Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: notAllocatedUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get 3rd application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}
