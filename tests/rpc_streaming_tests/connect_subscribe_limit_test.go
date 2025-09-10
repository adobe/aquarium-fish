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
	"testing"
	"time"

	"connectrpc.com/connect"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_connect_subscribe_limit_default tests the default stream limit behavior (limit = 1)
func Test_connect_subscribe_limit_default(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("Test Connect stream limit (default=1)", func(t *testing.T) {
		// Create first connection
		firstStream := streamingClient.Connect(ctx)
		defer firstStream.CloseRequest()

		// Send a keep-alive to ensure connection is established
		err := firstStream.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-1",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("Failed to send keep-alive on first connection:", err)
		}

		// Receive confirmation
		resp1, err := firstStream.Receive()
		if err != nil {
			t.Fatal("Failed to receive from first connection:", err)
		}
		if resp1.RequestId != "keepalive-1" && resp1.RequestId != "keep-alive" {
			t.Log("Received response for first connection:", resp1.RequestId)
		}

		// Create second connection - this should terminate the first one
		secondStream := streamingClient.Connect(ctx)
		if err != nil {
			t.Fatal("Failed to create second connection:", err)
		}
		defer secondStream.CloseRequest()

		// Send keep-alive to second connection
		err = secondStream.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("Failed to send keep-alive on second connection:", err)
		}

		// The first connection should receive a termination message
		terminationReceived := false
		for range 3 { // Try a few times in case there are keep-alives
			resp, err := firstStream.Receive()
			if err != nil {
				if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "canceled") {
					terminationReceived = true
					break
				}
				t.Log("Error receiving from first connection (expected):", err)
				terminationReceived = true
				break
			}
			if resp.Error != nil && strings.Contains(resp.Error.Message, "Stream limit exceeded") {
				terminationReceived = true
				t.Log("Received expected termination message:", resp.Error.Message)
				break
			}
		}

		if !terminationReceived {
			t.Error("First connection was not terminated as expected")
		}

		// Second connection should still work
		resp2, err := secondStream.Receive()
		if err != nil {
			t.Fatal("Second connection failed:", err)
		}
		if resp2.RequestId != "keepalive-2" && resp2.RequestId != "keep-alive" {
			t.Log("Received response for second connection:", resp2.RequestId)
		}

		t.Log("Connect stream limit test passed - old connection was terminated")
	})

	t.Run("Test Subscribe stream limit (default=1)", func(t *testing.T) {
		subscriptionTypes := []aquariumv2.SubscriptionType{
			aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
		}

		// Create first subscription
		firstSub, err := streamingClient.Subscribe(ctx, connect.NewRequest(&aquariumv2.StreamingServiceSubscribeRequest{
			SubscriptionTypes: subscriptionTypes,
		}))
		if err != nil {
			t.Fatal("Failed to create first subscription:", err)
		}
		defer firstSub.Close()

		// Wait for first subscription confirmation
		firstSub.Receive()
		resp1 := firstSub.Msg()
		if resp1.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
			t.Error("Expected subscription creation confirmation")
		}

		// Create second subscription - this should terminate the first one
		secondSub, err := streamingClient.Subscribe(ctx, connect.NewRequest(&aquariumv2.StreamingServiceSubscribeRequest{
			SubscriptionTypes: subscriptionTypes,
		}))
		if err != nil {
			t.Fatal("Failed to create second subscription:", err)
		}
		defer secondSub.Close()

		// The first subscription should receive a termination message
		terminationReceived := false
		for range 3 { // Try a few times
			firstSub.Receive()
			resp := firstSub.Msg()
			if resp.ChangeType == aquariumv2.ChangeType_CHANGE_TYPE_REMOVED {
				terminationReceived = true
				t.Log("Received expected termination for first subscription")
				break
			}
		}

		if !terminationReceived {
			t.Error("First subscription was not terminated as expected")
		}

		// Second subscription should receive confirmation
		secondSub.Receive()
		resp2 := secondSub.Msg()
		if err != nil {
			t.Fatal("Second subscription failed:", err)
		}
		if resp2.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
			t.Error("Expected second subscription creation confirmation")
		}

		t.Log("Subscribe stream limit test passed - old subscription was terminated")
	})

	t.Log("Default stream limit tests completed successfully!")
}

