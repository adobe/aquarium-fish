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

package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// SubscriptionPermissionCache manages user permissions for applications
type SubscriptionPermissionCache struct {
	mu            sync.RWMutex
	userAppAccess map[string]map[typesv2.ApplicationUID]bool // userName -> appUID -> hasAccess
	lastCleanup   time.Time
}

// NewSubscriptionPermissionCache creates a new permission cache
func NewSubscriptionPermissionCache() *SubscriptionPermissionCache {
	return &SubscriptionPermissionCache{
		userAppAccess: make(map[string]map[typesv2.ApplicationUID]bool),
		lastCleanup:   time.Now(),
	}
}

// GrantAccess grants a user access to an application UID
func (spc *SubscriptionPermissionCache) GrantAccess(userName string, appUID typesv2.ApplicationUID) {
	spc.mu.Lock()
	defer spc.mu.Unlock()
	logger := log.WithFunc("rpc", "GrantAccess")

	if spc.userAppAccess[userName] == nil {
		spc.userAppAccess[userName] = make(map[typesv2.ApplicationUID]bool)
	}
	spc.userAppAccess[userName][appUID] = true
	logger.Debug("Cache granted access to user for app", "user", userName, "app_uid", appUID)
}

// HasAccess checks if a user has cached access to an application UID
func (spc *SubscriptionPermissionCache) HasAccess(userName string, appUID typesv2.ApplicationUID) bool {
	spc.mu.RLock()
	defer spc.mu.RUnlock()

	userApps, exists := spc.userAppAccess[userName]
	if !exists {
		return false
	}
	return userApps[appUID]
}

// RevokeAccess removes a user's access to an application UID
func (spc *SubscriptionPermissionCache) RevokeAccess(userName string, appUID typesv2.ApplicationUID) {
	spc.mu.Lock()
	defer spc.mu.Unlock()
	logger := log.WithFunc("rpc", "RevokeAccess")

	if userApps, exists := spc.userAppAccess[userName]; exists {
		delete(userApps, appUID)
		logger.Debug("Cache revoked access for user from app", "user", userName, "app_uid", appUID)
	}
}

// CleanupStaleEntries removes entries for applications that no longer exist
func (spc *SubscriptionPermissionCache) CleanupStaleEntries(f *fish.Fish) {
	spc.mu.Lock()
	defer spc.mu.Unlock()
	logger := log.WithFunc("rpc", "CleanupStaleEntries")

	now := time.Now()
	// Only cleanup every 5 minutes to avoid excessive database calls
	if now.Sub(spc.lastCleanup) < 5*time.Minute {
		return
	}
	spc.lastCleanup = now

	logger.Debug("Starting permission cache cleanup")
	removedCount := 0

	for userName, userApps := range spc.userAppAccess {
		for appUID := range userApps {
			// Check if application still exists
			_, err := f.DB().ApplicationGet(context.Background(), appUID)
			if err != nil {
				// Application doesn't exist anymore, remove from cache
				delete(userApps, appUID)
				removedCount++
			}
		}
		// Remove empty user entries
		if len(userApps) == 0 {
			delete(spc.userAppAccess, userName)
		}
	}

	logger.Debug("Permission cache cleanup completed", "removed_count", removedCount)
}

// StreamingService implements the streaming service
type StreamingService struct {
	fish *fish.Fish

	// Service handlers for routing
	applicationService *ApplicationService
	labelService       *LabelService
	nodeService        *NodeService
	userService        *UserService
	roleService        *RoleService

	// Active subscriptions
	subscriptionsMutex sync.RWMutex
	subscriptions      map[string]*subscription

	// Active bidirectional connections
	connectionsMutex sync.RWMutex
	connections      map[string]*bidirectionalConnection

	// Permission cache for subscription filtering
	permissionCache *SubscriptionPermissionCache

	// Shutdown coordination
	shutdownMutex  sync.RWMutex
	isShuttingDown bool
}

// subscription represents an active database subscription
type subscription struct {
	id            string
	streamMutex   sync.Mutex // Protects concurrent access to stream.Send()
	stream        *connect.ServerStream[aquariumv2.StreamingServiceSubscribeResponse]
	subscriptions []aquariumv2.SubscriptionType
	userName      string
	ctx           context.Context //nolint:containedctx // Is used for sending stop for goroutines
	cancel        context.CancelFunc
	channels      *subChannels

	// Buffer overflow protection
	overflowMutex    sync.RWMutex
	overflowCount    int  // Count of consecutive buffer overflows
	isOverflowing    bool // Flag to indicate client is struggling
	lastOverflowTime time.Time

	// Goroutine coordination
	relayWg          sync.WaitGroup // WaitGroup to coordinate relay goroutine shutdown
	listenChannelsWg sync.WaitGroup // WaitGroup to coordinate listenChannels goroutine shutdown
}

