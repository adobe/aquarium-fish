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

package driver_provider_aws_tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	awsH "github.com/adobe/aquarium-fish/tests/driver_provider_aws_tests/helper"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_aws_task_snapshot tests the AWS TaskSnapshot functionality
func Test_aws_task_snapshot(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-snapshot-node", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    aws:
      region: us-west-2
      key_id: mock-access-key
      secret_key: mock-secret-key
      instance_key: generate
      base_url: `+mockServer.GetURL())

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST)

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string
	var appUID string

	t.Run("Create AWS Label for Snapshot Task", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"image":           "ami-12345678",
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_snapshot_test"})
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-aws-snapshot-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: "subnet-12345678",
						},
						Options: options,
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create AWS snapshot label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created AWS snapshot label with UID: %s", labelUID)
	})

	t.Run("Create AWS Application for Snapshot", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"app_name": "test-aws-snapshot-app"})
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
			t.Fatal("Failed to create AWS snapshot application:", err)
		}
		appUID = resp.Msg.Data.Uid
		t.Logf("Created AWS snapshot application with UID: %s", appUID)
	})

	t.Run("Wait for Application Allocation", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get AWS application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("AWS Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
		t.Logf("AWS application allocated for snapshot test")
	})

	var taskUID string
	t.Run("Create Snapshot Task", func(t *testing.T) {
		// Create the snapshot task using ApplicationService
		taskMd, _ := structpb.NewStruct(map[string]any{
			"task_type":   "create_snapshot",
			"description": "Test snapshot created by integration test",
		})

		resp, err := appClient.CreateTask(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateTaskRequest{
				ApplicationUid: appUID,
				Task: &aquariumv2.ApplicationTask{
					Task:    "snapshot",
					When:    aquariumv2.ApplicationState_ALLOCATED,
					Options: taskMd,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create snapshot task:", err)
		}
		taskUID = resp.Msg.Data.Uid
		t.Logf("Created AWS snapshot task with UID: %s", taskUID)
	})

	t.Run("Wait for Snapshot Task Completion", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 60 * time.Second, Wait: 2 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetTask(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetTaskRequest{
					ApplicationTaskUid: taskUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get snapshot task:", err)
			}
			// Check if task has results (indicates completion)
			if resp.Msg.Data.Result == nil {
				r.Fatal("Snapshot task not completed yet")
			}

			// Check if task result contains expected snapshot data
			resultMap := resp.Msg.Data.Result.AsMap()
			snapshotsInterface, exists := resultMap["snapshots"]
			if !exists {
				r.Fatalf("No snapshots field found in the result: %v", resultMap)
			}
			snapshots, ok := snapshotsInterface.([]interface{})
			if !ok {
				r.Fatalf("Snapshots field is not an array: %v", snapshotsInterface)
			}

			if len(snapshots) == 0 {
				r.Fatalf("No snapshots found in task result: %v", snapshots)
			}

			snapshotID, ok := snapshots[0].(string)
			if !ok {
				r.Fatalf("Snapshot ID is not a string: %v", snapshots[0])
			}

			// Verify the snapshot was created correctly
			if !strings.HasPrefix(snapshotID, "snap-") {
				r.Fatalf("Snapshot ID does not have expected prefix 'snap-': %s", snapshotID)
			}

			t.Logf("AWS snapshot task completed successfully: ID: %s", snapshotID)
		})
	})

	t.Run("Cleanup - Deallocate Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate AWS application:", err)
		}

		// Try to wait for deallocation to complete, but handle if it gets stuck
		deallocateSuccess := true
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get AWS application state:", err)
			}
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				if resp.Msg.Data.Status == aquariumv2.ApplicationState_DEALLOCATE {
					deallocateSuccess = false
					return // Don't retry if stuck in DEALLOCATE
				}
				r.Fatalf("AWS Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})

		if !deallocateSuccess {
			t.Fatalf("AWS application deallocation initiated but stuck")
		}
	})
}
