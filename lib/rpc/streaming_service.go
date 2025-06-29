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

	// Permission cache for subscription filtering
	permissionCache *SubscriptionPermissionCache
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
	log.Debugf("Streaming: NEW BIDIRECTIONAL CONNECTION ESTABLISHED - Connect method called!")

	userName := rpcutil.GetUserName(ctx)
	log.Debugf("Streaming: Bidirectional connection for user: %s", userName)

	for {
		log.Debugf("Streaming: Waiting for next request from client...")
		req, err := stream.Receive()
		if err != nil {
			// Check for wrapped EOF (common in streaming connections)
			if errors.Is(err, io.EOF) {
				log.Debugf("Streaming: Client closed bidirectional connection gracefully")
				return nil
			}
			log.Errorf("Streaming: Error receiving request: %v", err)
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to receive request: %w", err))
		}

		log.Debugf("Streaming: Received request - ID: %s, Type: %s", req.RequestId, req.RequestType)

		// Process request asynchronously using the authenticated context from initial connection
		go s.processStreamRequest(ctx, stream, req)
	}
}

// processStreamRequest processes a single streaming request asynchronously
func (s *StreamingService) processStreamRequest(ctx context.Context, stream *connect.BidiStream[aquariumv2.StreamingServiceConnectRequest, aquariumv2.StreamingServiceConnectResponse], req *aquariumv2.StreamingServiceConnectRequest) {
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
		if sendErr := stream.Send(response); sendErr != nil {
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
	if err := stream.Send(response); err != nil {
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
	subscriptionID := fmt.Sprintf("%s-%d", userName, time.Now().UnixNano())

	log.Debugf("Streaming: New subscription from user %s: %s", userName, subscriptionID)

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

	// Track which subscriptions were actually made for proper cleanup
	var subscribedToState, subscribedToTask, subscribedToResource bool

	// Subscribe to database changes based on requested types
	for _, subType := range req.Msg.SubscriptionTypes {
		switch subType {
		case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE:
			s.fish.DB().SubscribeApplicationState(stateChannel)
			subscribedToState = true
			log.Debugf("Streaming: Subscribed to ApplicationState changes")
		case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK:
			s.fish.DB().SubscribeApplicationTask(taskChannel)
			subscribedToTask = true
			log.Debugf("Streaming: Subscribed to ApplicationTask changes")
		case aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE:
			s.fish.DB().SubscribeApplicationResource(resourceChannel)
			subscribedToResource = true
			log.Debugf("Streaming: Subscribed to ApplicationResource changes")
		}
	}

	defer func() {
		// Unsubscribe from database BEFORE closing channels to avoid "send on closed channel" panic
		if subscribedToState {
			s.fish.DB().UnsubscribeApplicationState(stateChannel)
			log.Debugf("Streaming: Unsubscribed from ApplicationState changes")
		}
		if subscribedToTask {
			s.fish.DB().UnsubscribeApplicationTask(taskChannel)
			log.Debugf("Streaming: Unsubscribed from ApplicationTask changes")
		}
		if subscribedToResource {
			s.fish.DB().UnsubscribeApplicationResource(resourceChannel)
			log.Debugf("Streaming: Unsubscribed from ApplicationResource changes")
		}

		// Clean up subscription
		s.subscriptionsMutex.Lock()
		delete(s.subscriptions, subscriptionID)
		s.subscriptionsMutex.Unlock()
		cancel()

		// Now safe to close channels since database no longer has references
		close(stateChannel)
		close(taskChannel)
		close(resourceChannel)
		log.Debugf("Streaming: Subscription %s cleaned up", subscriptionID)
	}()

	// Send initial heartbeat to confirm subscription is active
	log.Debugf("Streaming: Subscription %s established and ready for events", subscriptionID)

	// Send initial confirmation message to client to confirm subscription is active
	confirmationResponse := &aquariumv2.StreamingServiceSubscribeResponse{
		ObjectType: aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_UNSPECIFIED, // Special type for control messages
		ChangeType: aquariumv2.ChangeType_CHANGE_TYPE_UNSPECIFIED,             // Special type for control messages
		Timestamp:  timestamppb.Now(),
		ObjectData: nil, // No object data for confirmation message
	}

	log.Debugf("Streaming: About to send subscription confirmation to client")
	if err := stream.Send(confirmationResponse); err != nil {
		log.Errorf("Streaming: Error sending subscription confirmation: %v", err)
		return err
	}
	log.Debugf("Streaming: Sent subscription confirmation to client")

	// Create a ticker for periodic keepalives (optional, for debugging)
	keepAliveTicker := time.NewTicker(10 * time.Second)
	defer keepAliveTicker.Stop()

	// Process subscription events
	for {
		select {
		case <-subCtx.Done():
			log.Debugf("Streaming: Subscription %s cancelled", subscriptionID)
			return nil

		case <-ctx.Done():
			log.Debugf("Streaming: Subscription %s context cancelled", subscriptionID)
			return nil

		case <-keepAliveTicker.C:
			// Periodic keepalive logging
			log.Debugf("Streaming: Subscription %s still active, waiting for events...", subscriptionID)

		case appState := <-stateChannel:
			log.Debugf("Streaming: Received ApplicationState notification for app %s, status %s", appState.ApplicationUid, appState.Status)
			if s.shouldSendObject(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE, appState) {
				log.Debugf("Streaming: Sending ApplicationState notification to client")
				if err := s.sendSubscriptionResponse(stream, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_STATE, aquariumv2.ChangeType_CHANGE_TYPE_CREATED, appState.ToApplicationState()); err != nil {
					log.Errorf("Streaming: Error sending ApplicationState update: %v", err)
					return err
				}
			} else {
				log.Debugf("Streaming: ApplicationState notification filtered out for user %s", sub.userName)
			}

		case appTask := <-taskChannel:
			log.Debugf("Streaming: Received ApplicationTask notification for app %s", appTask.ApplicationUid)
			if s.shouldSendObject(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK, appTask) {
				log.Debugf("Streaming: Sending ApplicationTask notification to client")
				if err := s.sendSubscriptionResponse(stream, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_TASK, aquariumv2.ChangeType_CHANGE_TYPE_CREATED, appTask.ToApplicationTask()); err != nil {
					log.Errorf("Streaming: Error sending ApplicationTask update: %v", err)
					return err
				}
			} else {
				log.Debugf("Streaming: ApplicationTask notification filtered out for user %s", sub.userName)
			}

		case appResource := <-resourceChannel:
			log.Debugf("Streaming: Received ApplicationResource notification for app %s", appResource.ApplicationUid)
			if s.shouldSendObject(sub, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE, appResource) {
				log.Debugf("Streaming: Sending ApplicationResource notification to client")
				if err := s.sendSubscriptionResponse(stream, aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_APPLICATION_RESOURCE, aquariumv2.ChangeType_CHANGE_TYPE_CREATED, appResource.ToApplicationResource()); err != nil {
					log.Errorf("Streaming: Error sending ApplicationResource update: %v", err)
					return err
				}
			} else {
				log.Debugf("Streaming: ApplicationResource notification filtered out for user %s", sub.userName)
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
	log.Debugf("Streaming: Checking if should send object for app %s to user %s", appUID, sub.userName)

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

	log.Debugf("Streaming: Permission check result for app %s: %t", appUID, hasAccess)

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