// Buffer overflow protection constants
const (
	maxOverflowCount  = 5                      // Max consecutive overflows before disconnection
	overflowTimeout   = 100 * time.Millisecond // Max time to wait for channel send
	overflowResetTime = 30 * time.Second       // Time after which overflow count resets
)

// recordOverflow tracks buffer overflow events for this subscription
func (sub *subscription) recordOverflow() bool {
	sub.overflowMutex.Lock()
	defer sub.overflowMutex.Unlock()

	now := time.Now()

	// Reset overflow count if enough time has passed since last overflow
	if now.Sub(sub.lastOverflowTime) > overflowResetTime {
		sub.overflowCount = 0
		sub.isOverflowing = false
	}

	sub.overflowCount++
	sub.lastOverflowTime = now

	// Mark as overflowing if we hit the threshold
	if sub.overflowCount >= maxOverflowCount {
		sub.isOverflowing = true
		return true // Signal that client should be disconnected
	}

	return false
}

// resetOverflow resets the overflow tracking (called on successful sends)
func (sub *subscription) resetOverflow() {
	sub.overflowMutex.Lock()
	defer sub.overflowMutex.Unlock()

	// Only reset if we had recent overflow issues
	if time.Since(sub.lastOverflowTime) < overflowResetTime {
		sub.overflowCount = 0
		sub.isOverflowing = false
	}
}

// isClientOverflowing checks if client is currently struggling with buffer overflow
func (sub *subscription) isClientOverflowing() bool {
	sub.overflowMutex.RLock()
	defer sub.overflowMutex.RUnlock()
	return sub.isOverflowing
}

// bidirectionalConnection represents an active bidirectional streaming connection
type bidirectionalConnection struct {
	stream      *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
	streamMutex sync.Mutex // Protects concurrent access to stream.Send()
	userName    string
	ctx         context.Context //nolint:containedctx // Is used for sending stop for goroutines
	cancel      context.CancelFunc
}

// safeSend safely sends a response through the bidirectional stream with proper synchronization
func (conn *bidirectionalConnection) safeSend(response *aquariumv2.StreamingServiceConnectResponse) error {
	conn.streamMutex.Lock()
	defer conn.streamMutex.Unlock()
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Recovered from panic: %v\n", r)
		}
	}()
	return conn.stream.Send(response)
}

// NewStreamingService creates a new streaming service
func NewStreamingService(f *fish.Fish) *StreamingService {
	return &StreamingService{
		fish:               f,
		applicationService: &ApplicationService{fish: f},
		labelService:       &LabelService{fish: f},
		nodeService:        &NodeService{fish: f},
		userService:        &UserService{fish: f},
		roleService:        &RoleService{fish: f},
		subscriptions:      make(map[string]*subscription),
		connections:        make(map[string]*bidirectionalConnection),
		permissionCache:    NewSubscriptionPermissionCache(),
	}
}

