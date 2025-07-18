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
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Checks if node can handle multiple application requests at a time
// Fish node should be able to handle ~20 requests / second when limited to 2 CPU core and 500MB of memory
func Test_allocate_apps_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
cpu_limit: 2
mem_target: "512MB"

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 1000
      ram_limit: 2000`)

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

		if labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	// Spin up 50 of threads to create application and look what will happen
	wg := &sync.WaitGroup{}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(t *testing.T, wg *sync.WaitGroup, id int, afi *h.AFInstance, labelUID string) {
			defer wg.Done()

			// Create individual client for each goroutine
			cli, opts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
			appClient := aquariumv2connect.NewApplicationServiceClient(
				cli,
				afi.APIAddress("grpc"),
				opts...,
			)

			t.Run(fmt.Sprintf("%04d Create Application", id), func(t *testing.T) {
				resp, err := appClient.Create(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
						Application: &aquariumv2.Application{
							LabelUid: labelUID,
						},
					}),
				)
				if err != nil {
					t.Error("Failed to create application:", err)
					return
				}

				if resp.Msg.Data.Uid == uuid.Nil.String() {
					t.Errorf("Application UID is incorrect: %v", resp.Msg.Data.Uid)
				}
			})
		}(t, wg, i, afi, labelUID)
	}
	wg.Wait()
}

// Checks if node can handle multiple application requests at a time with no auth
// Without auth it should be relatively simple for the fish node to ingest 200 requests in less then a second
func Test_allocate_apps_noauth_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
cpu_limit: 8
mem_target: "1024MB"

api_address: 127.0.0.1:0

disable_auth: true

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

	// Create admin client (no auth)
	adminCli, adminOpts := h.NewRPCClient("admin", "notoken", h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
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

		if labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	// Flooding the node with 10 batches of 200 parallel Applications requests
	for b := 0; b < 10; b++ {
		// Spin up 200 of threads to create application and look what will happen
		wg := &sync.WaitGroup{}
		afi.PrintMemUsage(t)
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func(t *testing.T, wg *sync.WaitGroup, batch, id int, afi *h.AFInstance, labelUID string) {
				defer wg.Done()

				// Create individual client for each goroutine (no auth)
				cli, opts := h.NewRPCClient("admin", "notoken", h.RPCClientREST, afi.GetCA(t))
				appClient := aquariumv2connect.NewApplicationServiceClient(
					cli,
					afi.APIAddress("grpc"),
					opts...,
				)

				t.Run(fmt.Sprintf("%03d-%04d Create Application", batch, id), func(t *testing.T) {
					h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 300 * time.Millisecond}, t, func(r *h.R) {
						resp, err := appClient.Create(
							context.Background(),
							connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
								Application: &aquariumv2.Application{
									LabelUid: labelUID,
								},
							}),
						)
						if err != nil {
							r.Error("Failed to create application:", err)
							return
						}

						if resp.Msg.Data.Uid == uuid.Nil.String() {
							r.Errorf("Application UID is incorrect: %v", resp.Msg.Data.Uid)
						}
					})
				})
			}(t, wg, b, i, afi, labelUID)
		}
		wg.Wait()
	}
}