// Test_connect_subscribe_limit_custom tests custom stream limit configurations
func Test_connect_subscribe_limit_custom(t *testing.T) {
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

	// Create user service client
	userClient := aquariumv2connect.NewUserServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Run("Test unlimited streams (limit=-1)", func(t *testing.T) {
		// Create a user with unlimited stream limit
		testUser := "unlimited-user"
		streamsLimit := int32(-1) // unlimited
		createReq := &aquariumv2.UserServiceCreateRequest{
			User: &aquariumv2.User{
				Name:  testUser,
				Roles: []string{"User"},
				Config: &aquariumv2.UserConfig{
					StreamsLimit: &streamsLimit,
				},
			},
		}

		userResp, err := userClient.Create(ctx, connect.NewRequest(createReq))
		if err != nil {
			t.Fatal("Failed to create unlimited user:", err)
		}

		// Create client for the unlimited user
		unlimitedCli, unlimitedOpts := h.NewRPCClient(testUser, userResp.Msg.GetData().GetPassword(), h.RPCClientGRPC, afi.GetCA(t))
		unlimitedStreamingClient := aquariumv2connect.NewStreamingServiceClient(
			unlimitedCli,
			afi.APIAddress("grpc"),
			unlimitedOpts...,
		)

		// Create multiple connections - all should succeed
		var connections []*connect.BidiStreamForClient[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
		for i := range 3 {
			conn := unlimitedStreamingClient.Connect(ctx)
			connections = append(connections, conn)

			// Send keep-alive
			err = conn.Send(&aquariumv2.StreamingServiceConnectRequest{
				RequestId:   fmt.Sprintf("keepalive-%d", i+1),
				RequestType: "KeepAliveRequest",
			})
			if err != nil {
				t.Fatalf("Failed to send keep-alive on connection %d: %v", i+1, err)
			}
		}

		// All connections should be working
		for i, conn := range connections {
			_, err := conn.Receive()
			if err != nil {
				t.Fatalf("Connection %d failed: %v", i+1, err)
			}
		}

		// Clean up
		for _, conn := range connections {
			conn.CloseRequest()
		}

		t.Log("Unlimited streams test passed - all connections were allowed")
	})

	t.Run("Test no streams allowed (limit=0)", func(t *testing.T) {
		// Create a user with no streaming allowed
		testUser := "no-stream-user"
		streamsLimit := int32(0) // no streaming
		createReq := &aquariumv2.UserServiceCreateRequest{
			User: &aquariumv2.User{
				Name:  testUser,
				Roles: []string{"User"},
				Config: &aquariumv2.UserConfig{
					StreamsLimit: &streamsLimit,
				},
			},
		}

		userResp, err := userClient.Create(ctx, connect.NewRequest(createReq))
		if err != nil {
			t.Fatal("Failed to create no-stream user:", err)
		}

		// Create client for the no-stream user
		noStreamCli, noStreamOpts := h.NewRPCClient(testUser, userResp.Msg.GetData().GetPassword(), h.RPCClientGRPC, afi.GetCA(t))
		noStreamStreamingClient := aquariumv2connect.NewStreamingServiceClient(
			noStreamCli,
			afi.APIAddress("grpc"),
			noStreamOpts...,
		)

		// Creating connection, which will be closed by the Fish
		conn := noStreamStreamingClient.Connect(ctx)
		defer conn.CloseRequest()
		// Send request to confirm it's not connected
		err = conn.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive",
			RequestType: "KeepAliveRequest",
		})
		if err == nil {
			t.Log("Succeded to send keep-alive on connection")
		}

		// Calling receive to get an error, it will not fail just on Connect
		_, err = conn.Receive()
		if err != nil {
			t.Logf("No-stream test passed - connection was rejected as expected: %v", err)
		} else {
			t.Error("Expected connection to fail for no-stream user, but it succeeded")
		}

		// Try to create subscription - should also fail
		subs, _ := noStreamStreamingClient.Subscribe(ctx, connect.NewRequest(&aquariumv2.StreamingServiceSubscribeRequest{
			SubscriptionTypes: []aquariumv2.SubscriptionType{
				aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
			},
		}))
		// Calling receive to get an error of the stream, it will not fail just on Subscribe
		b := subs.Receive()
		err = subs.Err()
		if b != false || err == nil {
			t.Errorf("Expected subscription to fail for no-stream user, but it succeeded: %v, %v", b, err)
		} else {
			t.Logf("No-stream test passed - subscribe was rejected as expected: %v", err)
		}
	})

	t.Run("Test custom limit (limit=2)", func(t *testing.T) {
		// Create a user with limit of 2 streams
		testUser := "limit-2-user"
		streamsLimit := int32(2)
		createReq := &aquariumv2.UserServiceCreateRequest{
			User: &aquariumv2.User{
				Name:  testUser,
				Roles: []string{"User"},
				Config: &aquariumv2.UserConfig{
					StreamsLimit: &streamsLimit,
				},
			},
		}

		userResp, err := userClient.Create(ctx, connect.NewRequest(createReq))
		if err != nil {
			t.Fatal("Failed to create limit-2 user:", err)
		}

		// Create client for the limit-2 user
		limit2Cli, limit2Opts := h.NewRPCClient(testUser, userResp.Msg.GetData().GetPassword(), h.RPCClientGRPC, afi.GetCA(t))
		limit2StreamingClient := aquariumv2connect.NewStreamingServiceClient(
			limit2Cli,
			afi.APIAddress("grpc"),
			limit2Opts...,
		)

		// Create first two connections - should succeed
		conn1 := limit2StreamingClient.Connect(ctx)
		defer conn1.CloseRequest()

		conn2 := limit2StreamingClient.Connect(ctx)
		defer conn2.CloseRequest()

		// Send keep-alives to both
		err = conn1.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-1",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("Failed to send keep-alive on first connection:", err)
		}

		err = conn2.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("Failed to send keep-alive on second connection:", err)
		}

		// Both should work
		_, err = conn1.Receive()
		if err != nil {
			t.Error("First connection failed:", err)
		}

		_, err = conn2.Receive()
		if err != nil {
			t.Error("Second connection failed:", err)
		}

		// Create third connection - should terminate the first one
		conn3 := limit2StreamingClient.Connect(ctx)
		defer conn3.CloseRequest()

		err = conn3.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-3",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("Failed to send keep-alive on third connection:", err)
		}

		// First connection should be terminated
		terminationReceived := false
		for range 3 {
			resp, err := conn1.Receive()
			if err != nil {
				terminationReceived = true
				t.Log("First connection terminated as expected:", err)
				break
			} else {
				t.Logf("First connection response: %#v", resp)
			}
		}

		if !terminationReceived {
			t.Error("First connection was not terminated when limit was exceeded")
		}

		// Second and third connections should still work
		err = conn2.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2-check",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Error("Second connection failed after third was created:", err)
		}

		err = conn3.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-3",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Error("Third connection failed:", err)
		}

		// Both should work
		_, err = conn2.Receive()
		if err != nil {
			t.Error("First connection failed:", err)
		}

		_, err = conn3.Receive()
		if err != nil {
			t.Error("Second connection failed:", err)
		}

		t.Log("Custom limit test passed - oldest connection was terminated when limit exceeded")
	})

	t.Log("Custom stream limit tests completed successfully!")
}
