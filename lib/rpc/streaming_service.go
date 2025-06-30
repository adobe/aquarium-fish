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

	if spc.userAppAccess[userName] == nil {
		spc.userAppAccess[userName] = make(map[typesv2.ApplicationUID]bool)
	}
	spc.userAppAccess[userName][appUID] = true
	log.Debugf("Streaming: Cache granted access to user %s for app %s", userName, appUID)
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

	if userApps, exists := spc.userAppAccess[userName]; exists {
		delete(userApps, appUID)
		log.Debugf("Streaming: Cache revoked access for user %s from app %s", userName, appUID)
	}
}

// CleanupStaleEntries removes entries for applications that no longer exist
func (spc *SubscriptionPermissionCache) CleanupStaleEntries(fish *fish.Fish) {
	spc.mu.Lock()
	defer spc.mu.Unlock()

	now := time.Now()
	// Only cleanup every 5 minutes to avoid excessive database calls
	if now.Sub(spc.lastCleanup) < 5*time.Minute {
		return
	}
	spc.lastCleanup = now

	log.Debugf("Streaming: Starting permission cache cleanup")
	removedCount := 0

	for userName, userApps := range spc.userAppAccess {
		for appUID := range userApps {
			// Check if application still exists
			_, err := fish.DB().ApplicationGet(appUID)
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

	log.Debugf("Streaming: Permission cache cleanup completed, removed %d stale entries", removedCount)
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
	stream        *connect.ServerStream[aquariumv2.StreamingServiceSubscribeResponse]
	subscriptions []aquariumv2.SubscriptionType
	filters       map[string]string
	userName      string
	ctx           context.Context
	cancel        context.CancelFunc
	stateChannel  chan *typesv2.ApplicationState
	taskChannel   chan *typesv2.ApplicationTask
	resourceChan  chan *typesv2.ApplicationResource

	// Buffer overflow protection
	overflowMutex    sync.RWMutex
	overflowCount    int  // Count of consecutive buffer overflows
	isOverflowing    bool // Flag to indicate client is struggling
	lastOverflowTime time.Time
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

// safeSendToStateChannel attempts to send to state channel with overflow detection
func (sub *subscription) safeSendToStateChannel(state *typesv2.ApplicationState) bool {
	select {
	case sub.stateChannel <- state:
		sub.resetOverflow()
		return true
	case <-time.After(overflowTimeout):
		log.Warnf("Subscription %s: State channel send timeout (buffer overflow)", sub.userName)
		return sub.recordOverflow()
	default:
		log.Warnf("Subscription %s: State channel full (buffer overflow)", sub.userName)
		return sub.recordOverflow()
	}
}

// safeSendToTaskChannel attempts to send to task channel with overflow detection
func (sub *subscription) safeSendToTaskChannel(task *typesv2.ApplicationTask) bool {
	select {
	case sub.taskChannel <- task:
		sub.resetOverflow()
		return true
	case <-time.After(overflowTimeout):
		log.Warnf("Subscription %s: Task channel send timeout (buffer overflow)", sub.userName)
		return sub.recordOverflow()
	default:
		log.Warnf("Subscription %s: Task channel full (buffer overflow)", sub.userName)
		return sub.recordOverflow()
	}
}

// safeSendToResourceChannel attempts to send to resource channel with overflow detection
func (sub *subscription) safeSendToResourceChannel(resource *typesv2.ApplicationResource) bool {
	select {
	case sub.resourceChan <- resource:
		sub.resetOverflow()
		return true
	case <-time.After(overflowTimeout):
		log.Warnf("Subscription %s: Resource channel send timeout (buffer overflow)", sub.userName)
		return sub.recordOverflow()
	default:
		log.Warnf("Subscription %s: Resource channel full (buffer overflow)", sub.userName)
		return sub.recordOverflow()
	}
}

// relayStateNotifications safely relays state notifications with buffer overflow protection
func (s *StreamingService) relayStateNotifications(ctx context.Context, subscriptionID string, sub *subscription, dbChannel <-chan *typesv2.ApplicationState) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Subscription %s: State relay goroutine panic: %v", subscriptionID, r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Debugf("Subscription %s: State relay stopping due to context cancellation", subscriptionID)
			return
		case state, ok := <-dbChannel:
			if !ok {
				log.Debugf("Subscription %s: State relay stopping due to closed database channel", subscriptionID)
				return
			}

			// Check if client is already overflowing - skip notification to prevent further overflow
			if sub.isClientOverflowing() {
				log.Debugf("Subscription %s: Skipping state notification due to client overflow", subscriptionID)
				continue
			}

			// Try to send safely - if this returns true, client should be disconnected
			if shouldDisconnect := !sub.safeSendToStateChannel(state); shouldDisconnect {
				log.Errorf("Subscription %s: Disconnecting client due to excessive buffer overflow", subscriptionID)
				sub.cancel() // This will cause the main subscription loop to exit
				return
			}
		}
	}
}