// Connect handles bidirectional streaming for RPC requests
func (s *StreamingService) Connect(ctx context.Context, stream *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]) error {
	userName := rpcutil.GetUserName(ctx)
	connectionUID := fmt.Sprintf("%s-%s", userName, uuid.NewString())
	logger := log.WithFunc("rpc", "Connect").With("conn_uid", connectionUID)
	logger.Debug("New bidirectional connection for user", "user", userName)
	defer logger.Debug("Completed bidirectional connection for user", "user", userName)

	// Check if server is shutting down
	s.shutdownMutex.RLock()
	if s.isShuttingDown {
		s.shutdownMutex.RUnlock()
		logger.Debug("Rejecting new connection during shutdown")
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("server is shutting down"))
	}
	s.shutdownMutex.RUnlock()

	// Create connection context
	connCtx, cancel := context.WithCancel(ctx)

	// Create and register connection
	conn := &bidirectionalConnection{
		stream:   stream,
		userName: userName,
		ctx:      connCtx,
		cancel:   cancel,
	}

	s.connectionsMutex.Lock()
	s.connections[connectionUID] = conn
	s.connectionsMutex.Unlock()

	defer func() {
		// Clean up connection
		s.connectionsMutex.Lock()
		delete(s.connections, connectionUID)
		s.connectionsMutex.Unlock()
		cancel()
		logger.Debug("Cleaned up bidirectional connection")
	}()

	// Create a channel to handle incoming requests
	requestChan := make(chan *aquariumv2.StreamingServiceConnectRequest, 10)

	// Start goroutine to read requests
	go func() {
		defer close(requestChan)
		for {
			req, err := stream.Receive()
			if err != nil {
				if errors.Is(err, io.EOF) {
					logger.Debug("Client closed bidirectional connection gracefully")
					return
				}
				// Check if context was cancelled (expected during shutdown)
				if connCtx.Err() != nil {
					logger.Debug("Connection context cancelled, stopping receive loop")
					return
				}
				logger.Error("Error receiving request", "err", err)
				return
			}

			select {
			case requestChan <- req:
			case <-connCtx.Done():
				return
			}
		}
	}()

	// Create keep-alive ticker to prevent connection timeouts
	keepAliveTicker := time.NewTicker(30 * time.Second)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-connCtx.Done():
			logger.Debug("Bidirectional connection context cancelled for user", "user", userName)
			return nil

		case <-keepAliveTicker.C:
			// Send keep-alive ping to prevent connection timeout
			logger.Debug("Sending keep-alive ping to client")
			// We can send a special keep-alive response that clients can ignore
			keepAliveResp := &aquariumv2.StreamingServiceConnectResponse{
				RequestId:    "keep-alive",
				ResponseType: "KeepAliveResponse",
			}
			if err := conn.safeSend(keepAliveResp); err != nil {
				logger.Error("Error sending keep-alive", "err", err)
				return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send keep-alive: %w", err))
			}

		case req, ok := <-requestChan:
			if !ok {
				// Channel closed, connection ended
				return nil
			}

			logger.Debug("Received request", "req_id", req.GetRequestId(), "req_type", req.GetRequestType())

			// Process request asynchronously using the connection context
			go s.processStreamRequest(connCtx, conn, req)
		}
	}
}

// processStreamRequest processes a single streaming request asynchronously
func (s *StreamingService) processStreamRequest(ctx context.Context, conn *bidirectionalConnection, req *aquariumv2.StreamingServiceConnectRequest) {
	logger := log.WithFunc("rpc", "processStreamRequest").With("req_id", req.GetRequestId(), "req_type", req.GetRequestType())
	logger.Debug("Processing request")

	// Check RBAC permissions and set proper context for target service
	targetCtx, err := s.prepareTargetServiceContext(ctx, req.GetRequestType())
	if err != nil {
		logger.Error("RBAC permission denied for request", "err", err)
		// Create error response
		response := &aquariumv2.StreamingServiceConnectResponse{
			RequestId:    req.GetRequestId(),
			ResponseType: s.getResponseType(req.GetRequestType()),
			Error: &aquariumv2.StreamError{
				Code:    connect.CodePermissionDenied.String(),
				Message: fmt.Sprintf("Permission denied: %v", err),
			},
		}
		if sendErr := conn.safeSend(response); sendErr != nil {
			logger.Error("Error sending permission denied response for request", "err", sendErr)
		}
		return
	}

	// Route the request to the appropriate service handler using the target service context
	responseData, err := s.routeRequest(targetCtx, req.GetRequestType(), req.GetRequestData())

	var response *aquariumv2.StreamingServiceConnectResponse
	if err != nil {
		logger.Error("Error processing request", "err", err)
		// Create error response
		response = &aquariumv2.StreamingServiceConnectResponse{
			RequestId:    req.GetRequestId(),
			ResponseType: s.getResponseType(req.GetRequestType()),
			Error: &aquariumv2.StreamError{
				Code:    connect.CodeOf(err).String(),
				Message: err.Error(),
			},
		}
	} else {
		logger.Debug("Successfully processed request")
		// Create success response
		response = &aquariumv2.StreamingServiceConnectResponse{
			RequestId:    req.GetRequestId(),
			ResponseType: s.getResponseType(req.GetRequestType()),
			ResponseData: responseData,
		}
	}

	logger.Debug("Sending response for request")
	// Send response back to client
	if err := conn.safeSend(response); err != nil {
		logger.Error("Error sending response for request", "err", err)
	} else {
		logger.Debug("Successfully sent response for request")
	}
}

