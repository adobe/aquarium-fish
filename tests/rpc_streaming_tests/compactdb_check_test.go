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

	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_compactdb_check verifies database compaction works correctly under streaming load
// This test ensures that:
// 1. Multiple streaming workers can create/monitor/deallocate applications continuously
// 2. Real-time subscription monitoring works under load
// 3. Database cleanup and compaction function properly with streaming operations
// 4. Final database state contains exactly 6 keys after compaction
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

	// Create admin client for gRPC streaming
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC)

	// Create streaming service client for label creation
	streamingClient := aquariumv2connect.NewStreamingServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	// Setup streaming helper for admin operations
	adminStreamingHelper := h.NewStreamingTestHelper(ctx, t, "common", streamingClient)
	defer adminStreamingHelper.Close()

	// Setup streaming with all subscription types
	subscriptionTypes := []aquariumv2.SubscriptionType{
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE,
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK,
	}

	if err := adminStreamingHelper.SetupFullStreaming(subscriptionTypes); err != nil {
		t.Fatalf("Failed to setup admin streaming: %v", err)
	}

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"test": "compactdb-streaming"})
		labelCreateReq := &aquariumv2.LabelServiceCreateRequest{
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
		}

		resp, err := adminStreamingHelper.SendRequestAndExpectSuccess(
			"create-test-label",
			"LabelServiceCreateRequest",
			labelCreateReq,
			"LabelServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}

		var labelResp aquariumv2.LabelServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&labelResp); err != nil {
			t.Fatal("Failed to unmarshal label response:", err)
		}
		labelUID = labelResp.Data.Uid
		t.Logf("Created label with UID: %s", labelUID)
	})

	var completed int32 // Use atomic int32: 0=false, 1=true

	// Streaming worker function that uses real-time subscriptions instead of polling
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, id int) {
		t.Logf("Worker %d: Started", id)
		defer t.Logf("Worker %d: Ended", id)
		defer wg.Done()

		// Create streaming client for this worker
		workerCli, workerOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC)
		workerStreamingClient := aquariumv2connect.NewStreamingServiceClient(
			workerCli,
			afi.APIAddress("grpc"),
			workerOpts...,
		)

		// Create worker context with extended timeout
		workerCtx, workerCancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer workerCancel()

		// Setup streaming helper for this worker
		workerStreamingHelper := h.NewStreamingTestHelper(workerCtx, t, fmt.Sprintf("worker%d", id), workerStreamingClient)
		defer workerStreamingHelper.Close()

		// Setup streaming for this worker
		if err := workerStreamingHelper.SetupFullStreaming(subscriptionTypes); err != nil {
			t.Errorf("Worker %d: Failed to setup streaming: %v", id, err)
			return
		}

		counter := 0
		for atomic.LoadInt32(&completed) == 0 {
			counter += 1
			// Create new application using streaming helper
			t.Logf("Worker %d: Starting new application", id)
			md, _ := structpb.NewStruct(map[string]any{"worker": fmt.Sprintf("worker-%d", id)})
			appCreateReq := &aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
					Metadata: md,
				},
			}

			resp, err := workerStreamingHelper.SendRequestAndExpectSuccess(
				fmt.Sprintf("create-app-%04d", counter),
				"ApplicationServiceCreateRequest",
				appCreateReq,
				"ApplicationServiceCreateResponse",
			)
			if err != nil {
				t.Errorf("Worker %d: Failed to create application: %v", id, err)
				return
			}

			var appResp aquariumv2.ApplicationServiceCreateResponse
			if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
				t.Errorf("Worker %d: Failed to unmarshal application response: %v", id, err)
				return
			}

			appUID := appResp.Data.Uid
			if appUID == "" {
				t.Errorf("Worker %d: Application UID is empty", id)
				return
			}
			t.Logf("Worker %d: Created application %s", id, appUID)

			// Wait for ALLOCATED state using real-time subscription (no polling!)
			_, err = workerStreamingHelper.GetStreamingClient().WaitForApplicationState(
				appUID,
				aquariumv2.ApplicationState_ALLOCATED,
				15*time.Second,
			)
			if err != nil {
				t.Errorf("Worker %d: Failed to wait for ALLOCATED state of Application %s: %v", id, appUID, err)
				return
			}
			t.Logf("Worker %d: Application allocated %s", id, appUID)

			// Deallocate the application
			t.Logf("Worker %d: Deallocating application %s", id, appUID)
			deallocateReq := &aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}

			_, err = workerStreamingHelper.SendRequestAndExpectSuccess(
				fmt.Sprintf("deallocate-app-%04d", counter),
				"ApplicationServiceDeallocateRequest",
				deallocateReq,
				"ApplicationServiceDeallocateResponse",
			)
			if err != nil {
				t.Errorf("Worker %d: Failed to deallocate application: %v", id, err)
				return
			}
			t.Logf("Worker %d: Deallocation of application completed %s", id, appUID)

			// Optional: Wait for DEALLOCATED state to ensure complete cleanup
			_, err = workerStreamingHelper.GetStreamingClient().WaitForApplicationState(
				appUID,
				aquariumv2.ApplicationState_DEALLOCATED,
				10*time.Second,
			)
			if err != nil {
				t.Logf("Worker %d: Warning - failed to wait for DEALLOCATED state: %v", id, err)
				// Don't return here, as deallocation might complete without this state change
			}

			time.Sleep(500 * time.Millisecond)
		}
	}

	// Run multiple streaming worker routines to keep DB busy during the processes
	wg := &sync.WaitGroup{}
	for id := range 10 {
		wg.Add(1)
		go workerFunc(t, wg, id)
		time.Sleep(123 * time.Millisecond)
	}

	t.Run("Applications should be cleaned from DB and compacted", func(t *testing.T) {
		// Wait for the next 10 cleanupdb completed to have enough time to fill the DB
		cleaned := make(chan struct{})
		afi.WaitForLog(` fish.cleanupdb=completed`, func(substring, line string) bool {
			t.Logf("Found cleanup: %q", substring)
			cleaned <- struct{}{}
			return false
		})
		for range 10 {
			<-cleaned
		}

		t.Logf("Now stopping the workers to calm down a bit and wait for a few more cleanups")
		atomic.StoreInt32(&completed, 1)

		t.Logf("Wait for all workers to finish...")
		wg.Wait()

		for range 4 {
			<-cleaned
		}
		afi.WaitForLogDelete(` fish.cleanupdb=completed`)

		t.Logf("Looking for Applications leftovers in the database...")
		listReq := &aquariumv2.ApplicationServiceListRequest{}
		listResp, err := adminStreamingHelper.SendRequestAndExpectSuccess(
			"list-applications",
			"ApplicationServiceListRequest",
			listReq,
			"ApplicationServiceListResponse",
		)
		if err != nil {
			t.Errorf("Failed to request list of applications: %v", err)
		} else {
			var appListResp aquariumv2.ApplicationServiceListResponse
			if err := listResp.ResponseData.UnmarshalTo(&appListResp); err != nil {
				t.Errorf("Failed to unmarshal application list response: %v", err)
			} else if len(appListResp.Data) > 0 {
				for _, app := range appListResp.Data {
					t.Logf("Found residue application: %s", app.String())
				}
			}
		}

		compacted := make(chan error)
		afi.WaitForLog(` database.compactdb=after`, func(substring, line string) bool {
			t.Logf("Found compact db result: %s", line)
			// Check the Keys get back to normal
			spl := strings.Split(line, " ")
			for _, val := range spl {
				if !strings.HasPrefix(val, "database.keys=") {
					continue
				}
				spl = strings.Split(val, "=")
				// Database should have just 6 keys left: user/admin, label/UID and node/node-1,
				// role/Administrator, role/User, role/Power
				if spl[1] != "6" {
					t.Errorf("Wrong amount of keys left in the database: %s != 6", spl[1])
				}
				break
			}
			if spl[0] != "database.keys" {
				t.Errorf("Unable to locate database compaction result for database.keys: %s", spl[0])
			}
			compacted <- nil
			return true
		})

		t.Logf("Stopping the node to trigger CompactDB process")
		afi.Stop(t)

		<-compacted
	})
}
