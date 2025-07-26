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
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	awsH "github.com/adobe/aquarium-fish/tests/driver_provider_aws_tests/helper"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_simple_app_aws_create_destroy tests the AWS provider driver with a mock server
// This test verifies the complete lifecycle: create label --> create application --> verify allocation --> deallocate --> verify termination
func Test_simple_app_aws_create_destroy(t *testing.T) {
	// Start mock AWS server
	mockServer := awsH.NewMockAWSServer()
	defer mockServer.Close()

	// Create AquariumFish instance with AWS driver configuration using BaseEndpoint
	afi := h.NewAquariumFish(t, "aws-node-1", `---
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
	t.Run("Create AWS Label", func(t *testing.T) {
		// Create label with AWS driver options
		options, _ := structpb.NewStruct(map[string]any{
			"image":           "ami-12345678",
			"instance_type":   "t3.micro",
			"security_groups": []any{"sg-12345678"},
		})

		md, _ := structpb.NewStruct(map[string]any{"test_env": "aws_mock"})
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-aws-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "aws",
						Resources: &aquariumv2.Resources{
							Cpu:     1,
							Ram:     2,
							Network: "subnet-12345678", // Use default subnet from mock
						},
						Options: options,
					}},
					Metadata: md,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create AWS label:", err)
		}
		labelUID = resp.Msg.Data.Uid
		t.Logf("Created AWS label with UID: %s", labelUID)
	})

	var appUID string
	t.Run("Create AWS Application", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"app_name": "test-aws-app"})
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
			t.Fatal("Failed to create AWS application:", err)
		}
		appUID = resp.Msg.Data.Uid
		t.Logf("Created AWS application with UID: %s", appUID)
	})

	t.Run("AWS Application should get ALLOCATED", func(t *testing.T) {
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
	})

	var resourceIdentifier string
	t.Run("AWS Resource should be created", func(t *testing.T) {
		resp, err := appClient.GetResource(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceGetResourceRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to get AWS application resource:", err)
		}
		if resp.Msg.Data.Identifier == "" {
			t.Fatal("AWS Resource identifier is empty")
		}
		resourceIdentifier = resp.Msg.Data.Identifier
		t.Logf("Created AWS resource with identifier: %s", resourceIdentifier)

		// Verify the mock server has the instance
		instances := mockServer.GetInstances()
		if instance, exists := instances[resourceIdentifier]; !exists {
			t.Fatalf("Mock server does not have instance: %s", resourceIdentifier)
		} else if instance.State != "running" {
			t.Fatalf("Mock instance is not in running state: %s", instance.State)
		}
	})

	t.Run("Deallocate AWS Application", func(t *testing.T) {
		_, err := appClient.Deallocate(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceDeallocateRequest{
				ApplicationUid: appUID,
			}),
		)
		if err != nil {
			t.Fatal("Failed to deallocate AWS application:", err)
		}
	})

	t.Run("AWS Application should get DEALLOCATED", func(t *testing.T) {
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
			if resp.Msg.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("AWS Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})

		// Verify the mock server has terminated the instance
		instances := mockServer.GetInstances()
		if instance, exists := instances[resourceIdentifier]; !exists {
			t.Fatalf("Mock server lost the instance: %s", resourceIdentifier)
		} else if instance.State != "terminated" {
			t.Fatalf("Mock instance is not in terminated state: %s", instance.State)
		}
	})

	t.Run("Verify Mock Server State", func(t *testing.T) {
		// Check that the mock server received the expected API calls
		instances := mockServer.GetInstances()
		if len(instances) == 0 {
			t.Fatal("Mock server has no instances")
		}

		// Verify at least one instance was created and terminated
		foundTerminated := false
		for _, instance := range instances {
			if instance.State == "terminated" {
				foundTerminated = true
				break
			}
		}
		if !foundTerminated {
			t.Fatal("No terminated instances found in mock server")
		}

		t.Logf("Mock server successfully handled AWS API calls")
		t.Logf("Total instances in mock server: %d", len(instances))
	})
}