// relayTaskNotifications safely relays task notifications with buffer overflow protection
func (s *StreamingService) relayTaskNotifications(ctx context.Context, subscriptionID string, sub *subscription, dbChannel <-chan *typesv2.ApplicationTask) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Subscription %s: Task relay goroutine panic: %v", subscriptionID, r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Debugf("Subscription %s: Task relay stopping due to context cancellation", subscriptionID)
			return
		case task, ok := <-dbChannel:
			if !ok {
				log.Debugf("Subscription %s: Task relay stopping due to closed database channel", subscriptionID)
				return
			}

			// Check if client is already overflowing - skip notification to prevent further overflow
			if sub.isClientOverflowing() {
				log.Debugf("Subscription %s: Skipping task notification due to client overflow", subscriptionID)
				continue
			}

			// Try to send safely - if this returns true, client should be disconnected
			if shouldDisconnect := !sub.safeSendToTaskChannel(task); shouldDisconnect {
				log.Errorf("Subscription %s: Disconnecting client due to excessive buffer overflow", subscriptionID)
				sub.cancel() // This will cause the main subscription loop to exit
				return
			}
		}
	}
}

// relayResourceNotifications safely relays resource notifications with buffer overflow protection
func (s *StreamingService) relayResourceNotifications(ctx context.Context, subscriptionID string, sub *subscription, dbChannel <-chan *typesv2.ApplicationResource) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Subscription %s: Resource relay goroutine panic: %v", subscriptionID, r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Debugf("Subscription %s: Resource relay stopping due to context cancellation", subscriptionID)
			return
		case resource, ok := <-dbChannel:
			if !ok {
				log.Debugf("Subscription %s: Resource relay stopping due to closed database channel", subscriptionID)
				return
			}

			// Check if client is already overflowing - skip notification to prevent further overflow
			if sub.isClientOverflowing() {
				log.Debugf("Subscription %s: Skipping resource notification due to client overflow", subscriptionID)
				continue
			}

			// Try to send safely - if this returns true, client should be disconnected
			if shouldDisconnect := !sub.safeSendToResourceChannel(resource); shouldDisconnect {
				log.Errorf("Subscription %s: Disconnecting client due to excessive buffer overflow", subscriptionID)
				sub.cancel() // This will cause the main subscription loop to exit
				return
			}
		}
	}
}

// bidirectionalConnection represents an active bidirectional streaming connection
type bidirectionalConnection struct {
	stream      *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]
	streamMutex sync.Mutex // Protects concurrent access to stream.Send()
	userName    string
	ctx         context.Context
	cancel      context.CancelFunc
}

