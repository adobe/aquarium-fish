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
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_simple_app_create_destroy_subscription demonstrates real-time streaming using subscriptions
// This test showcases:
// * Streamlined setup with helper abstractions
// * Pure subscription-based state verification (no polling!)
// * Clean separation between bidirectional requests and subscription notifications
// * Production-ready real-time streaming patterns
func Test_simple_app_create_destroy_subscription(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC, afi.GetCA(t))

	// Create streaming service client
	streamingClient := aquariumv2connect.NewStreamingServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Setup streaming helper with all subscription types
	streamingHelper := h.NewStreamingTestHelper(ctx, t, "common", streamingClient)
	defer streamingHelper.Close()

	// Setup both bidirectional and subscription streaming in one call
	subscriptionTypes := []aquariumv2.SubscriptionType{
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE,
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK,
	}

	if err := streamingHelper.SetupFullStreaming(subscriptionTypes); err != nil {
		t.Fatalf("Failed to setup streaming: %v", err)
	}

	var labelUID string
	var appUID string

	t.Run("Create Label", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"test1": "test2"})
		labelReq := &aquariumv2.LabelServiceCreateRequest{
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

		resp, err := streamingHelper.SendRequestAndExpectSuccess(
			"create-label",
			"LabelServiceCreateRequest",
			labelReq,
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
		if labelUID == "" {
			t.Fatal("Label UID is empty")
		}

		t.Logf("Created label with UID: %s", labelUID)
	})

	t.Run("Create Application", func(t *testing.T) {
		md, _ := structpb.NewStruct(map[string]any{"testk": "testv"})
		appReq := &aquariumv2.ApplicationServiceCreateRequest{
			Application: &aquariumv2.Application{
				LabelUid: labelUID,
				Metadata: md,
			},
		}

		resp, err := streamingHelper.SendRequestAndExpectSuccess(
			"create-app",
			"ApplicationServiceCreateRequest",
			appReq,
			"ApplicationServiceCreateResponse",
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}

		var appResp aquariumv2.ApplicationServiceCreateResponse
		if err := resp.ResponseData.UnmarshalTo(&appResp); err != nil {
			t.Fatal("Failed to unmarshal application response:", err)
		}

		appUID = appResp.Data.Uid
		if appUID == "" {
			t.Fatal("Application UID is empty")
		}

		t.Logf("Created application with UID: %s", appUID)
	})

	t.Run("Wait for ALLOCATED state (real-time subscription)", func(t *testing.T) {
		// This demonstrates real-time streaming - no polling required!
		_, err := streamingHelper.GetStreamingClient().WaitForApplicationState(
			appUID,
			aquariumv2.ApplicationState_ALLOCATED,
			15*time.Second,
		)
		if err != nil {
			t.Fatal("Failed to receive ALLOCATED state:", err)
		}

		t.Log("Received ALLOCATED state through real-time subscription!")
	})

	t.Run("Wait for Resource allocation (real-time subscription)", func(t *testing.T) {
		// Get resource information through subscription instead of request/response
		resource, err := streamingHelper.GetStreamingClient().WaitForApplicationResource(15 * time.Second)
		if err != nil {
			t.Fatal("Failed to receive resource notification:", err)
		}

		if resource.Identifier == "" {
			t.Fatal("Resource identifier is empty")
		}

		t.Logf("Received resource through subscription! Identifier: %s", resource.Identifier)
	})

	t.Run("Deallocate Application", func(t *testing.T) {
		deallocateReq := &aquariumv2.ApplicationServiceDeallocateRequest{
			ApplicationUid: appUID,
		}

		_, err := streamingHelper.SendRequestAndExpectSuccess(
			"deallocate-app",
			"ApplicationServiceDeallocateRequest",
			deallocateReq,
			"ApplicationServiceDeallocateResponse",
		)
		if err != nil {
			t.Fatal("Failed to deallocate application:", err)
		}

		t.Log("Successfully deallocated application")
	})

	t.Run("Wait for DEALLOCATED state (real-time subscription)", func(t *testing.T) {
		// Wait for final state through subscription
		_, err := streamingHelper.GetStreamingClient().WaitForApplicationState(
			appUID,
			aquariumv2.ApplicationState_DEALLOCATED,
			15*time.Second,
		)
		if err != nil {
			t.Fatal("Failed to receive DEALLOCATED state:", err)
		}

		t.Log("Received DEALLOCATED state through real-time subscription!")
	})

	t.Log("Real-time subscription streaming test completed successfully!")
}
