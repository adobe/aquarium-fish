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

package helper

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
)

// StreamingClient provides a high-level interface for streaming operations
type StreamingClient struct {
	t               *testing.T
	ctx             context.Context
	streamingClient aquariumv2connect.StreamingServiceClient

	// Bidirectional streaming
	bidirectionalStream *connect.BidiStreamForClient[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
	responses           map[string]*aquariumv2.StreamingServiceConnectResponse
	responsesMutex      sync.RWMutex
	responseWg          sync.WaitGroup

	// Subscription streaming
	subscriptions          map[aquariumv2.SubscriptionType]chan *aquariumv2.StreamingServiceSubscribeResponse
	subscriptionWg         sync.WaitGroup
	subscriptionCancelFunc context.CancelFunc
}

// NewStreamingClient creates a new streaming client with clean abstractions
func NewStreamingClient(t *testing.T, ctx context.Context, client aquariumv2connect.StreamingServiceClient) *StreamingClient {
	return &StreamingClient{
		t:               t,
		ctx:             ctx,
		streamingClient: client,
		responses:       make(map[string]*aquariumv2.StreamingServiceConnectResponse),
		subscriptions:   make(map[aquariumv2.SubscriptionType]chan *aquariumv2.StreamingServiceSubscribeResponse),
	}
}

// EstablishBidirectionalStreaming sets up bidirectional streaming for request/response operations
func (sc *StreamingClient) EstablishBidirectionalStreaming() error {
	sc.t.Log("Establishing bidirectional streaming connection...")

	sc.bidirectionalStream = sc.streamingClient.Connect(sc.ctx)

	// Start response handler goroutine
	sc.responseWg.Add(1)
	go func() {
		defer sc.responseWg.Done()
		defer func() {
			if r := recover(); r != nil {
				sc.t.Logf("Bidirectional stream goroutine recovered from panic: %v", r)
			}
		}()

		for {
			resp, err := sc.bidirectionalStream.Receive()
			if err != nil {
				if sc.ctx.Err() != nil {
					sc.t.Logf("Bidirectional stream context cancelled, stopping response handler")
					return
				}
				sc.t.Logf("Bidirectional stream ended with error: %v", err)
				return
			}

			sc.t.Logf("Received bidirectional response - ID: %s, Type: %s", resp.RequestId, resp.ResponseType)

			sc.responsesMutex.Lock()
			sc.responses[resp.RequestId] = resp
			sc.responsesMutex.Unlock()
		}
	}()

	sc.t.Log("Bidirectional streaming connection established")
	return nil
}

// EstablishSubscriptionStreaming sets up server-side streaming for real-time notifications
func (sc *StreamingClient) EstablishSubscriptionStreaming(subscriptionTypes []aquariumv2.SubscriptionType) error {
	sc.t.Logf("Establishing subscription streaming for %d types...", len(subscriptionTypes))

	// Create channels for each subscription type
	for _, subType := range subscriptionTypes {
		sc.subscriptions[subType] = make(chan *aquariumv2.StreamingServiceSubscribeResponse, 100)
	}

	// Establish subscription
	subscribeReq := connect.NewRequest(&aquariumv2.StreamingServiceSubscribeRequest{
		SubscriptionTypes: subscriptionTypes,
	})

	// Create cancellable context for subscription
	subCtx, cancel := context.WithCancel(sc.ctx)
	sc.subscriptionCancelFunc = cancel

	stream, err := sc.streamingClient.Subscribe(subCtx, subscribeReq)
	if err != nil {
		return fmt.Errorf("failed to establish subscription: %w", err)
	}

	// Start notification dispatcher goroutine
	sc.subscriptionWg.Add(1)
	go func() {
		defer sc.subscriptionWg.Done()
		defer func() {
			if r := recover(); r != nil {
				sc.t.Logf("Subscription goroutine recovered from panic: %v", r)
			}
		}()

		for stream.Receive() {
			msg := stream.Msg()

			// Handle initial confirmation message
			if msg.ObjectType == aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED {
				sc.t.Logf("Received subscription confirmation from server")
				continue
			}

			// Dispatch to appropriate channel
			if channel, exists := sc.subscriptions[msg.ObjectType]; exists {
				select {
				case channel <- msg:
				case <-subCtx.Done():
					return
				}
			} else {
				sc.t.Logf("Received unexpected notification type: %s", msg.ObjectType)
			}
		}

		if err := stream.Err(); err != nil && subCtx.Err() == nil {
			sc.t.Logf("Subscription stream ended with error: %v", err)
		}
	}()

	sc.t.Log("Subscription streaming established successfully")
	return nil
}

// SendRequest sends a request through bidirectional streaming and waits for response
func (sc *StreamingClient) SendRequest(requestID, requestType string, requestData proto.Message) (*aquariumv2.StreamingServiceConnectResponse, error) {
	if sc.bidirectionalStream == nil {
		return nil, fmt.Errorf("bidirectional streaming not established")
	}

	// Convert request data to Any
	anyData, err := anypb.New(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to convert request data to Any: %w", err)
	}

	req := &aquariumv2.StreamingServiceConnectRequest{
		RequestId:   requestID,
		RequestType: requestType,
		RequestData: anyData,
	}

	sc.t.Logf("Sending streaming request - ID: %s, Type: %s", requestID, requestType)

	// Send request
	if err := sc.bidirectionalStream.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send streaming request: %w", err)
	}

	// Wait for response with timeout
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-timeout.C:
			return nil, fmt.Errorf("timeout waiting for response to request %s", requestID)
		case <-time.After(100 * time.Millisecond):
			sc.responsesMutex.RLock()
			resp, ok := sc.responses[requestID]
			sc.responsesMutex.RUnlock()
			if ok {
				sc.t.Logf("Received response for request - ID: %s", requestID)
				return resp, nil
			}
		}
	}
}

