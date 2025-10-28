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
			t.Fatal("FATAL: Failed to send keep-alive on first connection:", err)
		}

		// Receive confirmation
		resp1, err := firstStream.Receive()
		if err != nil {
			t.Fatal("FATAL: Failed to receive from first connection:", err)
		}
		if resp1.RequestId != "keepalive-1" && resp1.RequestId != "keep-alive" {
			t.Log("Received response for first connection:", resp1.RequestId)
		}

		// Create second connection - this should terminate the first one
		secondStream := streamingClient.Connect(ctx)
		if err != nil {
			t.Fatal("FATAL: Failed to create second connection:", err)
		}
		defer secondStream.CloseRequest()

		// Send keep-alive to second connection
		err = secondStream.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("FATAL: Failed to send keep-alive on second connection:", err)
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
			t.Error("ERROR: First connection was not terminated as expected")
		}

		// Second connection should still work
		resp2, err := secondStream.Receive()
		if err != nil {
			t.Fatal("FATAL: Second connection failed:", err)
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
			t.Fatal("FATAL: Failed to create first subscription:", err)
		}
		defer firstSub.Close()

		// Wait for first subscription confirmation
		firstSub.Receive()
		resp1 := firstSub.Msg()
		if resp1.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
			t.Error("ERROR: Expected subscription creation confirmation")
		}

		// Create second subscription - this should terminate the first one
		secondSub, err := streamingClient.Subscribe(ctx, connect.NewRequest(&aquariumv2.StreamingServiceSubscribeRequest{
			SubscriptionTypes: subscriptionTypes,
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to create second subscription:", err)
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
			t.Error("ERROR: First subscription was not terminated as expected")
		}

		// Second subscription should receive confirmation
		secondSub.Receive()
		resp2 := secondSub.Msg()
		if err != nil {
			t.Fatal("FATAL: Second subscription failed:", err)
		}
		if resp2.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
			t.Error("ERROR: Expected second subscription creation confirmation")
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
			t.Fatal("FATAL: Failed to create unlimited user:", err)
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
				t.Fatalf("FATAL: Failed to send keep-alive on connection %d: %v", i+1, err)
			}
		}

		// All connections should be working
		for i, conn := range connections {
			_, err := conn.Receive()
			if err != nil {
				t.Fatalf("FATAL: Connection %d failed: %v", i+1, err)
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
			t.Fatal("FATAL: Failed to create no-stream user:", err)
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
			t.Error("ERROR: Expected connection to fail for no-stream user, but it succeeded")
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
			t.Errorf("ERROR: Expected subscription to fail for no-stream user, but it succeeded: %v, %v", b, err)
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
			t.Fatal("FATAL: Failed to create limit-2 user:", err)
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
			t.Fatal("FATAL: Failed to send keep-alive on first connection:", err)
		}

		err = conn2.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("FATAL: Failed to send keep-alive on second connection:", err)
		}

		// Both should work
		_, err = conn1.Receive()
		if err != nil {
			t.Error("ERROR: First connection failed:", err)
		}

		_, err = conn2.Receive()
		if err != nil {
			t.Error("ERROR: Second connection failed:", err)
		}

		t.Log("Two first connections are fine, creating third one")

		// Create third connection - should terminate the first one
		conn3 := limit2StreamingClient.Connect(ctx)
		defer conn3.CloseRequest()

		err = conn3.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-3",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("FATAL: Failed to send keep-alive on third connection:", err)
		}

		t.Log("Created third connection")

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
			t.Error("ERROR: First connection was not terminated when limit was exceeded")
		}

		// Second and third connections should still work
		err = conn2.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2-check",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Error("ERROR: Second connection failed after third was created:", err)
		}

		err = conn3.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-3",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Error("ERROR: Third connection failed:", err)
		}

		// Both should work
		_, err = conn2.Receive()
		if err != nil {
			t.Error("ERROR: First connection failed:", err)
		}

		_, err = conn3.Receive()
		if err != nil {
			t.Error("ERROR: Second connection failed:", err)
		}

		t.Log("Custom limit test passed - oldest connection was terminated when limit exceeded")
	})

	t.Log("Custom stream limit tests completed successfully!")
}

