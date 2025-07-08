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
	"errors"
	"fmt"
	"io"
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
	name            string
	ctx             context.Context //nolint:containedctx // Is fine for tests
	streamingClient aquariumv2connect.StreamingServiceClient

	// Bidirectional streaming
	bidirectionalStream *connect.BidiStreamForClient[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
	responses           map[string]*aquariumv2.StreamingServiceConnectResponse
	responsesMutex      sync.RWMutex
	responseWg          sync.WaitGroup

	// Subscription streaming
	subMu                  sync.RWMutex
	subUID                 string
	subscriptions          map[aquariumv2.SubscriptionType]chan *aquariumv2.StreamingServiceSubscribeResponse
	subscriptionWg         sync.WaitGroup
	subscriptionCancelFunc context.CancelFunc
}

// NewStreamingClient creates a new streaming client with clean abstractions
func NewStreamingClient(ctx context.Context, t *testing.T, name string, client aquariumv2connect.StreamingServiceClient) *StreamingClient {
	t.Helper()
	return &StreamingClient{
		t:               t,
		name:            name,
		ctx:             ctx,
		streamingClient: client,
		responses:       make(map[string]*aquariumv2.StreamingServiceConnectResponse),
		subscriptions:   make(map[aquariumv2.SubscriptionType]chan *aquariumv2.StreamingServiceSubscribeResponse),
	}
}

func (sc *StreamingClient) logf(format string, v ...any) {
	sc.subMu.RLock()
	defer sc.subMu.RUnlock()
	args := append([]any{sc.name, sc.subUID}, v...)
	sc.t.Logf("Client %s:%s "+format, args...)
}

// EstablishBidirectionalStreaming sets up bidirectional streaming for request/response operations
func (sc *StreamingClient) EstablishBidirectionalStreaming() error {
	sc.t.Logf("Client %s: Establishing bidirectional streaming connection...", sc.name)

	sc.bidirectionalStream = sc.streamingClient.Connect(sc.ctx)

	// Start response handler goroutine
	sc.responseWg.Add(1)
	go func() {
		defer sc.responseWg.Done()
		defer func() {
			if r := recover(); r != nil {
				sc.t.Logf("Client %s: Bidirectional stream goroutine recovered from panic: %v", sc.name, r)
			}
		}()

		for {
			resp, err := sc.bidirectionalStream.Receive()
			if err != nil {
				if sc.ctx.Err() != nil {
					sc.t.Logf("Client %s: Bidirectional stream context cancelled, stopping response handler", sc.name)
					return
				}
				if errors.Is(err, io.EOF) {
					sc.t.Logf("Client %s: Bidirectional stream ended with EOF", sc.name)
				} else {
					sc.t.Logf("Client %s: Bidirectional stream ended with error: %v", sc.name, err)
				}
				return
			}

			// Handle keep-alive messages (ignore them)
			if resp.GetRequestId() == "keep-alive" && resp.GetResponseType() == "KeepAliveResponse" {
				sc.t.Logf("Client %s: Received keep-alive ping from server", sc.name)
				continue
			}

			// Handle server shutdown messages
			if resp.GetRequestId() == "server-shutdown" && resp.GetResponseType() == "ServerShutdownNotification" {
				sc.t.Logf("Client %s: Received server shutdown notification, closing connection gracefully", sc.name)
				return // Exit the receive loop to allow graceful closure
			}

			sc.t.Logf("Client %s: Received bidirectional response - ID: %s, Type: %s", sc.name, resp.GetRequestId(), resp.GetResponseType())

			sc.responsesMutex.Lock()
			sc.responses[resp.GetRequestId()] = resp
			sc.responsesMutex.Unlock()
		}
	}()

	sc.t.Logf("Client %s: Bidirectional streaming connection established", sc.name)
	return nil
}