// prepareTargetServiceContext checks RBAC permissions and sets the proper context for the target service
func (s *StreamingService) prepareTargetServiceContext(ctx context.Context, requestType string) (context.Context, error) {
	// Get service and method from request type
	serviceMethod, exists := requestTypeMapping[requestType]
	if !exists {
		return ctx, fmt.Errorf("unknown request type: %s", requestType)
	}

	service := serviceMethod.service
	method := serviceMethod.method

	logger := log.WithFunc("rpc", "prepareTargetServiceContext").With("rpc_service", service, "rpc_method", method)
	logger.Debug("Checking RBAC permission")

	// Check if this service/method is excluded from RBAC
	if auth.IsEcludedFromRBAC(service, method) {
		logger.Debug("Service/method is excluded from RBAC")
		// Still set the context for consistency
		return s.setServiceMethodContext(ctx, service, method), nil
	}

	// Get user from context
	user := rpcutil.GetUserFromContext(ctx)
	if user == nil {
		return ctx, fmt.Errorf("no user found in context")
	}

	// Check RBAC permission for the target service and method
	enforcer := auth.GetEnforcer()
	if enforcer == nil {
		return ctx, fmt.Errorf("RBAC enforcer not available")
	}

	if !enforcer.CheckPermission(user.Roles, service, method) {
		return ctx, fmt.Errorf("user %s with roles %v does not have permission for %s.%s",
			user.Name, user.Roles, service, method)
	}

	logger.Debug("RBAC permission granted for user", "user", user.Name)

	// Set the target service and method in context (like RBACHandler does)
	return s.setServiceMethodContext(ctx, service, method), nil
}

// setServiceMethodContext sets the service and method in context for RBAC
func (*StreamingService) setServiceMethodContext(ctx context.Context, service, method string) context.Context {
	// Use the proper utility function to set RBAC context
	return rpcutil.SetRBACContext(ctx, service, method)
}

// getResponseType returns the response type for a given request type
func (*StreamingService) getResponseType(requestType string) string {
	return strings.Replace(requestType, "Request", "Response", 1)
}

// Subscribe handles server streaming for database change notifications
func (s *StreamingService) Subscribe(ctx context.Context, req *connect.Request[aquariumv2.StreamingServiceSubscribeRequest], stream *connect.ServerStream[aquariumv2.StreamingServiceSubscribeResponse]) error {
	userName := rpcutil.GetUserName(ctx)
	// Generating SubscriptionID with NodeUID prefix to later figure out where the user come from
	subscriptionUID := fmt.Sprintf("%s-%s", userName, s.fish.DB().NewUID())

	logger := log.WithFunc("rpc", "Subscribe").With("subs_uid", subscriptionUID)
	logger.Debug("New subscription from user", "user", userName)
	defer logger.Debug("Completed subscription from user", "user", userName)

	// Check if server is shutting down
	s.shutdownMutex.RLock()
	if s.isShuttingDown {
		s.shutdownMutex.RUnlock()
		logger.Debug("Rejecting new subscription during shutdown")
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("server is shutting down"))
	}
	s.shutdownMutex.RUnlock()

	// Create subscription context
	subCtx, cancel := context.WithCancel(ctx)

	// Create subscription
	sub := &subscription{
		id:            subscriptionUID,
		stream:        stream,
		subscriptions: req.Msg.GetSubscriptionTypes(),
		userName:      userName,
		ctx:           subCtx,
		cancel:        cancel,
		channels:      s.setupChannels(),
	}

	// Register subscription
	s.subscriptionsMutex.Lock()
	s.subscriptions[subscriptionUID] = sub
	s.subscriptionsMutex.Unlock()

	// Subscribe to database changes using generated setup function
	s.setupSubscriptions(subCtx, subscriptionUID, sub, req.Msg.GetSubscriptionTypes())

	defer func() {
		// Clean up subscription
		s.subscriptionsMutex.Lock()
		delete(s.subscriptions, subscriptionUID)
		s.subscriptionsMutex.Unlock()

		cancel()

		// Wait for all relay goroutines to finish before closing channels
		logger.Debug("Waiting for relay goroutines to finish")
		sub.relayWg.Wait()
		logger.Debug("All relay goroutines finished, waiting for listenChannels")

		// Wait for listenChannels goroutine to finish
		sub.listenChannelsWg.Wait()
		logger.Debug("All goroutines finished, closing channels")

		// Close subscription channels (relay goroutines have finished)
		sub.channels.Close()
		logger.Debug("Cleaned up")
	}()

	// Send initial heartbeat to confirm subscription is active
	logger.Debug("Established and ready for events")

	// Send initial confirmation message to client to confirm subscription is active
	err := s.sendSubscriptionResponse(sub,
		aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED,
		aquariumv2.ChangeType_CHANGE_TYPE_CREATED,
		&aquariumv2.StreamCreated{
			StreamUid: subscriptionUID,
		},
	)
	if err != nil {
		logger.Error("Error sending subscription confirmation", "err", err)
		return err
	}
	logger.Debug("Sent subscription confirmation to client")

	// Create a ticker for periodic keepalives
	keepAliveTicker := time.NewTicker(60 * time.Second)
	defer keepAliveTicker.Stop()

	// Add to WaitGroup before starting listenChannels goroutine
	sub.listenChannelsWg.Add(1)
	// Running background process to listen for the channels
	go s.listenChannels(sub, ctx, subCtx)

	// Process subscription events
	for {
		select {
		case <-subCtx.Done():
			logger.Debug("Cancelled")

			// Check if cancellation was due to buffer overflow
			if sub.isClientOverflowing() {
				logger.Warn("Client disconnected due to buffer overflow")
				// Send buffer overflow error notification
				if err := s.sendSubscriptionResponse(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED, aquariumv2.ChangeType_CHANGE_TYPE_UNSPECIFIED, nil); err != nil {
					logger.Debug("Failed to send buffer overflow notification", "err", err)
				}
				// Return error to signal client disconnect due to overflow
				return connect.NewError(connect.CodeResourceExhausted,
					fmt.Errorf("client disconnected due to buffer overflow - unable to keep up with notification rate"))
			} else {
				// Normal shutdown notification
				if err := s.sendSubscriptionResponse(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED, aquariumv2.ChangeType_CHANGE_TYPE_DELETED, nil); err != nil {
					logger.Debug("Failed to send subscription shutdown notification", "err", err)
				}
			}
			return nil

		case <-ctx.Done():
			logger.Debug("Context cancelled")
			return nil

		case <-keepAliveTicker.C:
			// Periodic keepalive logging
			logger.Debug("Still active, waiting for events...")
		}
	}
}