// Test_user_group_stream_limit_fallback tests that user inherits stream limit from user group
func Test_user_group_stream_limit_fallback(t *testing.T) {
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

	// Create a user group with stream limit
	groupName := "stream-group"
	streamsLimit := int32(2)

	t.Run("Create user group with stream limit", func(t *testing.T) {
		_, err := userClient.CreateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceCreateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   groupName,
				Users:  []string{},
				Config: &aquariumv2.UserConfig{StreamsLimit: &streamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to create user group:", err)
		}
	})

	// Create a test user without stream limit config
	testUser := "streamgroupuser"
	testPassword := "testpass123"

	t.Run("Create test user without stream limit", func(t *testing.T) {
		createReq := &aquariumv2.UserServiceCreateRequest{
			User: &aquariumv2.User{
				Name:     testUser,
				Password: &testPassword,
				Roles:    []string{"User"},
				// No config set - should inherit from group
			},
		}

		_, err := userClient.Create(ctx, connect.NewRequest(createReq))
		if err != nil {
			t.Fatal("FATAL: Failed to create test user:", err)
		}
	})

	// Add user to the group
	t.Run("Add user to group", func(t *testing.T) {
		_, err := userClient.UpdateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceUpdateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   groupName,
				Users:  []string{testUser},
				Config: &aquariumv2.UserConfig{StreamsLimit: &streamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to update user group:", err)
		}
	})

	// Create client for the test user
	testCli, testOpts := h.NewRPCClient(testUser, testPassword, h.RPCClientGRPC, afi.GetCA(t))
	testStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		testCli,
		afi.APIAddress("grpc"),
		testOpts...,
	)

	t.Run("User inherits stream limit from group", func(t *testing.T) {
		// Create first two connections - should succeed
		conn1 := testStreamingClient.Connect(ctx)
		defer conn1.CloseRequest()

		conn2 := testStreamingClient.Connect(ctx)
		defer conn2.CloseRequest()

		// Send keep-alives to both
		err := conn1.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-1",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("FATAL: Failed to send keep-alive on first connection:", err)
		}

		err = conn2.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-2",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("FATAL: Failed to send keep-alive on second connection:", err)
		}

		// Both should work
		_, err = conn1.Receive()
		if err != nil {
			t.Error("ERROR: First connection failed:", err)
		}

		_, err = conn2.Receive()
		if err != nil {
			t.Error("ERROR: Second connection failed:", err)
		}

		// Create third connection - should terminate the first one
		conn3 := testStreamingClient.Connect(ctx)
		defer conn3.CloseRequest()

		err = conn3.Send(&aquariumv2.StreamingServiceConnectRequest{
			RequestId:   "keepalive-3",
			RequestType: "KeepAliveRequest",
		})
		if err != nil {
			t.Fatal("FATAL: Failed to send keep-alive on third connection:", err)
		}

		// First connection should be terminated
		terminationReceived := false
		for range 3 {
			_, err := conn1.Receive()
			if err != nil {
				terminationReceived = true
				t.Log("First connection terminated as expected (group limit=2):", err)
				break
			}
		}

		if !terminationReceived {
			t.Error("ERROR: First connection was not terminated when group limit was exceeded")
		}

		t.Log("User successfully inherited stream limit from group")
	})
}

// Test_user_group_stream_limit_precedence tests that user's own config takes precedence over group
func Test_user_group_stream_limit_precedence(t *testing.T) {
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

	// Create a user group with low stream limit
	groupName := "low-stream-group"
	groupStreamsLimit := int32(1)

	t.Run("Create user group with low stream limit", func(t *testing.T) {
		_, err := userClient.CreateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceCreateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   groupName,
				Users:  []string{},
				Config: &aquariumv2.UserConfig{StreamsLimit: &groupStreamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to create user group:", err)
		}
	})

	// Create a test user with higher stream limit
	testUser := "streamprecedenceuser"
	userStreamsLimit := int32(3) // Higher than group limit
	createReq := &aquariumv2.UserServiceCreateRequest{
		User: &aquariumv2.User{
			Name:  testUser,
			Roles: []string{"User"},
			Config: &aquariumv2.UserConfig{
				StreamsLimit: &userStreamsLimit,
			},
		},
	}

	userResp, err := userClient.Create(ctx, connect.NewRequest(createReq))
	if err != nil {
		t.Fatal("FATAL: Failed to create test user:", err)
	}
	testPassword := userResp.Msg.GetData().GetPassword()

	// Add user to the group
	t.Run("Add user to group", func(t *testing.T) {
		_, err := userClient.UpdateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceUpdateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   groupName,
				Users:  []string{testUser},
				Config: &aquariumv2.UserConfig{StreamsLimit: &groupStreamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to update user group:", err)
		}
	})

	// Create client for the test user
	testCli, testOpts := h.NewRPCClient(testUser, testPassword, h.RPCClientGRPC, afi.GetCA(t))
	testStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		testCli,
		afi.APIAddress("grpc"),
		testOpts...,
	)

	t.Run("User's own config takes precedence over group", func(t *testing.T) {
		// Create connections up to user's limit (3)
		var connections []*connect.BidiStreamForClient[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
		for i := range 3 {
			conn := testStreamingClient.Connect(ctx)
			connections = append(connections, conn)

			// Send keep-alive
			err = conn.Send(&aquariumv2.StreamingServiceConnectRequest{
				RequestId:   fmt.Sprintf("keepalive-%d", i+1),
				RequestType: "KeepAliveRequest",
			})
			if err != nil {
				t.Fatalf("FATAL: Failed to send keep-alive on connection %d: %v", i+1, err)
			}
		}

		// All connections should be working (more than group limit of 1)
		for i, conn := range connections {
			_, err := conn.Receive()
			if err != nil {
				t.Fatalf("FATAL: Connection %d failed: %v", i+1, err)
			}
		}

		// Clean up
		for _, conn := range connections {
			conn.CloseRequest()
		}

		t.Logf("User successfully used own stream limit (%d) instead of group limit (%d)", userStreamsLimit, groupStreamsLimit)
	})
}

