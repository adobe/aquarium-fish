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

	// Stream limit tracking by user and connection type
	streamLimitsMutex sync.RWMutex
	userStreamCounts  map[string]map[string]int // userName -> connectionType -> count

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
	closed        bool // Indicates if the subscription has been closed

	// Goroutine coordination
	relayWg          sync.WaitGroup // WaitGroup to coordinate relay goroutine shutdown
	listenChannelsWg sync.WaitGroup // WaitGroup to coordinate listenChannels goroutine shutdown
}

// bidirectionalConnection represents an active bidirectional streaming connection
type bidirectionalConnection struct {
	stream      *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
	streamMutex sync.Mutex // Protects concurrent access to stream.Send()
	userName    string
	ctx         context.Context //nolint:containedctx // Is used for sending stop for goroutines
	cancel      context.CancelFunc
	closed      bool // Indicates if the connection has been closed
}

// safeSend safely sends a response through the bidirectional stream with proper synchronization
func (conn *bidirectionalConnection) safeSend(response *aquariumv2.StreamingServiceConnectResponse) error {
	conn.streamMutex.Lock()
	defer conn.streamMutex.Unlock()

	// Check if the connection has been closed or context cancelled
	if conn.closed {
		return fmt.Errorf("connection already closed")
	}

	select {
	case <-conn.ctx.Done():
		return fmt.Errorf("connection context cancelled")
	default:
	}

	defer func() {
		if r := recover(); r != nil {
			// Mark connection as closed if we panic during send
			conn.closed = true
		}
	}()

	return conn.stream.Send(response)
}

// close marks the connection as closed
func (conn *bidirectionalConnection) close() {
	conn.streamMutex.Lock()
	defer conn.streamMutex.Unlock()
	conn.closed = true
}

// close marks the subscription as closed
func (sub *subscription) close() {
	sub.streamMutex.Lock()
	defer sub.streamMutex.Unlock()
	sub.closed = true
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
		userStreamCounts:   make(map[string]map[string]int),
	}
}

// getUserStreamLimit gets the stream limit for a specific user from their configuration
func (*StreamingService) getUserStreamLimit(user *typesv2.User) int32 {
	// Check if user has custom stream limit configuration
	if user.Config != nil && user.Config.StreamsLimit != nil {
		return *user.Config.StreamsLimit
	}

	// By default we allow only one Connect and Stream connection per user
	return 1
}

// incrementStreamCount increments the stream count for a user and connection type
func (s *StreamingService) incrementStreamCount(userName, connectionType string) {
	s.streamLimitsMutex.Lock()
	defer s.streamLimitsMutex.Unlock()

	if s.userStreamCounts[userName] == nil {
		s.userStreamCounts[userName] = make(map[string]int)
	}
	s.userStreamCounts[userName][connectionType]++
}

// decrementStreamCount decrements the stream count for a user and connection type
func (s *StreamingService) decrementStreamCount(userName, connectionType string) {
	s.streamLimitsMutex.Lock()
	defer s.streamLimitsMutex.Unlock()

	if s.userStreamCounts[userName] != nil {
		if s.userStreamCounts[userName][connectionType] > 0 {
			s.userStreamCounts[userName][connectionType]--
		}
		// Clean up empty entries
		if s.userStreamCounts[userName][connectionType] == 0 {
			delete(s.userStreamCounts[userName], connectionType)
		}
		if len(s.userStreamCounts[userName]) == 0 {
			delete(s.userStreamCounts, userName)
		}
	}
}

// getStreamCount gets the current stream count for a user and connection type
func (s *StreamingService) getStreamCount(userName, connectionType string) int {
	s.streamLimitsMutex.RLock()
	defer s.streamLimitsMutex.RUnlock()

	if s.userStreamCounts[userName] == nil {
		return 0
	}
	return s.userStreamCounts[userName][connectionType]
}