// checkApplicationAccess checks if a user has access to an application (owner or RBAC permission)
func (s *StreamingService) checkApplicationAccess(sub *subscription, appUID typesv2.ApplicationUID, rbacMethod string) bool {
	logger := log.WithFunc("rpc", "checkApplicationAccess").With("app_uid", appUID)

	userName := sub.userName

	// Check cache first
	if s.permissionCache.HasAccess(userName, appUID) {
		return true
	}

	// Get application to check ownership
	app, err := s.fish.DB().ApplicationGet(context.Background(), appUID)
	if err != nil {
		logger.Debug("Failed to get application for permission check", "err", err)
		return false
	}

	// Check if user is the owner
	isOwner := app.OwnerName == userName

	// Check if user has "All" permission for this operation type
	hasRBACPermission := false
	if rbacMethod != "" {
		// Set proper RBAC context for permission checking
		rbacCtx := s.setServiceMethodContext(sub.ctx, auth.ApplicationService, rbacMethod)
		hasRBACPermission = rpcutil.CheckUserPermission(rbacCtx, rbacMethod)
	}

	hasAccess := isOwner || hasRBACPermission

	logger.Debug("Permission check for user", "user", userName, "owner", isOwner, "rbac", hasRBACPermission, "access", hasAccess)

	// Cache the result for future use
	if hasAccess {
		s.permissionCache.GrantAccess(userName, appUID)
	}

	return hasAccess
}

// shouldSendApplicationObject checks if application-related object should be sent based on ownership and RBAC permissions
func (s *StreamingService) shouldSendApplicationObject(sub *subscription, appUID typesv2.ApplicationUID, objectType aquariumv2.SubscriptionType) bool {
	// Get the appropriate RBAC method from the generated helper
	rbacMethod := s.getSubscriptionPermissionMethod(objectType)

	// Check if user has access (owner or RBAC permission)
	hasAccess := s.checkApplicationAccess(sub, appUID, rbacMethod)

	// Trigger cache cleanup periodically during permission checks
	s.permissionCache.CleanupStaleEntries(s.fish)

	return hasAccess
}

