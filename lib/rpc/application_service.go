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

package rpc

import (
	"context"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
)

// ApplicationService implements the Application service
type ApplicationService struct {
	fish *fish.Fish
}

// List returns a list of applications
func (s *ApplicationService) List(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceListRequest]) (*connect.Response[aquariumv2.ApplicationServiceListResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceListResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// Get returns an application by UID
func (s *ApplicationService) Get(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceGetResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// Create creates a new application
func (s *ApplicationService) Create(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceCreateRequest]) (*connect.Response[aquariumv2.ApplicationServiceCreateResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceCreateResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// GetState returns the state of an application
func (s *ApplicationService) GetState(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetStateRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetStateResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// GetResource returns the resource of an application
func (s *ApplicationService) GetResource(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetResourceRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetResourceResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// ListTask returns a list of application tasks
func (s *ApplicationService) ListTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceListTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceListTaskResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceListTaskResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// CreateTask creates a new application task
func (s *ApplicationService) CreateTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceCreateTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceCreateTaskResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// GetTask returns a task by UID
func (s *ApplicationService) GetTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetTaskResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}

// Deallocate deallocates an application
func (s *ApplicationService) Deallocate(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceDeallocateRequest]) (*connect.Response[aquariumv2.ApplicationServiceDeallocateResponse], error) {
	// TODO: Implement
	return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
		Status:  false,
		Message: "Not implemented",
	}), connect.NewError(connect.CodeUnimplemented, nil)
}