// enforceStreamLimit enforces stream limits by terminating old connections if necessary
func (s *StreamingService) enforceStreamLimit(user *typesv2.User, connectionType string, newConnectionID string) error {
	logger := log.WithFunc("rpc", "enforceStreamLimit").With("user", user.Name, "type", connectionType)
	streamLimit := s.getUserStreamLimit(user)
	logger.Debug("Checking stream limit", "limit", streamLimit)

	// If limit is -1 (unlimited), no enforcement needed
	if streamLimit == -1 {
		return nil
	}

	// If limit is 0, reject all connections
	if streamLimit == 0 {
		return fmt.Errorf("limit 0 prevents user from having streaming connections: %s", user.Name)
	}

	currentCount := s.getStreamCount(user.Name, connectionType)

	// If we're under the limit, no action needed
	if currentCount < int(streamLimit) {
		return nil
	}

	logger = logger.With("limit", streamLimit, "current", currentCount)
	logger.Debug("Stream limit exceeded, terminating oldest connection")

	// Find and terminate the oldest connection of this type
	if connectionType == "connect" {
		s.connectionsMutex.Lock()
		var oldestConnID string
		var oldestConn *bidirectionalConnection

		// Find the oldest connection for this user (excluding the new one)
		for connID, conn := range s.connections {
			if conn.userName == user.Name && connID != newConnectionID {
				if oldestConnID == "" {
					oldestConnID = connID
					oldestConn = conn
					break
				}
			}
		}

		if oldestConn != nil {
			logger.Debug("Terminating oldest bidirectional connection", "conn_id", oldestConnID)
			// Send termination message to client
			terminationMsg := &aquariumv2.StreamingServiceConnectResponse{
				RequestId:    "stream-limit-exceeded",
				ResponseType: "StreamLimitExceededNotification",
				Error: &aquariumv2.StreamError{
					Code:    connect.CodeResourceExhausted.String(),
					Message: "Stream limit exceeded, terminating oldest connection",
				},
			}
			oldestConn.safeSend(terminationMsg)
			// Mark connection as closed and cancel the context
			oldestConn.close()
			oldestConn.cancel()
			delete(s.connections, oldestConnID)
		}
		s.connectionsMutex.Unlock()
	} else if connectionType == "subscribe" {
		s.subscriptionsMutex.Lock()
		var oldestSubID string
		var oldestSub *subscription

		// Find the oldest subscription for this user
		for subID, sub := range s.subscriptions {
			if sub.userName == user.Name {
				if oldestSubID == "" {
					oldestSubID = subID
					oldestSub = sub
					break
				}
			}
		}

		if oldestSub != nil {
			logger.Debug("Terminating oldest subscription", "sub_id", oldestSubID)
			// Send termination message to client using the safe method
			s.sendSubscriptionResponse(oldestSub,
				aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED,
				aquariumv2.ChangeType_CHANGE_TYPE_REMOVED,
				nil,
			)
			// Mark subscription as closed and cancel the context
			oldestSub.close()
			oldestSub.cancel()
			delete(s.subscriptions, oldestSubID)
		}
		s.subscriptionsMutex.Unlock()
	}

	return nil
}

