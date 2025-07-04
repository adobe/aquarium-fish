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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Check the database compaction works correctly in constant flow of applications
func Test_compactdb_check(t *testing.T) {
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
default_resource_lifetime: 20s

api_address: 127.0.0.1:0

db_cleanup_interval: 10s
db_compact_interval: 5s

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

	// No ERROR could happen during execution of this test
	afi.WaitForLog("ERROR:", func(substring, line string) bool {
		t.Errorf("Error located in the Fish log: %q", line)
		return true
	})

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

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
	})

	workerCli, workerOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	var completed int32 // Use atomic int32: 0=false, 1=true
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, id int) {
		t.Logf("Worker %d: Started", id)
		defer t.Logf("Worker %d: Ended", id)
		defer wg.Done()

		// Create service clients for this worker
		appClient := aquariumv2connect.NewApplicationServiceClient(
			workerCli,
			afi.APIAddress("grpc"),
			workerOpts...,
		)

		for atomic.LoadInt32(&completed) == 0 {
			// Create new application
			t.Logf("Worker %d: Starting new application", id)
			resp, err := appClient.Create(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
					Application: &aquariumv2.Application{
						LabelUid: labelUID,
					},
				}),
			)
			if err != nil {
				t.Errorf("Worker %d: Failed to create application: %v", id, err)
				return
			}

			appUID := resp.Msg.Data.Uid
			if appUID == "" || appUID == uuid.Nil.String() {
				t.Errorf("Worker %d: Application UID is empty", id)
				return
			}
			t.Logf("Worker %d: Created application %s", id, appUID)

			// Checking state for 20 times until it's allocated - duratin ~5 seconds total
			var appStatus aquariumv2.ApplicationState_Status
			for range 20 {
				stateResp, err := appClient.GetState(
					context.Background(),
					connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
						ApplicationUid: appUID,
					}),
				)
				if err != nil {
					t.Errorf("Worker %d: Failed to get application state: %v", id, err)
					return
				}

				if stateResp.Msg.Data.Uid == "" || stateResp.Msg.Data.Uid == uuid.Nil.String() {
					t.Errorf("Worker %d: ApplicationStatus UID is empty", id)
					return
				}

				appStatus = stateResp.Msg.Data.Status
				if appStatus == aquariumv2.ApplicationState_ERROR {
					t.Errorf("Worker %d: ApplicationStatus is ERROR: %v", id, appStatus)
					return
				}

				if appStatus == aquariumv2.ApplicationState_ALLOCATED {
					t.Logf("Worker %d: Application allocated %s", id, appUID)
					break
				}

				time.Sleep(250 * time.Millisecond)
			}

			if appStatus != aquariumv2.ApplicationState_ALLOCATED {
				t.Errorf("Worker %d: Did not receive ALLOCATED ApplicationStatus in 5 seconds: %s", id, appStatus)
				return
			}

			// Time to deallocate
			t.Logf("Worker %d: Deallocating application %s", id, appUID)
			_, err = appClient.Deallocate(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				t.Errorf("Worker %d: Failed to deallocate application: %v", id, err)
				return
			}
			t.Logf("Worker %d: Deallocation of application completed %s", id, appUID)

			time.Sleep(500 * time.Millisecond)
		}
	}

	// Run multiple application create/terminate routines to keep DB busy during the processes
	wg := &sync.WaitGroup{}
	for id := range 10 {
		wg.Add(1)
		go workerFunc(t, wg, id)
		time.Sleep(123 * time.Millisecond)
	}

	t.Run("Applications should be cleaned from DB and compacted", func(t *testing.T) {
		// Wait for the next 20 cleanupdb completed to have enough time to fill the DB
		cleaned := make(chan struct{})
		for i := range 10 {
			afi.WaitForLog("Fish: CleanupDB completed", func(substring, line string) bool {
				t.Logf("Found warm up %d: %q", i, substring)
				cleaned <- struct{}{}
				return true
			})
			<-cleaned
		}

		t.Logf("Now stopping the workers to calm down a bit and wait for a few more cleanups")
		atomic.StoreInt32(&completed, 1)

		t.Logf("Wait for all workers to finish...")
		wg.Wait()

		for i := range 4 {
			afi.WaitForLog("Fish: CleanupDB completed", func(substring, line string) bool {
				t.Logf("Found calm down %d: %q", i, substring)
				cleaned <- struct{}{}
				return true
			})
			<-cleaned
		}

		t.Logf("Looking for Applications leftovers in the database...")
		appClient := aquariumv2connect.NewApplicationServiceClient(
			workerCli,
			afi.APIAddress("grpc"),
			workerOpts...,
		)
		listResp, err := appClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceListRequest{}),
		)
		if err != nil {
			t.Errorf("Failed to request list of applications: %v", err)
		} else if len(listResp.Msg.Data) > 0 {
			for _, app := range listResp.Msg.Data {
				t.Logf("Found residue application: %s", app.String())
			}
		}

		compacted := make(chan error)
		afi.WaitForLog("DB: CompactDB: After compaction: ", func(substring, line string) bool {
			t.Logf("Found compact db result: %s", line)
			// Check the Keys get back to normal
			spl := strings.Split(line, ", ")
			for _, val := range spl {
				if !strings.Contains(val, "Keys: ") {
					continue
				}
				spl = strings.Split(val, ": ")
				// Database should have just 6 keys left: user/admin, label/UID and node/node-1,
				// role/Administrator, role/User, role/Power
				if spl[1] != "6" {
					t.Errorf("Wrong amount of keys left in the database: %s != 6", spl[1])
					break
				}
			}
			if spl[0] != "Keys" {
				t.Errorf("Unable to locate database compaction result for Keys: %s", spl[0])
			}
			compacted <- nil
			return true
		})

		t.Logf("Stopping the node to trigger CompactDB process")
		afi.Stop(t)

		<-compacted
	})
}
