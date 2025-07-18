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
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/steinfletcher/apitest"
	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_simple_app is a simple test with one application and without any limits:
// * Create Label
// * Create Application
// * Check Application state
// * Get Application resource
// * Deallocate Application
// * Check Application state
func Test_simple_app_create_destroy(t *testing.T) {
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

	cli := &http.Client{
		Timeout: time.Second * 5,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		},
	}

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
		md, _ := structpb.NewStruct(map[string]any{"test1": "test2"})
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
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"testk": "testv"})
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid
	})

	t.Run("Check there is just one Application in the list", func(t *testing.T) {
		resp, err := appClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list applications:", err)
		}
		if len(resp.Msg.Data) != 1 {
			t.Fatalf("Amount of Applications is incorrect: %d != 1", len(resp.Msg.Data))
		}
	})

	t.Run("Application should get ALLOCATED in 1 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: time.Second, Wait: 300 * time.Millisecond}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err, resp)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
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
		if resp.Msg.Data.Identifier == "" {
			t.Fatal("Resource identifier is empty")
		}
	})

	var metadata map[string]string
	t.Run("Check metadata is available for the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("meta/v1/data/")).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&metadata)

		if len(metadata) != 2 {
			t.Fatalf("Amount of metadata keys is incorrect: %d != 2, %v", len(metadata), metadata)
		}

		if val, ok := metadata["test1"]; !ok || val != "test2" {
			t.Fatalf("Metadata key from label is unset: %v", metadata)
		}

		if val, ok := metadata["testk"]; !ok || val != "testv" {
			t.Fatalf("Metadata key from application is unset: %v", metadata)
		}

		// Also check for env formatter
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("meta/v1/data/")).
			Query("format", "env").
			Expect(t).
			Status(http.StatusOK).
			Body("test1=test2\ntestk=testv\n").
			End()

		// And export formatter
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("meta/v1/data/")).
			Query("format", "export").
			Expect(t).
			Status(http.StatusOK).
			Body("export test1=test2\nexport testk=testv\n").
			End()

		// And ps1 formatter
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("meta/v1/data/")).
			Query("format", "ps1").
			Expect(t).
			Status(http.StatusOK).
			Body("$test1='test2'\n$testk='testv'\n").
			End()
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

	t.Run("Application should get DEALLOCATED in 1 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: time.Second, Wait: 300 * time.Millisecond}, t, func(r *h.R) {
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
