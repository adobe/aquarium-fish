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

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Benchmark to check how many nodes could wait for Application
func Test_jenkins_agents_awaiting_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 100000
      ram_limit: 200000`)

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

	// Running periodic requests to test what's the delay will be
	wg := &sync.WaitGroup{}
	reachedLimit := false
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, afi *h.AFInstance) {
		defer wg.Done()

		// Create individual client for each goroutine
		cli, opts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)
		appClient := aquariumv2connect.NewApplicationServiceClient(
			cli,
			afi.APIAddress("grpc"),
			opts...,
		)

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
			t.Errorf("Failed to create application: %v", err)
			return
		}
		appUID = appResp.Msg.Data.Uid

		if appUID == uuid.Nil.String() {
			t.Errorf("Application UID is incorrect: %v", appUID)
		}

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
			time.Sleep(5 * time.Second)
		}
		t.Logf("Client thread completed")
	}
	counter := 0
	for !reachedLimit {
		// Running 40 parallel threads at a time to simulate a big pipeline startup
		for range 40 {
			wg.Add(1)
			go workerFunc(t, wg, afi)
			counter += 1
		}
		t.Logf("Client threads: %d", counter)
		time.Sleep(time.Second)
	}
	t.Logf("Completed, waiting for stop: %d", counter)
	wg.Wait()
}