// EstablishSubscriptionStreaming sets up server-side streaming for real-time notifications
func (sc *StreamingClient) EstablishSubscriptionStreaming(subscriptionTypes []aquariumv2.SubscriptionType) error {
	sc.t.Logf("Client %s: Establishing subscription streaming for %d types...", sc.name, len(subscriptionTypes))

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
				sc.logf("Subscription goroutine recovered from panic: %v", r)
			}
		}()

		for stream.Receive() {
			msg := stream.Msg()

			// Handle control messages (confirmation, shutdown, or buffer overflow)
			if msg.GetObjectType() == aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED {
				switch msg.GetChangeType() {
				case aquariumv2.ChangeType_CHANGE_TYPE_CREATED:
					// This is a subscription confirmation
					var streamCreated aquariumv2.StreamCreated
					if err := msg.GetObjectData().UnmarshalTo(&streamCreated); err != nil {
						sc.t.Logf("Client %s: ERROR: Unable to parse the StreamCreated data: %v", sc.name, err)
					} else {
						sc.subMu.Lock()
						sc.subUID = streamCreated.StreamUid
						sc.subMu.Unlock()
						sc.logf("Received subscription confirmation from server")
					}
					continue
				case aquariumv2.ChangeType_CHANGE_TYPE_UPDATED:
					// Is not used in control messages, skipping
				case aquariumv2.ChangeType_CHANGE_TYPE_UNSPECIFIED:
					// This is a buffer overflow disconnection notification
					sc.t.Errorf("Client %s:%s DISCONNECTED by server due to BUFFER OVERFLOW - client cannot keep up with notification rate!", sc.name, sc.subUID)
					return // Exit the receive loop - server is disconnecting us
				case aquariumv2.ChangeType_CHANGE_TYPE_DELETED:
					// This is a shutdown notification
					sc.logf("Received server shutdown notification for subscription, closing gracefully")
					return // Exit the receive loop to allow graceful closure
				default:
					// Unknown control message type
					sc.logf("Received unknown control message with ChangeType: %s", msg.GetChangeType())
					continue
				}
			}

			// Dispatch to appropriate channel
			if channel, exists := sc.subscriptions[msg.GetObjectType()]; exists {
				select {
				case channel <- msg:
					// Successfully sent notification
				case <-subCtx.Done():
					return
				default:
					// Channel is full, try to drain one old message and send the new one
					select {
					case <-channel:
						// Drained one old message, now try to send the new one
						select {
						case channel <- msg:
							sc.logf("Channel full, dropped old notification to make room for new %s", msg.GetObjectType())
						default:
							// Still can't send, drop the new message
							sc.logf("Channel overflowed, dropping %s notification", msg.GetObjectType())
						}
					default:
						// Can't even drain, drop the new message
						sc.logf("Channel completely blocked, dropping %s notification", msg.GetObjectType())
					}
				}
			} else {
				sc.logf("Received unexpected notification type: %s", msg.GetObjectType())
			}
		}

		if err := stream.Err(); err != nil && subCtx.Err() == nil {
			sc.logf("Subscription stream ended with error: %v", err)
		}
	}()

	sc.logf("Subscription streaming established successfully")
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

	sc.t.Logf("Client %s: Sending streaming request - ID: %s, Type: %s", sc.name, requestID, requestType)

	// Send request
	if err := sc.bidirectionalStream.Send(req); err != nil {
		return nil, fmt.Errorf("failed to send streaming request: %w", err)
	}

	// Wait for response with timeout (increased for heavy load testing)
	timeout := time.NewTimer(120 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-timeout.C:
			return nil, fmt.Errorf("timeout waiting for response to request %s", requestID)
		case <-sc.ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for response to request %s", requestID)
		case <-time.After(100 * time.Millisecond):
			sc.responsesMutex.RLock()
			resp, ok := sc.responses[requestID]
			sc.responsesMutex.RUnlock()
			if ok {
				sc.t.Logf("Client %s: Received response for request - ID: %s", sc.name, requestID)
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
			// Apply filter if provided
			if filter == nil || filter(notification) {
				return notification, nil
			}
		}
	}
}