// WaitForNotification waits for a specific notification type with optional filtering
func (sc *StreamingClient) WaitForNotification(
	subscriptionType aquariumv2.SubscriptionType,
	timeout time.Duration,
	filter func(*aquariumv2.StreamingServiceSubscribeResponse) bool,
) (*aquariumv2.StreamingServiceSubscribeResponse, error) {

	channel, exists := sc.subscriptions[subscriptionType]
	if !exists {
		return nil, fmt.Errorf("subscription type %s not established", subscriptionType)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for %s notification", subscriptionType)
		case notification := <-channel:
			sc.t.Logf("Received %s notification: %s", subscriptionType, notification.ChangeType)

			// Apply filter if provided
			if filter == nil || filter(notification) {
				return notification, nil
			}
		}
	}
}

// WaitForApplicationState waits for a specific application state notification
func (sc *StreamingClient) WaitForApplicationState(expectedState aquariumv2.ApplicationState_Status, timeout time.Duration) (*aquariumv2.ApplicationState, error) {
	sc.t.Logf("Waiting for application state: %s", expectedState)

	notification, err := sc.WaitForNotification(
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
		timeout,
		func(n *aquariumv2.StreamingServiceSubscribeResponse) bool {
			if n.ChangeType != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
				return false
			}

			var appState aquariumv2.ApplicationState
			if err := n.ObjectData.UnmarshalTo(&appState); err != nil {
				sc.t.Logf("Failed to unmarshal application state: %v", err)
				return false
			}

			sc.t.Logf("  Application state: %s", appState.Status)
			return appState.Status == expectedState
		},
	)

	if err != nil {
		return nil, err
	}

	var appState aquariumv2.ApplicationState
	if err := notification.ObjectData.UnmarshalTo(&appState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal application state: %w", err)
	}

	sc.t.Logf("Successfully received %s state notification!", expectedState)
	return &appState, nil
}

// WaitForApplicationResource waits for an application resource notification
func (sc *StreamingClient) WaitForApplicationResource(timeout time.Duration) (*aquariumv2.ApplicationResource, error) {
	sc.t.Log("Waiting for application resource notification...")

	notification, err := sc.WaitForNotification(
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE,
		timeout,
		func(n *aquariumv2.StreamingServiceSubscribeResponse) bool {
			return n.ChangeType == aquariumv2.ChangeType_CHANGE_TYPE_CREATED
		},
	)

	if err != nil {
		return nil, err
	}

	var appResource aquariumv2.ApplicationResource
	if err := notification.ObjectData.UnmarshalTo(&appResource); err != nil {
		return nil, fmt.Errorf("failed to unmarshal application resource: %w", err)
	}

	sc.t.Logf("Successfully received resource notification! Identifier: %s", appResource.Identifier)
	return &appResource, nil
}

// WaitForApplicationTask waits for an application task notification
func (sc *StreamingClient) WaitForApplicationTask(timeout time.Duration) (*aquariumv2.ApplicationTask, error) {
	sc.t.Log("Waiting for application task notification...")

	notification, err := sc.WaitForNotification(
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK,
		timeout,
		func(n *aquariumv2.StreamingServiceSubscribeResponse) bool {
			return n.ChangeType == aquariumv2.ChangeType_CHANGE_TYPE_CREATED
		},
	)

	if err != nil {
		return nil, err
	}

	var appTask aquariumv2.ApplicationTask
	if err := notification.ObjectData.UnmarshalTo(&appTask); err != nil {
		return nil, fmt.Errorf("failed to unmarshal application task: %w", err)
	}

	sc.t.Logf("Successfully received task notification! Application UID: %s", appTask.ApplicationUid)
	return &appTask, nil
}