// safeSend safely sends a response through the bidirectional stream with proper synchronization
func (conn *bidirectionalConnection) safeSend(response *aquariumv2.StreamingServiceConnectResponse) error {
	conn.streamMutex.Lock()
	defer conn.streamMutex.Unlock()
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

// requestTypeMapping maps request types to service and method names for RBAC
type serviceMethodInfo struct {
	service string
	method  string
}

var requestTypeMapping = map[string]serviceMethodInfo{
	// ApplicationService
	"ApplicationServiceListRequest":        {auth.ApplicationService, auth.ApplicationServiceList},
	"ApplicationServiceGetRequest":         {auth.ApplicationService, auth.ApplicationServiceGet},
	"ApplicationServiceCreateRequest":      {auth.ApplicationService, auth.ApplicationServiceCreate},
	"ApplicationServiceGetStateRequest":    {auth.ApplicationService, auth.ApplicationServiceGetState},
	"ApplicationServiceGetResourceRequest": {auth.ApplicationService, auth.ApplicationServiceGetResource},
	"ApplicationServiceListTaskRequest":    {auth.ApplicationService, auth.ApplicationServiceListTask},
	"ApplicationServiceCreateTaskRequest":  {auth.ApplicationService, auth.ApplicationServiceCreateTask},
	"ApplicationServiceGetTaskRequest":     {auth.ApplicationService, auth.ApplicationServiceGetTask},
	"ApplicationServiceDeallocateRequest":  {auth.ApplicationService, auth.ApplicationServiceDeallocate},

	// LabelService
	"LabelServiceListRequest":   {auth.LabelService, auth.LabelServiceList},
	"LabelServiceGetRequest":    {auth.LabelService, auth.LabelServiceGet},
	"LabelServiceCreateRequest": {auth.LabelService, auth.LabelServiceCreate},
	"LabelServiceDeleteRequest": {auth.LabelService, auth.LabelServiceDelete},

	// NodeService
	"NodeServiceListRequest":           {auth.NodeService, auth.NodeServiceList},
	"NodeServiceGetThisRequest":        {auth.NodeService, auth.NodeServiceGetThis},
	"NodeServiceSetMaintenanceRequest": {auth.NodeService, auth.NodeServiceSetMaintenance},

	// UserService
	"UserServiceGetMeRequest":  {auth.UserService, auth.UserServiceGetMe},
	"UserServiceListRequest":   {auth.UserService, auth.UserServiceList},
	"UserServiceGetRequest":    {auth.UserService, auth.UserServiceGet},
	"UserServiceCreateRequest": {auth.UserService, auth.UserServiceCreate},
	"UserServiceUpdateRequest": {auth.UserService, auth.UserServiceUpdate},
	"UserServiceDeleteRequest": {auth.UserService, auth.UserServiceDelete},

	// RoleService
	"RoleServiceListRequest":   {auth.RoleService, auth.RoleServiceList},
	"RoleServiceGetRequest":    {auth.RoleService, auth.RoleServiceGet},
	"RoleServiceCreateRequest": {auth.RoleService, auth.RoleServiceCreate},
	"RoleServiceUpdateRequest": {auth.RoleService, auth.RoleServiceUpdate},
	"RoleServiceDeleteRequest": {auth.RoleService, auth.RoleServiceDelete},
}

// Connect handles bidirectional streaming for RPC requests
func (s *StreamingService) Connect(ctx context.Context, stream *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse]) error {
	userName := rpcutil.GetUserName(ctx)
	connectionID := fmt.Sprintf("%s-%s", userName, uuid.NewString())
	log.Debugf("Streaming: New bidirectional connection for user: %s (ID: %s)", userName, connectionID)

	// Check if server is shutting down
	s.shutdownMutex.RLock()
	if s.isShuttingDown {
		s.shutdownMutex.RUnlock()
		log.Debugf("Streaming: Rejecting new connection during shutdown: %s", connectionID)
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
	s.connections[connectionID] = conn
	s.connectionsMutex.Unlock()

	defer func() {
		// Clean up connection
		s.connectionsMutex.Lock()
		delete(s.connections, connectionID)
		s.connectionsMutex.Unlock()
		cancel()
		log.Debugf("Streaming: Cleaned up bidirectional connection: %s", connectionID)
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
					log.Debugf("Streaming: Client closed bidirectional connection gracefully")
					return
				}
				// Check if context was cancelled (expected during shutdown)
				if connCtx.Err() != nil {
					log.Debugf("Streaming: Connection context cancelled, stopping receive loop")
					return
				}
				log.Errorf("Streaming: Error receiving request: %v", err)
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
			log.Debugf("Streaming: Bidirectional connection context cancelled for user: %s (ID: %s)", userName, connectionID)
			return nil

		case <-keepAliveTicker.C:
			// Send keep-alive ping to prevent connection timeout
			log.Debugf("Streaming: Sending keep-alive ping to client")
			// We can send a special keep-alive response that clients can ignore
			keepAliveResp := &aquariumv2.StreamingServiceConnectResponse{
				RequestId:    "keep-alive",
				ResponseType: "KeepAliveResponse",
			}
			if err := conn.safeSend(keepAliveResp); err != nil {
				log.Errorf("Streaming: Error sending keep-alive: %v", err)
				return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send keep-alive: %w", err))
			}

		case req, ok := <-requestChan:
			if !ok {
				// Channel closed, connection ended
				return nil
			}

			log.Debugf("Streaming: Received request - ID: %s, Type: %s", req.RequestId, req.RequestType)

			// Process request asynchronously using the connection context
			go s.processStreamRequest(connCtx, conn, req)
		}
	}
}

