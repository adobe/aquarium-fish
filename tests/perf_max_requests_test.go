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

// Benchmark to find the max amount of requests per second
func Test_max_requests_stress(t *testing.T) {
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
      cpu_limit: 1
      ram_limit: 2`)

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

	var appUID string
	appResp, err := appClient.Create(
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
	appUID = appResp.Msg.Data.Uid

	if appUID == uuid.Nil.String() {
		t.Errorf("Application UID is incorrect: %v", appUID)
	}

	// Here all the apps are in the queue, so let's request something with a small timeout
	stateResp, err := appClient.GetState(
		context.Background(),
		connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
			ApplicationUid: appUID,
		}),
	)
	if err != nil {
		t.Fatal("Failed to get application state:", err)
	}

	if stateResp.Msg.Data.Status != aquariumv2.ApplicationState_NEW {
		t.Fatalf("Application Status is incorrect: %v", stateResp.Msg.Data.Status)
	}

	// Running periodic requests to test what's the delay will be
	wg := &sync.WaitGroup{}
	reachedLimit := false
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, afi *h.AFInstance, appUID string) {
		defer wg.Done()

		// Create individual client for each goroutine
		cli, opts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)
		appClient := aquariumv2connect.NewApplicationServiceClient(
			cli,
			afi.APIAddress("grpc"),
			opts...,
		)

		for !reachedLimit {
			start := time.Now()
			_, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				t.Errorf("Failed to get application state: %v", err)
				return
			}

			elapsed := time.Since(start).Milliseconds()
			t.Logf("Request delay: %dms", elapsed)
			if elapsed > 5000 {
				reachedLimit = true
			}
		}
		t.Logf("Client thread completed")
	}
	counter := 0
	for !reachedLimit {
		// Gradually increase the amount of parallel threads
		wg.Add(1)
		go workerFunc(t, wg, afi, appUID)
		counter += 1
		t.Logf("Client threads: %d", counter)
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("Completed, waiting for stop: %d", counter)
	wg.Wait()
}
