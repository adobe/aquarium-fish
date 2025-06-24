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
	"testing"
	"time"

	"connectrpc.com/connect"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Check the database compaction works correctly in constant flow of applications
func Test_compactdb_check(t *testing.T) {
	t.Parallel()
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

	completed := false
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, id int) {
		t.Logf("Worker %d: Started", id)
		defer t.Logf("Worker %d: Ended", id)
		defer wg.Done()

		// Create service clients for this worker
		workerCli, workerOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)
		appClient := aquariumv2connect.NewApplicationServiceClient(
			workerCli,
			afi.APIAddress("grpc"),
			workerOpts...,
		)

		for !completed {
			// Create application
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
			if appUID == "" {
				t.Errorf("Worker %d: Application UID is empty", id)
				return
			}

			// Checking state until it's allocated
			for {
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

				if stateResp.Msg.Data.Uid == "" {
					t.Errorf("Worker %d: ApplicationStatus UID is empty", id)
					return
				}
				if stateResp.Msg.Data.Status == aquariumv2.ApplicationState_ERROR {
					t.Errorf("Worker %d: ApplicationStatus is ERROR: %v", id, stateResp.Msg.Data.Status)
					return
				}

				if stateResp.Msg.Data.Status == aquariumv2.ApplicationState_ALLOCATED {
					break
				}

				time.Sleep(time.Second)
			}

			// Time to deallocate
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
		for range 10 {
			afi.WaitForLog("Fish: CleanupDB completed", func(substring, line string) bool {
				cleaned <- struct{}{}
				return true
			})
			<-cleaned
		}

		// Now stopping the workers to calm down a bit and wait for a few more cleanups
		completed = true
		for range 3 {
			afi.WaitForLog("Fish: CleanupDB completed", func(substring, line string) bool {
				cleaned <- struct{}{}
				return true
			})
			<-cleaned
		}

		compacted := make(chan error)
		afi.WaitForLog("DB: CompactDB: After compaction: ", func(substring, line string) bool {
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

		// Stopping the node to trigger CompactDB process
		afi.Stop(t)

		<-compacted

		// Wait for all workers to finish
		wg.Wait()
	})
}