// WaitForApplicationState waits for a specific application state notification for a specific application UID
func (sc *StreamingClient) WaitForApplicationState(applicationUID string, expectedState aquariumv2.ApplicationState_Status, timeout time.Duration) (*aquariumv2.ApplicationState, error) {
	sc.t.Logf("Client %s: Waiting for application state: %s for app: %s", sc.name, expectedState, applicationUID)

	notification, err := sc.WaitForNotification(
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE,
		timeout,
		func(n *aquariumv2.StreamingServiceSubscribeResponse) bool {
			if n.GetChangeType() != aquariumv2.ChangeType_CHANGE_TYPE_CREATED {
				return false
			}

			var appState aquariumv2.ApplicationState
			if err := n.GetObjectData().UnmarshalTo(&appState); err != nil {
				sc.logf("Failed to unmarshal application state: %v", err)
				return false
			}

			// Filter by both application UID and expected state
			if appState.GetApplicationUid() == applicationUID {
				sc.logf("Received Application state: %s for app: %s", appState.GetStatus(), appState.GetApplicationUid())
				if appState.GetStatus() == expectedState {
					return true
				}
			}

			return false
		},
	)

	if err != nil {
		return nil, err
	}

	var appState aquariumv2.ApplicationState
	if err := notification.GetObjectData().UnmarshalTo(&appState); err != nil {
		return nil, fmt.Errorf("failed to unmarshal application state: %w", err)
	}

	sc.logf("Successfully received %s state notification for app %s", expectedState, applicationUID)
	return &appState, nil
}

// WaitForApplicationResource waits for an application resource notification
func (sc *StreamingClient) WaitForApplicationResource(timeout time.Duration) (*aquariumv2.ApplicationResource, error) {
	sc.t.Logf("Client %s: Waiting for application resource notification...", sc.name)

	notification, err := sc.WaitForNotification(
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE,
		timeout,
		func(n *aquariumv2.StreamingServiceSubscribeResponse) bool {
			return n.GetChangeType() == aquariumv2.ChangeType_CHANGE_TYPE_CREATED
		},
	)

	if err != nil {
		return nil, err
	}

	var appResource aquariumv2.ApplicationResource
	if err := notification.GetObjectData().UnmarshalTo(&appResource); err != nil {
		return nil, fmt.Errorf("failed to unmarshal application resource: %w", err)
	}

	sc.logf("Successfully received resource notification! Identifier: %s", appResource.GetIdentifier())
	return &appResource, nil
}

// WaitForApplicationTask waits for an application task notification
func (sc *StreamingClient) WaitForApplicationTask(timeout time.Duration) (*aquariumv2.ApplicationTask, error) {
	sc.t.Logf("Client %s: Waiting for application task notification...", sc.name)

	notification, err := sc.WaitForNotification(
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK,
		timeout,
		func(n *aquariumv2.StreamingServiceSubscribeResponse) bool {
			return n.GetChangeType() == aquariumv2.ChangeType_CHANGE_TYPE_CREATED
		},
	)

	if err != nil {
		return nil, err
	}

	var appTask aquariumv2.ApplicationTask
	if err := notification.GetObjectData().UnmarshalTo(&appTask); err != nil {
		return nil, fmt.Errorf("failed to unmarshal application task: %w", err)
	}

	sc.logf("Successfully received task notification! Application UID: %s", appTask.GetApplicationUid())
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
	sc.t.Logf("Client %s Closing streaming connections...", sc.name)

	// Close bidirectional stream
	if sc.bidirectionalStream != nil {
		if err := sc.bidirectionalStream.CloseRequest(); err != nil {
			sc.t.Logf("Client %s: Error closing bidirectional stream: %v", sc.name, err)
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
		sc.t.Logf("Client %s: All streaming goroutines finished successfully", sc.name)
	case <-time.After(3 * time.Second):
		sc.t.Logf("Client %s: Timeout waiting for streaming goroutines to finish", sc.name)
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
func NewStreamingTestHelper(ctx context.Context, t *testing.T, name string, client aquariumv2connect.StreamingServiceClient) *StreamingTestHelper {
	t.Helper()
	return &StreamingTestHelper{
		sc: NewStreamingClient(ctx, t, name, client),
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

	if resp.GetError() != nil {
		return nil, fmt.Errorf("request failed: %s - %s", resp.GetError().GetCode(), resp.GetError().GetMessage())
	}

	if resp.GetResponseType() != expectedResponseType {
		return nil, fmt.Errorf("unexpected response type: %s (expected %s)", resp.GetResponseType(), expectedResponseType)
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
