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
	"fmt"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
	"github.com/adobe/aquarium-fish/lib/rpc/converters"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/gen/proto/aquarium/v2"
)

// ApplicationService implements the Application service
type ApplicationService struct {
	fish *fish.Fish
}

// checkApplicationOwnerOrHasAccess checks if user has permission to deallocate this application
func (s *ApplicationService) getApplicationIfUserIsOwnerOrHasAccess(ctx context.Context, appUIDStr, method string) (*types.Application, error) {
	// All the dance is needed to not spoil the internal DB state to unauthorized user
	appUID, err1 := converters.StringToApplicationUID(appUIDStr)

	var app *types.Application
	var err2 error
	if err1 == nil {
		app, err2 = s.fish.DB().ApplicationGet(appUID)
	}

	if (app == nil || !isUserName(ctx, app.OwnerName)) && (method == "" || !checkPermission(ctx, method)) {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("Permission denied"))
	}
	if err1 != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("Invalid application UID: "+err1.Error()))
	}
	if err2 != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("Unable to get the application: "+err2.Error()))
	}

	return app, nil
}

// List returns a list of applications
func (s *ApplicationService) List(ctx context.Context, _ /*req*/ *connect.Request[aquariumv2.ApplicationServiceListRequest]) (*connect.Response[aquariumv2.ApplicationServiceListResponse], error) {
	out, err := s.fish.DB().ApplicationList()
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceListResponse{
			Status: false, Message: "Unable to get the application list: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	// Filter the output by owner unless user has permission to view all applications
	if !checkPermission(ctx, auth.ApplicationServiceListAll) {
		userName := getUserName(ctx)
		var ownerOut []types.Application
		for _, app := range out {
			if app.OwnerName == userName {
				ownerOut = append(ownerOut, app)
			}
		}
		out = ownerOut
	}

	// Convert to proto response
	resp := &aquariumv2.ApplicationServiceListResponse{
		Status: true, Message: "Applications listed successfully",
		Data: make([]*aquariumv2.Application, len(out)),
	}

	for i, app := range out {
		resp.Data[i] = converters.ConvertApplication(&app)
	}

	return connect.NewResponse(resp), nil
}

// Get returns an application by UID
func (s *ApplicationService) Get(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetResponse], error) {
	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetUid(), auth.ApplicationServiceGetAll)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetResponse{
		Status: true, Message: "Application retrieved successfully",
		Data: converters.ConvertApplication(app),
	}), nil
}

// Create creates a new application
func (s *ApplicationService) Create(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceCreateRequest]) (*connect.Response[aquariumv2.ApplicationServiceCreateResponse], error) {
	// Convert proto application to internal type
	app, err := converters.ConvertApplicationNewFromProto(req.Msg.GetApplication())
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateResponse{
			Status: false, Message: "Invalid application data: " + err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Set owner name from context
	app.OwnerName = getUserName(ctx)

	// Create the application
	err = s.fish.DB().ApplicationCreate(app)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateResponse{
			Status: false, Message: "Failed to create application: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceCreateResponse{
		Status: true, Message: "Application created successfully",
		Data: converters.ConvertApplication(app),
	}), nil
}

// GetState returns the state of an application
func (s *ApplicationService) GetState(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetStateRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetStateResponse], error) {
	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetUid(), auth.ApplicationServiceGetStateAll)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	state, err := s.fish.DB().ApplicationStateGetByApplication(app.UID)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
			Status: false, Message: "Unable to get application state: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
		Status: true, Message: "Application state retrieved successfully",
		Data: converters.ConvertApplicationState(state),
	}), nil
}

// GetResource returns the resource of an application
func (s *ApplicationService) GetResource(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetResourceRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetResourceResponse], error) {
	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetUid(), auth.ApplicationServiceGetResourceAll)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	resource, err := s.fish.DB().ApplicationResourceGetByApplication(app.UID)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
			Status: false, Message: "Unable to get application resource: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
		Status: true, Message: "Application resource retrieved successfully",
		Data: converters.ConvertApplicationResource(resource),
	}), nil
}

// ListTask returns the list of tasks for an application
func (s *ApplicationService) ListTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceListTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceListTaskResponse], error) {
	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetUid(), auth.ApplicationServiceListTaskAll)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceListTaskResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	tasks, err := s.fish.DB().ApplicationTaskListByApplication(app.UID)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceListTaskResponse{
			Status: false, Message: "Unable to list application tasks: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	resp := &aquariumv2.ApplicationServiceListTaskResponse{
		Status: true, Message: "Application tasks listed successfully",
		Data: make([]*aquariumv2.ApplicationTask, len(tasks)),
	}

	for i, task := range tasks {
		resp.Data[i] = converters.ConvertApplicationTask(&task)
	}

	return connect.NewResponse(resp), nil
}

// CreateTask creates a new task for an application
func (s *ApplicationService) CreateTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceCreateTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceCreateTaskResponse], error) {
	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetUid(), "")
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	task, err := converters.ConvertApplicationTaskFromProto(req.Msg.GetTask())
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
			Status: false, Message: "Invalid task data: " + err.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err)
	}

	task.ApplicationUID = app.UID

	err = s.fish.DB().ApplicationTaskCreate(task)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
			Status: false, Message: "Failed to create task: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
		Status: true, Message: "Application task created successfully",
		Data: converters.ConvertApplicationTask(task),
	}), nil
}

// GetTask returns a specific task for an application
func (s *ApplicationService) GetTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetTaskResponse], error) {
	taskUID, err1 := converters.StringToApplicationTaskUID(req.Msg.GetTaskUid())

	var task *types.ApplicationTask
	var err2 error
	if err1 == nil {
		task, err2 = s.fish.DB().ApplicationTaskGet(taskUID)
	}

	// Check if user has permission to view this task
	var app *types.Application
	var err3 error
	if err2 == nil {
		app, err3 = s.fish.DB().ApplicationGet(task.ApplicationUID)
	}

	if app == nil || !isUserName(ctx, app.OwnerName) /*&& !checkPermission(ctx, auth.ApplicationServiceGetTaskAll)*/ {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Permission denied",
		}), connect.NewError(connect.CodePermissionDenied, nil)
	}
	if err1 != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Invalid task UID: " + err1.Error(),
		}), connect.NewError(connect.CodeInvalidArgument, err1)
	}
	if err2 != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Unable to get task: " + err2.Error(),
		}), connect.NewError(connect.CodeNotFound, err2)
	}
	if err3 != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Unable to get the application: " + err3.Error(),
		}), connect.NewError(connect.CodeNotFound, err3)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
		Status: true, Message: "Application task retrieved successfully",
		Data: converters.ConvertApplicationTask(task),
	}), nil
}

// Deallocate deallocates an application
func (s *ApplicationService) Deallocate(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceDeallocateRequest]) (*connect.Response[aquariumv2.ApplicationServiceDeallocateResponse], error) {
	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetUid(), auth.ApplicationServiceDeallocateAll)
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	state, err := s.fish.DB().ApplicationDeallocate(app.UID, getUserName(ctx))
	if err != nil {
		return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
			Status: false, Message: "Failed to deallocate application: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
		Status: true, Message: "Application deallocated successfully: " + state.Description,
	}), nil
}