// processStreamRequest processes a single streaming request asynchronously
func (s *StreamingService) processStreamRequest(ctx context.Context, conn *bidirectionalConnection, req *aquariumv2.StreamingServiceConnectRequest) {
	log.Debugf("Streaming: Processing request - ID: %s, Type: %s", req.RequestId, req.RequestType)

	// Check RBAC permissions and set proper context for target service
	targetCtx, err := s.prepareTargetServiceContext(ctx, req.RequestType)
	if err != nil {
		log.Errorf("Streaming: RBAC permission denied for request %s: %v", req.RequestId, err)
		// Create error response
		response := &aquariumv2.StreamingServiceConnectResponse{
			RequestId:    req.RequestId,
			ResponseType: s.getResponseType(req.RequestType),
			Error: &aquariumv2.StreamError{
				Code:    connect.CodePermissionDenied.String(),
				Message: fmt.Sprintf("Permission denied: %v", err),
			},
		}
		if sendErr := conn.safeSend(response); sendErr != nil {
			log.Errorf("Streaming: Error sending permission denied response for request %s: %v", req.RequestId, sendErr)
		}
		return
	}

	// Route the request to the appropriate service handler using the target service context
	responseData, err := s.routeRequest(targetCtx, req.RequestType, req.RequestData)

	var response *aquariumv2.StreamingServiceConnectResponse
	if err != nil {
		log.Errorf("Streaming: Error processing request %s: %v", req.RequestId, err)
		// Create error response
		response = &aquariumv2.StreamingServiceConnectResponse{
			RequestId:    req.RequestId,
			ResponseType: s.getResponseType(req.RequestType),
			Error: &aquariumv2.StreamError{
				Code:    connect.CodeOf(err).String(),
				Message: err.Error(),
			},
		}
	} else {
		log.Debugf("Streaming: Successfully processed request %s", req.RequestId)
		// Create success response
		response = &aquariumv2.StreamingServiceConnectResponse{
			RequestId:    req.RequestId,
			ResponseType: s.getResponseType(req.RequestType),
			ResponseData: responseData,
		}
	}

	log.Debugf("Streaming: Sending response for request %s", req.RequestId)
	// Send response back to client
	if err := conn.safeSend(response); err != nil {
		log.Errorf("Streaming: Error sending response for request %s: %v", req.RequestId, err)
	} else {
		log.Debugf("Streaming: Successfully sent response for request %s", req.RequestId)
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

	log.Debugf("Streaming: Checking RBAC permission for %s.%s", service, method)

	// Check if this service/method is excluded from RBAC
	if auth.IsEcludedFromRBAC(service, method) {
		log.Debugf("Streaming: Service/method %s.%s is excluded from RBAC", service, method)
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

	log.Debugf("Streaming: RBAC permission granted for user %s: %s.%s", user.Name, service, method)

	// Set the target service and method in context (like RBACHandler does)
	return s.setServiceMethodContext(ctx, service, method), nil
}

// setServiceMethodContext sets the service and method in context for RBAC
func (s *StreamingService) setServiceMethodContext(ctx context.Context, service, method string) context.Context {
	// Use the proper utility function to set RBAC context
	return rpcutil.SetRBACContext(ctx, service, method)
}

// routeRequest routes a request to the appropriate service handler
func (s *StreamingService) routeRequest(ctx context.Context, requestType string, requestData *anypb.Any) (*anypb.Any, error) {
	log.Debugf("Streaming: Routing request type: %s", requestType)

	switch requestType {
	// Application Service
	case "ApplicationServiceListRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceGetRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceCreateRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceGetStateRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceGetResourceRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceListTaskRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceCreateTaskRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceGetTaskRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)
	case "ApplicationServiceDeallocateRequest":
		return s.routeApplicationRequest(ctx, requestType, requestData)

	// Label Service
	case "LabelServiceListRequest":
		return s.routeLabelRequest(ctx, requestType, requestData)
	case "LabelServiceGetRequest":
		return s.routeLabelRequest(ctx, requestType, requestData)
	case "LabelServiceCreateRequest":
		return s.routeLabelRequest(ctx, requestType, requestData)
	case "LabelServiceDeleteRequest":
		return s.routeLabelRequest(ctx, requestType, requestData)

	// Node Service
	case "NodeServiceListRequest":
		return s.routeNodeRequest(ctx, requestType, requestData)
	case "NodeServiceGetThisRequest":
		return s.routeNodeRequest(ctx, requestType, requestData)
	case "NodeServiceSetMaintenanceRequest":
		return s.routeNodeRequest(ctx, requestType, requestData)

	// User Service
	case "UserServiceGetMeRequest":
		return s.routeUserRequest(ctx, requestType, requestData)
	case "UserServiceListRequest":
		return s.routeUserRequest(ctx, requestType, requestData)
	case "UserServiceGetRequest":
		return s.routeUserRequest(ctx, requestType, requestData)
	case "UserServiceCreateRequest":
		return s.routeUserRequest(ctx, requestType, requestData)
	case "UserServiceUpdateRequest":
		return s.routeUserRequest(ctx, requestType, requestData)
	case "UserServiceDeleteRequest":
		return s.routeUserRequest(ctx, requestType, requestData)

	// Role Service
	case "RoleServiceListRequest":
		return s.routeRoleRequest(ctx, requestType, requestData)
	case "RoleServiceGetRequest":
		return s.routeRoleRequest(ctx, requestType, requestData)
	case "RoleServiceCreateRequest":
		return s.routeRoleRequest(ctx, requestType, requestData)
	case "RoleServiceUpdateRequest":
		return s.routeRoleRequest(ctx, requestType, requestData)
	case "RoleServiceDeleteRequest":
		return s.routeRoleRequest(ctx, requestType, requestData)

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("unsupported request type: %s", requestType))
	}
}

// routeApplicationRequest routes application service requests
func (s *StreamingService) routeApplicationRequest(ctx context.Context, requestType string, requestData *anypb.Any) (*anypb.Any, error) {
	switch requestType {
	case "ApplicationServiceListRequest":
		var req aquariumv2.ApplicationServiceListRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.List(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceGetRequest":
		var req aquariumv2.ApplicationServiceGetRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.Get(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceCreateRequest":
		var req aquariumv2.ApplicationServiceCreateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.Create(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}

		// Cache permission for the owner (creator) of the application
		if resp.Msg.Status && resp.Msg.Data != nil {
			userName := rpcutil.GetUserName(ctx)
			appUID := stringToUUID(resp.Msg.Data.Uid)
			s.permissionCache.GrantAccess(userName, appUID)
			log.Debugf("Streaming: Cached permission for app creator %s -> %s", userName, appUID)
		}

		return anypb.New(resp.Msg)

	case "ApplicationServiceGetStateRequest":
		var req aquariumv2.ApplicationServiceGetStateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.GetState(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceGetResourceRequest":
		var req aquariumv2.ApplicationServiceGetResourceRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.GetResource(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceListTaskRequest":
		var req aquariumv2.ApplicationServiceListTaskRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.ListTask(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceCreateTaskRequest":
		var req aquariumv2.ApplicationServiceCreateTaskRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.CreateTask(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceGetTaskRequest":
		var req aquariumv2.ApplicationServiceGetTaskRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.GetTask(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "ApplicationServiceDeallocateRequest":
		var req aquariumv2.ApplicationServiceDeallocateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.applicationService.Deallocate(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("unsupported application request type: %s", requestType))
	}
}

// routeLabelRequest routes label service requests
func (s *StreamingService) routeLabelRequest(ctx context.Context, requestType string, requestData *anypb.Any) (*anypb.Any, error) {
	switch requestType {
	case "LabelServiceListRequest":
		var req aquariumv2.LabelServiceListRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.labelService.List(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "LabelServiceGetRequest":
		var req aquariumv2.LabelServiceGetRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.labelService.Get(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "LabelServiceCreateRequest":
		var req aquariumv2.LabelServiceCreateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.labelService.Create(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "LabelServiceDeleteRequest":
		var req aquariumv2.LabelServiceDeleteRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.labelService.Delete(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("unsupported label request type: %s", requestType))
	}
}

// routeNodeRequest routes node service requests
func (s *StreamingService) routeNodeRequest(ctx context.Context, requestType string, requestData *anypb.Any) (*anypb.Any, error) {
	switch requestType {
	case "NodeServiceListRequest":
		var req aquariumv2.NodeServiceListRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.nodeService.List(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "NodeServiceGetThisRequest":
		var req aquariumv2.NodeServiceGetThisRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.nodeService.GetThis(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "NodeServiceSetMaintenanceRequest":
		var req aquariumv2.NodeServiceSetMaintenanceRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.nodeService.SetMaintenance(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("unsupported node request type: %s", requestType))
	}
}

// routeUserRequest routes user service requests
func (s *StreamingService) routeUserRequest(ctx context.Context, requestType string, requestData *anypb.Any) (*anypb.Any, error) {
	switch requestType {
	case "UserServiceGetMeRequest":
		var req aquariumv2.UserServiceGetMeRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.userService.GetMe(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "UserServiceListRequest":
		var req aquariumv2.UserServiceListRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.userService.List(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "UserServiceGetRequest":
		var req aquariumv2.UserServiceGetRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.userService.Get(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "UserServiceCreateRequest":
		var req aquariumv2.UserServiceCreateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.userService.Create(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "UserServiceUpdateRequest":
		var req aquariumv2.UserServiceUpdateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.userService.Update(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "UserServiceDeleteRequest":
		var req aquariumv2.UserServiceDeleteRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.userService.Delete(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("unsupported user request type: %s", requestType))
	}
}

// routeRoleRequest routes role service requests
func (s *StreamingService) routeRoleRequest(ctx context.Context, requestType string, requestData *anypb.Any) (*anypb.Any, error) {
	switch requestType {
	case "RoleServiceListRequest":
		var req aquariumv2.RoleServiceListRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.roleService.List(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "RoleServiceGetRequest":
		var req aquariumv2.RoleServiceGetRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.roleService.Get(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "RoleServiceCreateRequest":
		var req aquariumv2.RoleServiceCreateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.roleService.Create(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "RoleServiceUpdateRequest":
		var req aquariumv2.RoleServiceUpdateRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.roleService.Update(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	case "RoleServiceDeleteRequest":
		var req aquariumv2.RoleServiceDeleteRequest
		if err := requestData.UnmarshalTo(&req); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		resp, err := s.roleService.Delete(ctx, connect.NewRequest(&req))
		if err != nil {
			return nil, err
		}
		return anypb.New(resp.Msg)

	default:
		return nil, connect.NewError(connect.CodeUnimplemented, fmt.Errorf("unsupported role request type: %s", requestType))
	}
}

// getResponseType returns the response type for a given request type
func (s *StreamingService) getResponseType(requestType string) string {
	return strings.Replace(requestType, "Request", "Response", 1)
}

// Subscribe handles server streaming for database change notifications
func (s *StreamingService) Subscribe(ctx context.Context, req *connect.Request[aquariumv2.StreamingServiceSubscribeRequest], stream *connect.ServerStream[aquariumv2.StreamingServiceSubscribeResponse]) error {
	userName := rpcutil.GetUserName(ctx)
	// Generating SubscriptionID with NodeUID prefix to later figure out where the user come from
	subscriptionID := fmt.Sprintf("%s-%s", userName, s.fish.DB().NewUID())

	log.Debugf("Subscription %s: New subscription from user %s", subscriptionID, userName)

	// Check if server is shutting down
	s.shutdownMutex.RLock()
	if s.isShuttingDown {
		s.shutdownMutex.RUnlock()
		log.Debugf("Subscription %s: Rejecting new subscription during shutdown", subscriptionID)
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("server is shutting down"))
	}
	s.shutdownMutex.RUnlock()

	// Create subscription context
	subCtx, cancel := context.WithCancel(ctx)

	// Create channels for database changes
	stateChannel := make(chan *typesv2.ApplicationState, 100)
	taskChannel := make(chan *typesv2.ApplicationTask, 100)
	resourceChannel := make(chan *typesv2.ApplicationResource, 100)

	// Create subscription
	sub := &subscription{
		stream:        stream,
		subscriptions: req.Msg.SubscriptionTypes,
		filters:       req.Msg.Filters,
		userName:      userName,
		ctx:           subCtx,
		cancel:        cancel,
		stateChannel:  stateChannel,
		taskChannel:   taskChannel,
		resourceChan:  resourceChannel,
	}

	// Register subscription
	s.subscriptionsMutex.Lock()
	s.subscriptions[subscriptionID] = sub
	s.subscriptionsMutex.Unlock()

	// Subscribe to database changes based on requested types with buffer overflow protection
	for _, subType := range req.Msg.SubscriptionTypes {
		switch subType {
		case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE:
			// Create a safe wrapper channel for database notifications
			dbStateChannel := make(chan *typesv2.ApplicationState, 100)
			s.fish.DB().SubscribeApplicationState(dbStateChannel)
			log.Debugf("Streaming: Subscribed to ApplicationState changes with overflow protection")

			// Start goroutine to safely relay notifications to subscription
			go s.relayStateNotifications(subCtx, subscriptionID, sub, dbStateChannel)

		case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK:
			// Create a safe wrapper channel for database notifications
			dbTaskChannel := make(chan *typesv2.ApplicationTask, 100)
			s.fish.DB().SubscribeApplicationTask(dbTaskChannel)
			log.Debugf("Streaming: Subscribed to ApplicationTask changes with overflow protection")

			// Start goroutine to safely relay notifications to subscription
			go s.relayTaskNotifications(subCtx, subscriptionID, sub, dbTaskChannel)

		case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE:
			// Create a safe wrapper channel for database notifications
			dbResourceChannel := make(chan *typesv2.ApplicationResource, 100)
			s.fish.DB().SubscribeApplicationResource(dbResourceChannel)
			log.Debugf("Streaming: Subscribed to ApplicationResource changes with overflow protection")

			// Start goroutine to safely relay notifications to subscription
			go s.relayResourceNotifications(subCtx, subscriptionID, sub, dbResourceChannel)
		}
	}

	defer func() {
		// Note: Database channels are now handled by relay goroutines
		// The database unsubscription will happen automatically when channels are closed
		// by the relay goroutines when the context is cancelled

		// Clean up subscription
		s.subscriptionsMutex.Lock()
		delete(s.subscriptions, subscriptionID)
		s.subscriptionsMutex.Unlock()
		cancel()

		// Close subscription channels (relay goroutines will handle database channels)
		close(stateChannel)
		close(taskChannel)
		close(resourceChannel)
		log.Debugf("Subscription %s: Cleaned up", subscriptionID)
	}()

	// Send initial heartbeat to confirm subscription is active
	log.Debugf("Subscription %s: Established and ready for events", subscriptionID)

	// Send initial confirmation message to client to confirm subscription is active
	confirmationResponse := &aquariumv2.StreamingServiceSubscribeResponse{
		ObjectType: aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED, // Special type for control messages
		ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_CREATED,                 // Use CREATED to indicate subscription confirmation
		Timestamp:  timestamppb.Now(),
		ObjectData: nil, // No object data for confirmation message
	}

	log.Debugf("Streaming: About to send subscription confirmation to client")
	if err := stream.Send(confirmationResponse); err != nil {
		log.Errorf("Subscription %s: Error sending subscription confirmation: %v", subscriptionID, err)
		return err
	}
	log.Debugf("Streaming: Sent subscription confirmation to client")

	// Create a ticker for periodic keepalives
	keepAliveTicker := time.NewTicker(60 * time.Second)
	defer keepAliveTicker.Stop()

	// Process subscription events
	for {
		select {
		case <-subCtx.Done():
			log.Debugf("Subscription %s: Cancelled", subscriptionID)

			// Check if cancellation was due to buffer overflow
			if sub.isClientOverflowing() {
				log.Warnf("Subscription %s: Client disconnected due to buffer overflow", subscriptionID)
				// Send buffer overflow error notification
				overflowNotification := &aquariumv2.StreamingServiceSubscribeResponse{
					ObjectType: aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED,
					ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_DELETED, // Use DELETED to indicate error/disconnection
					Timestamp:  timestamppb.Now(),
					ObjectData: nil,
				}
				if err := stream.Send(overflowNotification); err != nil {
					log.Debugf("Subscription %s: Failed to send buffer overflow notification: %v", subscriptionID, err)
				}
				// Return error to signal client disconnect due to overflow
				return connect.NewError(connect.CodeResourceExhausted,
					fmt.Errorf("client disconnected due to buffer overflow - unable to keep up with notification rate"))
			} else {
				// Normal shutdown notification
				shutdownNotification := &aquariumv2.StreamingServiceSubscribeResponse{
					ObjectType: aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED,
					ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_UNSPECIFIED,
					Timestamp:  timestamppb.Now(),
					ObjectData: nil,
				}
				if err := stream.Send(shutdownNotification); err != nil {
					log.Debugf("Subscription %s: Failed to send subscription shutdown notification: %v", subscriptionID, err)
				}
			}
			return nil

		case <-ctx.Done():
			log.Debugf("Subscription %s: Context cancelled", subscriptionID)
			return nil

		case <-keepAliveTicker.C:
			// Periodic keepalive logging
			log.Debugf("Subscription %s: Still active, waiting for events...", subscriptionID)

		case appState := <-stateChannel:
			if s.shouldSendObject(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE, appState) {
				log.Debugf("Subscription %s: Sending ApplicationState notification for app %s, status %s", subscriptionID, appState.ApplicationUid, appState.Status)
				if err := s.sendSubscriptionResponse(stream, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE, aquariumv2.ChangeType_CHANGE_TYPE_CREATED, appState.ToApplicationState()); err != nil {
					log.Errorf("Subscription %s: Error sending ApplicationState update: %v", subscriptionID, err)
					return err
				}
			} else {
				log.Debugf("Subscription %s: Skipping ApplicationState notification for user %s", subscriptionID, sub.userName)
			}

		case appTask := <-taskChannel:
			if s.shouldSendObject(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK, appTask) {
				log.Debugf("Subscription %s: Sending ApplicationTask notification for app %s", subscriptionID, appTask.ApplicationUid)
				if err := s.sendSubscriptionResponse(stream, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK, aquariumv2.ChangeType_CHANGE_TYPE_CREATED, appTask.ToApplicationTask()); err != nil {
					log.Errorf("Subscription %s: Error sending ApplicationTask update: %v", subscriptionID, err)
					return err
				}
			} else {
				log.Debugf("Subscription %s: Skipping ApplicationTask notification for user %s", subscriptionID, sub.userName)
			}

		case appResource := <-resourceChannel:
			if s.shouldSendObject(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE, appResource) {
				log.Debugf("Subscription %s: Sending ApplicationResource notification for app %s", subscriptionID, appResource.ApplicationUid)
				if err := s.sendSubscriptionResponse(stream, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE, aquariumv2.ChangeType_CHANGE_TYPE_CREATED, appResource.ToApplicationResource()); err != nil {
					log.Errorf("Subscription %s: Error sending ApplicationResource update: %v", subscriptionID, err)
					return err
				}
			} else {
				log.Debugf("Subscription %s: Skipping ApplicationResource notification for user %s", subscriptionID, sub.userName)
			}
		}
	}
}

// checkApplicationAccess checks if a user has access to an application (owner or RBAC permission)
func (s *StreamingService) checkApplicationAccess(sub *subscription, appUID typesv2.ApplicationUID, rbacMethod string) bool {
	userName := sub.userName

	// First check cache for quick lookup
	if s.permissionCache.HasAccess(userName, appUID) {
		return true
	}

	// Not in cache, need to validate through database + RBAC
	app, err := s.fish.DB().ApplicationGet(appUID)
	if err != nil {
		log.Debugf("Streaming: Failed to get application %s for permission check: %v", appUID, err)
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

	log.Debugf("Streaming: Permission check for user %s -> app %s: owner=%t, rbac=%t, access=%t",
		userName, appUID, isOwner, hasRBACPermission, hasAccess)

	// Cache the result for future use
	if hasAccess {
		s.permissionCache.GrantAccess(userName, appUID)
	}

	return hasAccess
}

// shouldSendObject checks if an object should be sent to the subscriber based on filters and permissions
func (s *StreamingService) shouldSendObject(sub *subscription, objectType aquariumv2.SubscriptionType, obj interface{}) bool {
	// Check if this subscription type is requested
	found := false
	for _, subType := range sub.subscriptions {
		if subType == objectType {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	// Apply filters based on object type
	switch objectType {
	case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE:
		if state, ok := obj.(*typesv2.ApplicationState); ok {
			return s.shouldSendApplicationObject(sub, state.ApplicationUid, objectType)
		}
	case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK:
		if task, ok := obj.(*typesv2.ApplicationTask); ok {
			return s.shouldSendApplicationObject(sub, task.ApplicationUid, objectType)
		}
	case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE:
		if resource, ok := obj.(*typesv2.ApplicationResource); ok {
			return s.shouldSendApplicationObject(sub, resource.ApplicationUid, objectType)
		}
	}

	return true
}

// shouldSendApplicationObject checks if application-related object should be sent based on ownership and RBAC permissions
func (s *StreamingService) shouldSendApplicationObject(sub *subscription, appUID typesv2.ApplicationUID, objectType aquariumv2.SubscriptionType) bool {
	// Check application filters
	if appUIDFilter, exists := sub.filters["application_uid"]; exists {
		if appUID.String() != appUIDFilter {
			log.Debugf("Streaming: Object filtered out by application_uid filter: %s != %s", appUID.String(), appUIDFilter)
			return false
		}
	}

	// Determine the appropriate RBAC method based on subscription type
	var rbacMethod string
	switch objectType {
	case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE:
		rbacMethod = auth.ApplicationServiceGetStateAll
	case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK:
		rbacMethod = auth.ApplicationServiceListTaskAll
	case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE:
		rbacMethod = auth.ApplicationServiceGetResourceAll
	}

	// Check if user has access (owner or RBAC permission)
	hasAccess := s.checkApplicationAccess(sub, appUID, rbacMethod)

	// Trigger cache cleanup periodically during permission checks
	s.permissionCache.CleanupStaleEntries(s.fish)

	return hasAccess
}

// sendSubscriptionResponse sends a subscription response to the client
func (s *StreamingService) sendSubscriptionResponse(stream *connect.ServerStream[aquariumv2.StreamingServiceSubscribeResponse], objectType aquariumv2.SubscriptionType, changeType aquariumv2.ChangeType, obj proto.Message) error {
	objectData, err := anypb.New(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal object data: %w", err)
	}

	response := &aquariumv2.StreamingServiceSubscribeResponse{
		ObjectType: objectType,
		ChangeType: changeType,
		Timestamp:  timestamppb.Now(),
		ObjectData: objectData,
	}

	return stream.Send(response)
}

// GracefulShutdown initiates graceful shutdown of all streaming connections
func (s *StreamingService) GracefulShutdown(timeout time.Duration) {
	log.Info("Streaming: Starting graceful shutdown of all connections...")

	// Set shutdown flag to reject new connections
	s.shutdownMutex.Lock()
	s.isShuttingDown = true
	s.shutdownMutex.Unlock()

	// Create a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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

		log.Debugf("Streaming: Signaling %d bidirectional connections to shutdown", len(connections))
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
				log.Debugf("Streaming: Failed to send shutdown message: %v", err)
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

		log.Debugf("Streaming: Signaling %d subscriptions to shutdown", len(subscriptions))
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
					log.Info("Streaming: All connections closed gracefully")
					return
				}
				log.Debugf("Streaming: Waiting for %d connections and %d subscriptions to close", connCount, subCount)
			}
		}
	}()

	// Wait for graceful shutdown or timeout
	select {
	case <-done:
		log.Info("Streaming: Graceful shutdown completed")
	case <-ctx.Done():
		s.forceCloseAllConnections()
		log.Warn("Streaming: Graceful shutdown timeout, forced closure of remaining connections")
	}
}

// forceCloseAllConnections forcefully closes all remaining streaming connections
func (s *StreamingService) forceCloseAllConnections() {
	// Force close all bidirectional connections
	s.connectionsMutex.Lock()
	for id, conn := range s.connections {
		log.Debugf("Streaming: Force closing bidirectional connection: %s", id)
		conn.cancel()
		delete(s.connections, id)
	}
	s.connectionsMutex.Unlock()

	// Force close all subscriptions
	s.subscriptionsMutex.Lock()
	for id, sub := range s.subscriptions {
		log.Debugf("Streaming: Force closing subscription: %s", id)
		sub.cancel()
		delete(s.subscriptions, id)
	}
	s.subscriptionsMutex.Unlock()

	log.Info("Streaming: All connections forcefully closed")
}