// Test_user_group_stream_limit_max_value tests that max value from multiple groups is used
func Test_user_group_stream_limit_max_value(t *testing.T) {
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

	// Create two user groups with different stream limits
	group1Name := "low-stream-group-1"
	group1StreamsLimit := int32(1)

	group2Name := "high-stream-group-2"
	group2StreamsLimit := int32(2) // Higher limit

	t.Run("Create first user group with low stream limit", func(t *testing.T) {
		_, err := userClient.CreateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceCreateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   group1Name,
				Users:  []string{},
				Config: &aquariumv2.UserConfig{StreamsLimit: &group1StreamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to create first user group:", err)
		}
	})

	t.Run("Create second user group with higher stream limit", func(t *testing.T) {
		_, err := userClient.CreateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceCreateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   group2Name,
				Users:  []string{},
				Config: &aquariumv2.UserConfig{StreamsLimit: &group2StreamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to create second user group:", err)
		}
	})

	// Create a test user without stream limit config
	testUser := "multistreamgroupuser"
	createReq := &aquariumv2.UserServiceCreateRequest{
		User: &aquariumv2.User{
			Name:  testUser,
			Roles: []string{"User"},
			// No config set - should inherit max from groups
		},
	}

	userResp, err := userClient.Create(ctx, connect.NewRequest(createReq))
	if err != nil {
		t.Fatal("FATAL: Failed to create test user:", err)
	}
	testPassword := userResp.Msg.GetData().GetPassword()

	// Add user to both groups
	t.Run("Add user to first group", func(t *testing.T) {
		_, err := userClient.UpdateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceUpdateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   group1Name,
				Users:  []string{testUser},
				Config: &aquariumv2.UserConfig{StreamsLimit: &group1StreamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to update first user group:", err)
		}
	})

	t.Run("Add user to second group", func(t *testing.T) {
		_, err := userClient.UpdateGroup(ctx, connect.NewRequest(&aquariumv2.UserServiceUpdateGroupRequest{
			Usergroup: &aquariumv2.UserGroup{
				Name:   group2Name,
				Users:  []string{testUser},
				Config: &aquariumv2.UserConfig{StreamsLimit: &group2StreamsLimit},
			},
		}))
		if err != nil {
			t.Fatal("FATAL: Failed to update second user group:", err)
		}
	})

	// Create client for the test user
	testCli, testOpts := h.NewRPCClient(testUser, testPassword, h.RPCClientGRPC, afi.GetCA(t))
	testStreamingClient := aquariumv2connect.NewStreamingServiceClient(
		testCli,
		afi.APIAddress("grpc"),
		testOpts...,
	)

	t.Run("User uses max stream limit from multiple groups", func(t *testing.T) {
		// Create connections up to the higher group limit (2)
		var connections []*connect.BidiStreamForClient[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
		for i := range 2 {
			conn := testStreamingClient.Connect(ctx)
			connections = append(connections, conn)

			// Send keep-alive
			err = conn.Send(&aquariumv2.StreamingServiceConnectRequest{
				RequestId:   fmt.Sprintf("keepalive-%d", i+1),
				RequestType: "KeepAliveRequest",
			})
			if err != nil {
				t.Fatalf("FATAL: Failed to send keep-alive on connection %d: %v", i+1, err)
			}
		}

		// All connections should work (more than lower group limit of 1)
		for i, conn := range connections {
			_, err := conn.Receive()
			if err != nil {
				t.Fatalf("FATAL: Connection %d failed: %v", i+1, err)
			}
		}

		// Clean up
		for _, conn := range connections {
			conn.CloseRequest()
		}

		t.Logf("User successfully used max group stream limit (%d) instead of lower limit (%d)", group2StreamsLimit, group1StreamsLimit)
	})
}
