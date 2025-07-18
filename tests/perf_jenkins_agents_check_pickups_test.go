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
	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

const STEP = 50
const ATTEMPTS = 10

// Benchmark to run multiple persistent vote processes and measure the allocation time for
// It also checks that the amount of pickups is equal the amount of application requests
func Test_jenkins_agents_check_pickups_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      is_remote: true
      cpu_limit: 100000
      ram_limit: 200000`, "--timestamp=true")

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

	// Creating 2 labels - one for the app that can't be allocated and another one for a good app
	var labelNoWayUID string
	delayOptions, _ := structpb.NewStruct(map[string]any{"delay_available_capacity": 0.1})
	resp, err := labelClient.Create(
		context.Background(),
		connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "label-noway",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver:  "test",
					Options: delayOptions,
					Resources: &aquariumv2.Resources{
						Cpu: 999999,
						Ram: 9999999,
					},
				}},
			},
		}),
	)
	if err != nil {
		t.Fatal("Failed to create labelNoWay:", err)
	}
	labelNoWayUID = resp.Msg.Data.Uid

	if labelNoWayUID == uuid.Nil.String() {
		t.Fatalf("LabelNoWay UID is incorrect: %v", labelNoWayUID)
	}

	var labelTheWayUID string
	resp, err = labelClient.Create(
		context.Background(),
		connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
			Label: &aquariumv2.Label{
				Name:    "label-theway",
				Version: 1,
				Definitions: []*aquariumv2.LabelDefinition{{
					Driver:  "test",
					Options: delayOptions,
					Resources: &aquariumv2.Resources{
						Cpu: 1,
						Ram: 2,
					},
				}},
			},
		}),
	)
	if err != nil {
		t.Fatal("Failed to create labelTheWay:", err)
	}
	labelTheWayUID = resp.Msg.Data.Uid

	if labelTheWayUID == uuid.Nil.String() {
		t.Fatalf("LabelTheWay UID is incorrect: %v", labelTheWayUID)
	}

	// Running goroutines amount fetcher
	exitTest := false
	//go func() {
	//	var amount int
	//
	//	for !exitTest {
	//		amount = 0
	//		res := apitest.New().
	//			EnableNetworking(cli).
	//			Get(afi.APIAddress("api/v1/node/this/profiling/goroutine")).
	//			Query("debug", "2").
	//			BasicAuth("admin", afi.AdminToken()).
	//			Expect(t).
	//			Status(http.StatusOK).
	//			End()
	//
	//		scanner := bufio.NewScanner(res.Response.Body)
	//		for scanner.Scan() {
	//			line := scanner.Text()
	//			if strings.HasPrefix(line, "goroutine ") {
	//				amount += 1
	//			}
	//		}
	//		res.Response.Body.Close()
	//
	//		t.Log("Goroutines amount:", amount)
	//
	//		time.Sleep(5 * time.Second)
	//	}
	//}()

	// Running periodic requests to test what's the delay will be
	workerFunc := func(t *testing.T, afi *h.AFInstance) {
		// Create individual client for each goroutine
		cli, opts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
		appClient := aquariumv2connect.NewApplicationServiceClient(
			cli,
			afi.APIAddress("grpc"),
			opts...,
		)

		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelNoWayUID,
				},
			}),
		)
		if err != nil {
			exitTest = true
			t.Errorf("Failed to create application: %v", err)
			return
		}

		if resp.Msg.Data.Uid == uuid.Nil.String() {
			exitTest = true
			t.Errorf("Application UID is incorrect: %v", resp.Msg.Data.Uid)
		}
	}

	// Creating the apps that will not be executed and then measuring how much time it takes to
	// allocate and deallocate the actual good application
	wg := &sync.WaitGroup{}
	counter := 0

	// Monitoring pickups of Applications
	afi.WaitForLog("Fish: NEW Application with no Vote:", func(substring, line string) bool {
		// If the application processing is not expected - wg.Done() will panic
		defer func() {
			// Notifying the test of a failure in Application processing
			if r := recover(); r != nil {
				//exitTest = true
				t.Errorf("Detected not expected Application processing: %s: %v", line, r)
			}
		}()
		wg.Done()

		// Returning false here to continue to catch the Applications processing forever
		return false
	})

	// Repeated test until failure
	for range ATTEMPTS {
		// Running test on how long it takes to pickup & allocate an application
		// It should be no longer then 5 seconds (delay between pickups)
		t.Logf("Running test: (bg elections: %d)", counter)
		t.Run(fmt.Sprintf("Application should be ALLOCATED in 20 sec (bg elections: %d)", counter), func(t *testing.T) {
			var appUID string
			// Keep track of applications in wg to make sure there is no more apps picked up by the Fish
			wg.Add(1)
			resp, err := appClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
					Application: &aquariumv2.Application{
						LabelUid: labelTheWayUID,
					},
				}),
			)
			if err != nil {
				exitTest = true
				t.Errorf("Failed to create desired application: %v", err)
				return
			}
			appUID = resp.Msg.Data.Uid

			if appUID == uuid.Nil.String() {
				exitTest = true
				t.Errorf("Desired Application UID is incorrect: %v", appUID)
			}

			// Wait for Allocate of the Application
			h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
				stateResp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: appUID,
					}),
				)
				if err != nil {
					exitTest = true
					r.Fatalf("Failed to get application state: %v", err)
					return
				}

				if stateResp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
					exitTest = true
					r.Fatalf("Desired Application %s Status is incorrect: %v", stateResp.Msg.Data.ApplicationUid, stateResp.Msg.Data.Status)
				} else {
					exitTest = false
				}
			})
		})

		// Stop test execution if error happened
		if exitTest {
			break
		}

		// Running STEP amount of parallel applications at a time to simulate a big pipeline startup
		wg.Add(STEP)
		for range STEP {
			go workerFunc(t, afi)
			counter += 1
		}

		// Wait for pickup of those Applications
		wg.Wait()
	}
	t.Logf("Completed: %d", counter)
}