// Connect handles bidirectional streaming for RPC requests
func (s *StreamingService) Connect(ctx context.Context, stream *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]) error {
	user := rpcutil.GetUserFromContext(ctx)
	if user == nil {
		return fmt.Errorf("no user found in context")
	}
	connectionUID := fmt.Sprintf("%s-%s", user.Name, uuid.NewString())
	logger := log.WithFunc("rpc", "Connect").With("conn_uid", connectionUID, "user", user.Name)
	logger.Debug("New bidirectional connection for user")
	defer logger.Debug("Completed bidirectional connection for user")

	// Check if server is shutting down
	s.shutdownMutex.RLock()
	if s.isShuttingDown {
		s.shutdownMutex.RUnlock()
		logger.Debug("Rejecting new connection during shutdown")
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("server is shutting down"))
	}
	s.shutdownMutex.RUnlock()

	// Enforce stream limits before creating the connection
	if err := s.enforceStreamLimit(user, "connect", connectionUID); err != nil {
		logger.Error("Stream limit enforcer denied access", "err", err)
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to enforce stream limit: %w", err))
	}

	// Create connection context
	connCtx, cancel := context.WithCancel(ctx)

	// Create and register connection
	conn := &bidirectionalConnection{
		stream:   stream,
		userName: user.Name,
		ctx:      connCtx,
		cancel:   cancel,
	}

	s.connectionsMutex.Lock()
	s.connections[connectionUID] = conn
	s.connectionsMutex.Unlock()

	// Increment stream count for this user and connection type
	s.incrementStreamCount(user.Name, "connect")

	defer func() {
		// Mark connection as closed first to prevent new sends
		conn.close()

		// Clean up connection
		s.connectionsMutex.Lock()
		delete(s.connections, connectionUID)
		s.connectionsMutex.Unlock()

		// Decrement stream count
		s.decrementStreamCount(user.Name, "connect")

		// Cancel context to stop background goroutines
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

	logger.Debug("Sending confirmation keep-alive ping to client")
	keepAliveResp := &aquariumv2.StreamingServiceConnectResponse{
		RequestId:    "keep-alive",
		ResponseType: "KeepAliveResponse",
	}
	if err := conn.safeSend(keepAliveResp); err != nil {
		logger.Error("Error sending confirmation keep-alive", "err", err)
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send confirmation keep-alive: %w", err))
	}

	// Create keep-alive ticker to prevent connection timeouts
	keepAliveTicker := time.NewTicker(30 * time.Second)
	defer keepAliveTicker.Stop()

	for {
		select {
		case <-connCtx.Done():
			logger.Debug("Bidirectional connection context cancelled for user", "user", user.Name)
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
	user := rpcutil.GetUserFromContext(ctx)
	// Generating SubscriptionID with NodeUID prefix to later figure out where the user come from
	subscriptionUID := fmt.Sprintf("%s-%s", user.Name, s.fish.DB().NewUID())

	logger := log.WithFunc("rpc", "Subscribe").With("user", user.Name, "subs_uid", subscriptionUID)
	logger.Debug("New subscription from user")
	defer logger.Debug("Completed subscription from user")

	// Check if server is shutting down
	s.shutdownMutex.RLock()
	if s.isShuttingDown {
		s.shutdownMutex.RUnlock()
		logger.Debug("Rejecting new subscription during shutdown")
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("server is shutting down"))
	}
	s.shutdownMutex.RUnlock()

	// Enforce stream limits before creating the subscription
	if err := s.enforceStreamLimit(user, "subscribe", subscriptionUID); err != nil {
		logger.Error("Stream limit enforcer denied access", "err", err)
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to enforce stream limit: %w", err))
	}

	// Check the requested subscriptions and if user has access to those by rbac permissions
	for _, subType := range req.Msg.GetSubscriptionTypes() {
		rbacService, rbacMethod := s.getSubscriptionPermission(subType)
		rbacCtx := s.setServiceMethodContext(ctx, rbacService, rbacMethod)
		if !rpcutil.CheckUserPermission(rbacCtx, rbacMethod) {
			logger.Warn("Rejecting new subscription due to lack of permission", "sub_type", subType)
			return connect.NewError(connect.CodeUnavailable, fmt.Errorf("no permission to subscribe to %s", subType))
		}
	}

	// Create subscription context
	subCtx, cancel := context.WithCancel(ctx)

	// Create subscription
	sub := &subscription{
		id:            subscriptionUID,
		stream:        stream,
		subscriptions: req.Msg.GetSubscriptionTypes(),
		userName:      user.Name,
		ctx:           subCtx,
		cancel:        cancel,
		channels:      s.setupChannels(),
	}

	// Register subscription
	s.subscriptionsMutex.Lock()
	s.subscriptions[subscriptionUID] = sub
	s.subscriptionsMutex.Unlock()

	// Increment stream count for this user and connection type
	s.incrementStreamCount(user.Name, "subscribe")

	// Subscribe to database changes using generated setup function
	s.setupSubscriptions(subCtx, subscriptionUID, sub, req.Msg.GetSubscriptionTypes())

	defer func() {
		// Mark subscription as closed first to prevent new sends
		sub.close()

		// Clean up subscription
		s.subscriptionsMutex.Lock()
		delete(s.subscriptions, subscriptionUID)
		s.subscriptionsMutex.Unlock()

		// Decrement stream count
		s.decrementStreamCount(user.Name, "subscribe")

		// Cancel context to stop background goroutines
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

			// Normal shutdown notification
			if err := s.sendSubscriptionResponse(
				sub,
				aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED,
				aquariumv2.ChangeType_CHANGE_TYPE_REMOVED,
				nil,
			); err != nil {
				logger.Debug("Failed to send subscription shutdown notification", "err", err)
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
		hasRBACPermission = rpcutil.CheckUserPermission(rbacCtx, rbacMethod+"All")
	}

	hasAccess := isOwner || hasRBACPermission

	logger.Debug("Permission check for user", "user", userName, "owner", isOwner, "rbac", hasRBACPermission, "access", hasAccess)

	// Cache the result for future use
	if hasAccess {
		s.permissionCache.GrantAccess(userName, appUID)
	}

	return hasAccess
}

// shouldSendApplicationUID helper allows to unify logic of Application related objects
func (s *StreamingService) shouldSendApplicationUIDHelper(sub *subscription, appUID typesv2.ApplicationUID, method string) bool {
	// Check if user has access (owner or RBAC permission)
	hasAccess := s.checkApplicationAccess(sub, appUID, method)

	// Trigger cache cleanup periodically during permission checks
	s.permissionCache.CleanupStaleEntries(s.fish)

	return hasAccess
}

// shouldSendApplicationObject checks if application-related object should be sent based on ownership and RBAC permissions
func (s *StreamingService) shouldSendApplicationObject(sub *subscription, app *typesv2.Application, method string) bool {
	return s.shouldSendApplicationUIDHelper(sub, app.Uid, method)
}

// shouldSendApplicationStateObject checks if application-related object should be sent based on ownership and RBAC permissions
func (s *StreamingService) shouldSendApplicationStateObject(sub *subscription, state *typesv2.ApplicationState, method string) bool {
	return s.shouldSendApplicationUIDHelper(sub, state.ApplicationUid, method)
}

// shouldSendApplicationTaskObject checks if application-related object should be sent based on ownership and RBAC permissions
func (s *StreamingService) shouldSendApplicationTaskObject(sub *subscription, task *typesv2.ApplicationTask, method string) bool {
	return s.shouldSendApplicationUIDHelper(sub, task.ApplicationUid, method)
}

// shouldSendApplicationResourceObject checks if application-related object should be sent based on ownership and RBAC permissions
func (s *StreamingService) shouldSendApplicationResourceObject(sub *subscription, res *typesv2.ApplicationResource, method string) bool {
	return s.shouldSendApplicationUIDHelper(sub, res.ApplicationUid, method)
}

// shouldSendRoleObject returns true since subscribers already have required permission
func (*StreamingService) shouldSendRoleObject(_ *subscription, _ *typesv2.Role, _ /*method*/ string) bool {
	// We checking role get access during subscription, so if user has one - no need to filter out anything
	return true
}

// shouldSendLabelObject returns true since subscribers already have required permission
func (*StreamingService) shouldSendLabelObject(_ *subscription, _ *typesv2.Label, _ /*method*/ string) bool {
	// Labels generally available for everyone, so no need in additional checks since user already has label get permission
	return true
}

// shouldSendUserObject returns true since subscribers already have required permission
func (*StreamingService) shouldSendUserObject(_ *subscription, _ *typesv2.User, _ /*method*/ string) bool {
	// We checking user get access during subscription, so if user has one - no need to filter out anything
	return true
}

// shouldSendUserGroupObject returns true since subscribers already have required permission
func (*StreamingService) shouldSendUserGroupObject(_ *subscription, _ *typesv2.UserGroup, _ /*method*/ string) bool {
	// We checking user group get access during subscription, so if user has one - no need to filter out anything
	return true
}

// shouldSendNodeObject returns true since subscribers already have required permission
func (*StreamingService) shouldSendNodeObject(_ *subscription, _ *typesv2.Node, _ /*method*/ string) bool {
	// We checking node get access during subscription, so if user has one - no need to filter out anything
	return true
}

// sendSubscriptionResponse sends a subscription response to the client
func (*StreamingService) sendSubscriptionResponse(sub *subscription, objectType aquariumv2.SubscriptionType, changeType aquariumv2.ChangeType, obj proto.Message) error {
	sub.streamMutex.Lock()
	defer sub.streamMutex.Unlock()

	// Check if subscription is closed
	if sub.closed {
		return fmt.Errorf("subscription already closed")
	}

	// Check if context is cancelled
	select {
	case <-sub.ctx.Done():
		return fmt.Errorf("subscription context cancelled")
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

	defer func() {
		if r := recover(); r != nil {
			// Mark subscription as closed if we panic during send
			sub.closed = true
		}
	}()

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

			conn.safeSend(shutdownMsg)

			// Mark connection as closed and cancel the context
			conn.close()
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
			// Mark subscription as closed and cancel context
			sub.close()
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
		conn.close()
		conn.cancel()
		delete(s.connections, id)
	}
	s.connectionsMutex.Unlock()

	// Force close all subscriptions
	s.subscriptionsMutex.Lock()
	for id, sub := range s.subscriptions {
		logger.Debug("Force closing subscription", "id", id)
		sub.close()
		sub.cancel()
		delete(s.subscriptions, id)
	}
	s.subscriptionsMutex.Unlock()

	// Clear all stream counts since all connections are closed
	s.streamLimitsMutex.Lock()
	s.userStreamCounts = make(map[string]map[string]int)
	s.streamLimitsMutex.Unlock()

	logger.Info("All connections forcefully closed")
}