// sendSubscriptionResponse sends a subscription response to the client
func (*StreamingService) sendSubscriptionResponse(sub *subscription, objectType aquariumv2.SubscriptionType, changeType aquariumv2.ChangeType, obj proto.Message) error {
	// Check if context is cancelled
	select {
	case <-sub.ctx.Done():
		logger := log.WithFunc("rpc", "sendSubscriptionResponse").With("subs_uid", sub.id)
		logger.Warn("Subscription context cancelled, skipping sending response")
		return nil
	default:
	}

	var objectData *anypb.Any
	var err error

	// Only marshal object data if obj is not nil
	if obj != nil {
		objectData, err = anypb.New(obj)
		if err != nil {
			return fmt.Errorf("failed to marshal object data: %w", err)
		}
	}

	response := &aquariumv2.StreamingServiceSubscribeResponse{
		ObjectType: objectType,
		ChangeType: changeType,
		Timestamp:  timestamppb.Now(),
		ObjectData: objectData,
	}

	sub.streamMutex.Lock()
	defer sub.streamMutex.Unlock()

	return sub.stream.Send(response)
}

// GracefulShutdown initiates graceful shutdown of all streaming connections
func (s *StreamingService) GracefulShutdown(ctx context.Context) {
	logger := log.WithFunc("rpc", "GracefulShutdown")
	logger.Info("Starting graceful shutdown of all connections")

	// Set shutdown flag to reject new connections
	s.shutdownMutex.Lock()
	s.isShuttingDown = true
	s.shutdownMutex.Unlock()

	// Channel to wait for all connections to close
	done := make(chan struct{})

	go func() {
		defer close(done)

		// Send shutdown signal to all bidirectional connections
		s.connectionsMutex.RLock()
		connections := make([]*bidirectionalConnection, 0, len(s.connections))
		for _, conn := range s.connections {
			connections = append(connections, conn)
		}
		s.connectionsMutex.RUnlock()

		logger.Debug("Signaling bidirectional connections to shutdown", "count", len(connections))
		for _, conn := range connections {
			// Send shutdown message to client
			shutdownMsg := &aquariumv2.StreamingServiceConnectResponse{
				RequestId:    "server-shutdown",
				ResponseType: "ServerShutdownNotification",
				Error: &aquariumv2.StreamError{
					Code:    connect.CodeUnavailable.String(),
					Message: "Server is shutting down, please disconnect",
				},
			}

			if err := conn.stream.Send(shutdownMsg); err != nil {
				logger.Debug("Failed to send shutdown message", "err", err)
			}

			// Cancel the connection context to trigger graceful closure
			conn.cancel()
		}

		// Send shutdown signal to all subscriptions
		s.subscriptionsMutex.RLock()
		subscriptions := make([]*subscription, 0, len(s.subscriptions))
		for _, sub := range s.subscriptions {
			subscriptions = append(subscriptions, sub)
		}
		s.subscriptionsMutex.RUnlock()

		logger.Debug("Signaling subscriptions to shutdown", "count", len(subscriptions))
		for _, sub := range subscriptions {
			// Cancel subscription context - this will trigger the subscription goroutine
			// to send the shutdown notification itself, avoiding race conditions
			sub.cancel()
		}

		// Wait for all connections to actually close
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return // Timeout reached
			case <-ticker.C:
				s.connectionsMutex.RLock()
				connCount := len(s.connections)
				s.connectionsMutex.RUnlock()

				s.subscriptionsMutex.RLock()
				subCount := len(s.subscriptions)
				s.subscriptionsMutex.RUnlock()

				if connCount == 0 && subCount == 0 {
					logger.Info("All connections closed gracefully")
					return
				}
				logger.Debug("Waiting for connections and subscriptions to close", "connections", connCount, "subscriptions", subCount)
			}
		}
	}()

	// Wait for graceful shutdown or timeout
	select {
	case <-done:
		logger.Info("Graceful shutdown completed")
	case <-ctx.Done():
		s.forceCloseAllConnections()
		logger.Warn("Graceful shutdown timeout, forced closure of remaining connections")
	}
}

// forceCloseAllConnections forcefully closes all remaining streaming connections
func (s *StreamingService) forceCloseAllConnections() {
	logger := log.WithFunc("rpc", "forceCloseAllConnections")
	// Force close all bidirectional connections
	s.connectionsMutex.Lock()
	for id, conn := range s.connections {
		logger.Debug("Force closing bidirectional connection", "id", id)
		conn.cancel()
		delete(s.connections, id)
	}
	s.connectionsMutex.Unlock()

	// Force close all subscriptions
	s.subscriptionsMutex.Lock()
	for id, sub := range s.subscriptions {
		logger.Debug("Force closing subscription", "id", id)
		sub.cancel()
		delete(s.subscriptions, id)
	}
	s.subscriptionsMutex.Unlock()

	logger.Info("All connections forcefully closed")
}
