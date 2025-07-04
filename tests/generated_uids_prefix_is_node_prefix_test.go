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

// The test ensures the Node is actually participate in generating it's data UID's
// * Checks Label
// * Checks Application
// * Checks ApplicationState
// * Checks Resource
// * TODO: Other data UIDs
func Test_generated_uids_prefix_is_node_prefix(t *testing.T) {
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

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	// Create service clients
	nodeClient := aquariumv2connect.NewNodeServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
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

	var node *aquariumv2.Node
	t.Run("Get node data", func(t *testing.T) {
		resp, err := nodeClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.NodeServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list nodes:", err)
		}

		if len(resp.Msg.Data) != 1 {
			t.Fatalf("Nodes list count is not 1: %d", len(resp.Msg.Data))
		}
		node = resp.Msg.Data[0]
		if node.Uid == "" || node.Uid == uuid.Nil.String() {
			t.Fatalf("Node UID is empty")
		}
	})

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
			t.Fatalf("Label UID is empty")
		}

		if labelUID[:6] != node.Uid[:6] {
			t.Fatalf("Label UID prefix != Node UID prefix: %v, %v", labelUID, node.Uid)
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
			t.Fatalf("Application UID is empty")
		}

		if appUID[:6] != node.Uid[:6] {
			t.Fatalf("Application UID prefix != Node UID prefix: %v, %v", appUID, node.Uid)
		}
	})

	var appStateUID string
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

			if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
				r.Fatalf("ApplicationState UID is empty")
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}

			appStateUID = resp.Msg.Data.Uid
			if appStateUID[:6] != node.Uid[:6] {
				r.Fatalf("ApplicationState UID prefix != Node UID prefix: %v, %v", appStateUID, node.Uid)
			}
		})
	})

	t.Run("Resource should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get application resource:", err)
		}

		if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
			t.Fatalf("Resource UID is empty")
		}

		if resp.Msg.Data.Identifier == "" {
			t.Fatalf("Resource identifier is empty")
		}

		if resp.Msg.Data.Uid[:6] != node.Uid[:6] {
			t.Fatalf("Resource UID prefix != Node UID prefix: %v, %v", resp.Msg.Data.Uid, node.Uid)
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
