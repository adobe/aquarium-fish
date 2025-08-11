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

// Test_aws_task_image tests the AWS TaskImage functionality
func Test_aws_task_image(t *testing.T) {
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Create AquariumFish instance with AWS driver configuration
	afi := h.NewAquariumFish(t, "aws-image-node", `---
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

	// Create RPC clients
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	var labelUID string
	var appUID string

	t.Run("Create AWS Label for Image Task", func(t *testing.T) {
		options, _ := structpb.NewStruct(map[string]any{
			"image":           "ami-12345678",
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_image_test"})
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-aws-image-label",
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
			t.Fatal("Failed to create AWS image label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created AWS image label with UID: %s", labelUID)
	})

	t.Run("Create AWS Application for Image", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"app_name": "test-aws-image-app"})
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
			t.Fatal("Failed to create AWS image application:", err)
		}
		appUID = resp.Msg.Data.Uid
		t.Logf("Created AWS image application with UID: %s", appUID)
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
		t.Logf("AWS application allocated for image test")
	})

	var taskUID string
	t.Run("Create Image Task", func(t *testing.T) {
		// Create the image task using ApplicationService
		taskMd, _ := structpb.NewStruct(map[string]any{
			"task_type":   "create_image",
			"name":        "test-ami-from-integration-test",
			"description": "Test AMI created by integration test",
			"no_reboot":   true,
		})

		resp, err := appClient.CreateTask(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateTaskRequest{
				Task: &aquariumv2.ApplicationTask{
					ApplicationUid: appUID,
					Task:           "image",
					When:           aquariumv2.ApplicationState_ALLOCATED,
					Options:        taskMd,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create image task:", err)
		}
		taskUID = resp.Msg.Data.Uid
		t.Logf("Created AWS image task with UID: %s", taskUID)
	})

	t.Run("Wait for Image Task Completion", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetTask(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetTaskRequest{
					ApplicationTaskUid: taskUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get image task:", err)
			}
			// Check if task has results (indicates completion)
			if resp.Msg.Data.Result == nil {
				r.Fatal("Image task not completed yet")
			}

			// Verify the task result contains the expected image data
			resultMap := resp.Msg.Data.Result.AsMap()
			imageID, exists := resultMap["image"]
			if !exists {
				r.Fatalf("Image ID not found in task result: %v", resultMap)
			}

			imageIDStr, ok := imageID.(string)
			if !ok {
				r.Fatalf("Image ID is not a string: %v", imageID)
			}

			imageName, exists := resultMap["image_name"]
			if !exists {
				r.Fatalf("Image name not found in task result: %v", resultMap)
			}

			imageNameStr, ok := imageName.(string)
			if !ok {
				r.Fatalf("Image name is not a string: %v", imageName)
			}

			// Verify the image was created correctly
			if !strings.HasPrefix(imageIDStr, "ami-") {
				r.Fatalf("Image ID does not have expected prefix 'ami-': %s", imageIDStr)
			}

			if !strings.Contains(imageNameStr, "ami-12345678") {
				r.Fatalf("Image name does not contain expected prefix: %s", imageNameStr)
			}

			t.Logf("AWS image task completed successfully: ID: %s, Name: %s", imageIDStr, imageNameStr)
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
