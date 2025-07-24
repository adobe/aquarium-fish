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
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_simple_app_create_destroy is a test that focuses purely on bidirectional streaming (Connect method)
// * Establish bidirectional streaming connection
// * Create Label through streaming
// * Create Application through streaming
// * Check Application state through streaming requests (no subscriptions)
// * Get Application resource through streaming
// * Deallocate Application through streaming
// * Verify all operations work through bidirectional streaming while preserving RBAC
func Test_simple_app_create_destroy(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC, afi.GetCA(t))

	// Create streaming service client
	streamingClient := aquariumv2connect.NewStreamingServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup streaming helper for bidirectional streaming only (no subscriptions)
	streamingHelper := h.NewStreamingTestHelper(ctx, t, "common", streamingClient)
	defer streamingHelper.Close()

	// Setup only bidirectional streaming
	if err := streamingHelper.GetStreamingClient().EstablishBidirectionalStreaming(); err != nil {
		t.Fatalf("Failed to setup bidirectional streaming: %v", err)
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

	t.Run("Poll for ALLOCATED state", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: time.Second, Wait: 200 * time.Millisecond}, t, func(r *h.R) {
			stateReq := &aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}

			resp, err := streamingHelper.GetStreamingClient().SendRequest(
				fmt.Sprintf("get-state-%d", time.Now().UnixNano()),
				"ApplicationServiceGetStateRequest",
				stateReq,
			)
			if err != nil {
				r.Fatal("Failed to send state request:", err)
			}

			if resp.Error != nil {
				r.Fatalf("Get state failed: %s - %s", resp.Error.Code, resp.Error.Message)
			}

			var stateResp aquariumv2.ApplicationServiceGetStateResponse
			if err := resp.ResponseData.UnmarshalTo(&stateResp); err != nil {
				r.Fatal("Failed to unmarshal state response:", err)
			}

			if stateResp.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", stateResp.Data.Status)
			}
		})

		t.Log("Application reached ALLOCATED state")
	})

	t.Run("Get Resource", func(t *testing.T) {
		resourceReq := &aquariumv2.ApplicationServiceGetResourceRequest{
			ApplicationUid: appUID,
		}

		resp, err := streamingHelper.SendRequestAndExpectSuccess(
			"get-resource",
			"ApplicationServiceGetResourceRequest",
			resourceReq,
			"ApplicationServiceGetResourceResponse",
		)
		if err != nil {
			t.Fatal("Failed to get resource:", err)
		}

		var resourceResp aquariumv2.ApplicationServiceGetResourceResponse
		if err := resp.ResponseData.UnmarshalTo(&resourceResp); err != nil {
			t.Fatal("Failed to unmarshal resource response:", err)
		}

		if resourceResp.Data.Identifier == "" {
			t.Fatal("Resource identifier is empty")
		}

		t.Logf("Got resource with identifier: %s", resourceResp.Data.Identifier)
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

	t.Run("Poll for DEALLOCATED state", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: time.Second, Wait: 200 * time.Millisecond}, t, func(r *h.R) {
			stateReq := &aquariumv2.ApplicationServiceGetStateRequest{
				ApplicationUid: appUID,
			}

			resp, err := streamingHelper.GetStreamingClient().SendRequest(
				fmt.Sprintf("get-final-state-%d", time.Now().UnixNano()),
				"ApplicationServiceGetStateRequest",
				stateReq,
			)
			if err != nil {
				r.Fatal("Failed to send final state request:", err)
			}

			if resp.Error != nil {
				r.Fatalf("Get final state failed: %s - %s", resp.Error.Code, resp.Error.Message)
			}

			var stateResp aquariumv2.ApplicationServiceGetStateResponse
			if err := resp.ResponseData.UnmarshalTo(&stateResp); err != nil {
				r.Fatal("Failed to unmarshal final state response:", err)
			}

			if stateResp.Data.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Final Application Status is incorrect: %v", stateResp.Data.Status)
			}
		})

		t.Log("Application reached DEALLOCATED state")
	})

	t.Log("Bidirectional streaming test completed successfully!")
}