// GetStateNotifications returns the channel for application state notifications
func (sc *StreamingClient) GetStateNotifications() <-chan *aquariumv2.StreamingServiceSubscribeResponse {
	if channel, exists := sc.subscriptions[aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE]; exists {
		return channel
	}
	// Return empty channel if not subscribed
	empty := make(chan *aquariumv2.StreamingServiceSubscribeResponse)
	close(empty)
	return empty
}

// GetResourceNotifications returns the channel for application resource notifications
func (sc *StreamingClient) GetResourceNotifications() <-chan *aquariumv2.StreamingServiceSubscribeResponse {
	if channel, exists := sc.subscriptions[aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE]; exists {
		return channel
	}
	// Return empty channel if not subscribed
	empty := make(chan *aquariumv2.StreamingServiceSubscribeResponse)
	close(empty)
	return empty
}

// GetTaskNotifications returns the channel for application task notifications
func (sc *StreamingClient) GetTaskNotifications() <-chan *aquariumv2.StreamingServiceSubscribeResponse {
	if channel, exists := sc.subscriptions[aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK]; exists {
		return channel
	}
	// Return empty channel if not subscribed
	empty := make(chan *aquariumv2.StreamingServiceSubscribeResponse)
	close(empty)
	return empty
}

// Close gracefully closes all streaming connections
func (sc *StreamingClient) Close() {
	sc.t.Log("Closing streaming connections...")

	// Close bidirectional stream
	if sc.bidirectionalStream != nil {
		if err := sc.bidirectionalStream.CloseRequest(); err != nil {
			sc.t.Logf("Error closing bidirectional stream: %v", err)
		}
	}

	// Cancel subscription context
	if sc.subscriptionCancelFunc != nil {
		sc.subscriptionCancelFunc()
	}

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		sc.responseWg.Wait()
		sc.subscriptionWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		sc.t.Log("All streaming goroutines finished successfully")
	case <-time.After(3 * time.Second):
		sc.t.Log("Timeout waiting for streaming goroutines to finish (this is acceptable)")
	}

	// Close subscription channels
	for _, ch := range sc.subscriptions {
		close(ch)
	}
}

// StreamingTestHelper provides an even higher-level interface for common test patterns
type StreamingTestHelper struct {
	sc *StreamingClient
	t  *testing.T
}

// NewStreamingTestHelper creates a new test helper with streamlined setup
func NewStreamingTestHelper(t *testing.T, ctx context.Context, client aquariumv2connect.StreamingServiceClient) *StreamingTestHelper {
	return &StreamingTestHelper{
		sc: NewStreamingClient(t, ctx, client),
		t:  t,
	}
}

// SetupFullStreaming establishes both bidirectional and subscription streaming
func (sth *StreamingTestHelper) SetupFullStreaming(subscriptionTypes []aquariumv2.SubscriptionType) error {
	if err := sth.sc.EstablishBidirectionalStreaming(); err != nil {
		return fmt.Errorf("failed to establish bidirectional streaming: %w", err)
	}

	if err := sth.sc.EstablishSubscriptionStreaming(subscriptionTypes); err != nil {
		return fmt.Errorf("failed to establish subscription streaming: %w", err)
	}

	return nil
}

// SendRequestAndExpectSuccess sends a request and expects a successful response
func (sth *StreamingTestHelper) SendRequestAndExpectSuccess(requestID, requestType string, requestData proto.Message, expectedResponseType string) (*aquariumv2.StreamingServiceConnectResponse, error) {
	resp, err := sth.sc.SendRequest(requestID, requestType, requestData)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("request failed: %s - %s", resp.Error.Code, resp.Error.Message)
	}

	if resp.ResponseType != expectedResponseType {
		return nil, fmt.Errorf("unexpected response type: %s (expected %s)", resp.ResponseType, expectedResponseType)
	}

	return resp, nil
}

// GetStreamingClient returns the underlying streaming client for advanced operations
func (sth *StreamingTestHelper) GetStreamingClient() *StreamingClient {
	return sth.sc
}

// Close closes all streaming connections
func (sth *StreamingTestHelper) Close() {
	sth.sc.Close()
}
