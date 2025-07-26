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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/fish"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

var rpcTracer = otel.Tracer("aquarium-fish/rpc")

// ApplicationService implements the Application service
type ApplicationService struct {
	fish *fish.Fish
}

// checkApplicationOwnerOrHasAccess checks if user has permission to deallocate this application
// Set method to "" when you need to check the owner only
func (s *ApplicationService) getApplicationIfUserIsOwnerOrHasAccess(ctx context.Context, appUIDStr, method string) (*typesv2.Application, error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.getApplicationIfUserIsOwnerOrHasAccess",
		trace.WithAttributes(
			attribute.String("application.uid", appUIDStr),
			attribute.String("auth.method", method),
		))
	defer span.End()

	// All the dance is needed to not spoil the internal DB state to unauthorized user
	app, err := s.fish.DB().ApplicationGet(ctx, stringToUUID(appUIDStr))

	userName := rpcutil.GetUserName(ctx)
	span.SetAttributes(attribute.String("user.name", userName))

	// Method could be set to "" when only owner is needed verification
	if (app == nil || !rpcutil.IsUserName(ctx, app.OwnerName)) && (method == "" || !rpcutil.CheckUserPermission(ctx, method)) {
		err := connect.NewError(connect.CodePermissionDenied, fmt.Errorf("Permission denied"))
		span.RecordError(err)
		span.SetStatus(codes.Error, "Permission denied")
		return nil, err
	}
	if err != nil {
		connectErr := connect.NewError(connect.CodeNotFound, fmt.Errorf("Unable to get the application: "+err.Error()))
		span.RecordError(connectErr)
		span.SetStatus(codes.Error, err.Error())
		return nil, connectErr
	}

	if app != nil {
		span.SetAttributes(
			attribute.String("application.owner", app.OwnerName),
			attribute.String("application.label_uid", app.LabelUid.String()),
		)
	}

	return app, nil
}

// List returns a list of applications
func (s *ApplicationService) List(ctx context.Context, _ /*req*/ *connect.Request[aquariumv2.ApplicationServiceListRequest]) (*connect.Response[aquariumv2.ApplicationServiceListResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.List")
	defer span.End()

	out, err := s.fish.DB().ApplicationList(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceListResponse{
			Status: false, Message: "Unable to get the application list: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.Int("applications.total_count", len(out)))

	// Filter the output by owner unless user has permission to view all applications
	if !rpcutil.CheckUserPermission(ctx, auth.ApplicationServiceListAll) {
		userName := rpcutil.GetUserName(ctx)
		span.SetAttributes(attribute.String("user.name", userName))
		var ownerOut []typesv2.Application
		for _, app := range out {
			if app.OwnerName == userName {
				ownerOut = append(ownerOut, app)
			}
		}
		out = ownerOut
		span.SetAttributes(attribute.Bool("applications.filtered_by_owner", true))
	}

	span.SetAttributes(attribute.Int("applications.returned_count", len(out)))

	// Convert to proto response
	resp := &aquariumv2.ApplicationServiceListResponse{
		Status: true, Message: "Applications listed successfully",
		Data: make([]*aquariumv2.Application, len(out)),
	}

	for i, app := range out {
		resp.Data[i] = app.ToApplication()
	}

	return connect.NewResponse(resp), nil
}

// Get returns an application by UID
func (s *ApplicationService) Get(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.Get",
		trace.WithAttributes(
			attribute.String("application.uid", req.Msg.GetApplicationUid()),
		))
	defer span.End()

	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetApplicationUid(), auth.ApplicationServiceGetAll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetResponse{
		Status: true, Message: "Application retrieved successfully",
		Data: app.ToApplication(),
	}), nil
}

// Create creates a new application
func (s *ApplicationService) Create(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceCreateRequest]) (*connect.Response[aquariumv2.ApplicationServiceCreateResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.Create")
	defer span.End()

	// Convert proto application to internal type
	app := typesv2.FromApplication(req.Msg.GetApplication())

	// Set owner name from context
	app.OwnerName = rpcutil.GetUserName(ctx)

	span.SetAttributes(
		attribute.String("application.owner", app.OwnerName),
		attribute.String("application.label_uid", app.LabelUid.String()),
	)

	// Create the application
	if err := s.fish.DB().ApplicationCreate(ctx, &app); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateResponse{
			Status: false, Message: "Failed to create application: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.String("application.uid", app.Uid.String()))

	return connect.NewResponse(&aquariumv2.ApplicationServiceCreateResponse{
		Status: true, Message: "Application created successfully",
		Data: app.ToApplication(),
	}), nil
}

// ListState returns a list of applications
func (s *ApplicationService) ListState(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceListStateRequest]) (*connect.Response[aquariumv2.ApplicationServiceListStateResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.ListState")
	defer span.End()

	out, err := s.fish.DB().ApplicationStateListLatest(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceListStateResponse{
			Status: false, Message: "Unable to get the application states list: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.Int("application_states.total_count", len(out)))

	// Filter the output by owner unless user has permission to list all application states
	if !rpcutil.CheckUserPermission(ctx, auth.ApplicationServiceListStateAll) {
		userName := rpcutil.GetUserName(ctx)
		span.SetAttributes(attribute.String("user.name", userName))
		var ownerOut []typesv2.ApplicationState
		for _, appState := range out {
			app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, appState.ApplicationUid.String(), auth.ApplicationServiceListStateAll)
			if err == nil && app.OwnerName == userName {
				ownerOut = append(ownerOut, appState)
			}
		}
		out = ownerOut
		span.SetAttributes(attribute.Bool("application_states.filtered_by_owner", true))
	}

	span.SetAttributes(attribute.Int("application_states.returned_count", len(out)))

	// Convert to proto response
	resp := &aquariumv2.ApplicationServiceListStateResponse{
		Status: true, Message: "Application States listed successfully",
		Data: make([]*aquariumv2.ApplicationState, len(out)),
	}

	for i, appState := range out {
		resp.Data[i] = appState.ToApplicationState()
	}

	return connect.NewResponse(resp), nil
}

// GetState returns the state of an application
func (s *ApplicationService) GetState(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetStateRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetStateResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.GetState",
		trace.WithAttributes(
			attribute.String("application.uid", req.Msg.GetApplicationUid()),
		))
	defer span.End()

	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetApplicationUid(), auth.ApplicationServiceGetStateAll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	state, err := s.fish.DB().ApplicationStateGetByApplication(ctx, app.Uid)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
			Status: false, Message: "Unable to get application state: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.String("application.status", state.Status.String()))

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetStateResponse{
		Status: true, Message: "Application state retrieved successfully",
		Data: state.ToApplicationState(),
	}), nil
}

// ListResource returns a list of applications
func (s *ApplicationService) ListResource(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceListResourceRequest]) (*connect.Response[aquariumv2.ApplicationServiceListResourceResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.ListResource")
	defer span.End()

	out, err := s.fish.DB().ApplicationResourceList(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceListResourceResponse{
			Status: false, Message: "Unable to get the application resources list: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.Int("application_resources.total_count", len(out)))

	// Filter the output by owner unless user has permission to list all application resources
	if !rpcutil.CheckUserPermission(ctx, auth.ApplicationServiceListResourceAll) {
		userName := rpcutil.GetUserName(ctx)
		span.SetAttributes(attribute.String("user.name", userName))
		var ownerOut []typesv2.ApplicationResource
		for _, appRes := range out {
			app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, appRes.ApplicationUid.String(), auth.ApplicationServiceListResourceAll)
			if err == nil && app.OwnerName == userName {
				ownerOut = append(ownerOut, appRes)
			}
		}
		out = ownerOut
		span.SetAttributes(attribute.Bool("application_resources.filtered_by_owner", true))
	}

	span.SetAttributes(attribute.Int("application_resources.returned_count", len(out)))

	// Convert to proto response
	resp := &aquariumv2.ApplicationServiceListResourceResponse{
		Status: true, Message: "Application Resources listed successfully",
		Data: make([]*aquariumv2.ApplicationResource, len(out)),
	}

	for i, appResource := range out {
		resp.Data[i] = appResource.ToApplicationResource()
	}

	return connect.NewResponse(resp), nil
}

// GetResource returns the resource of an application
func (s *ApplicationService) GetResource(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetResourceRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetResourceResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.GetResource",
		trace.WithAttributes(
			attribute.String("application.uid", req.Msg.GetApplicationUid()),
		))
	defer span.End()

	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetApplicationUid(), auth.ApplicationServiceGetResourceAll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	resource, err := s.fish.DB().ApplicationResourceGetByApplication(ctx, app.Uid)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
			Status: false, Message: "Unable to get application resource: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetResourceResponse{
		Status: true, Message: "Application resource retrieved successfully",
		Data: resource.ToApplicationResource(),
	}), nil
}

// ListTask returns the list of tasks for an application
func (s *ApplicationService) ListTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceListTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceListTaskResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.ListTask",
		trace.WithAttributes(
			attribute.String("application.uid", req.Msg.GetApplicationUid()),
		))
	defer span.End()

	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetApplicationUid(), auth.ApplicationServiceListTaskAll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceListTaskResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	tasks, err := s.fish.DB().ApplicationTaskListByApplication(ctx, app.Uid)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceListTaskResponse{
			Status: false, Message: "Unable to list application tasks: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.Int("application.tasks_count", len(tasks)))

	resp := &aquariumv2.ApplicationServiceListTaskResponse{
		Status: true, Message: "Application tasks listed successfully",
		Data: make([]*aquariumv2.ApplicationTask, len(tasks)),
	}

	for i, task := range tasks {
		resp.Data[i] = task.ToApplicationTask()
	}

	return connect.NewResponse(resp), nil
}

// CreateTask creates a new task for an application
func (s *ApplicationService) CreateTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceCreateTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceCreateTaskResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.CreateTask",
		trace.WithAttributes(
			attribute.String("application.uid", req.Msg.GetApplicationUid()),
		))
	defer span.End()

	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetApplicationUid(), auth.ApplicationServiceCreateTaskAll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	task := typesv2.FromApplicationTask(req.Msg.GetTask())
	task.ApplicationUid = app.Uid

	span.SetAttributes(
		attribute.String("application.task", task.Task),
		attribute.String("application.task_when", task.When.String()),
	)

	err = s.fish.DB().ApplicationTaskCreate(ctx, &task)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
			Status: false, Message: "Failed to create task: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.String("application.task_uid", task.Uid.String()))

	return connect.NewResponse(&aquariumv2.ApplicationServiceCreateTaskResponse{
		Status: true, Message: "Application task created successfully",
		Data: task.ToApplicationTask(),
	}), nil
}

// GetTask returns a specific task for an application
func (s *ApplicationService) GetTask(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceGetTaskRequest]) (*connect.Response[aquariumv2.ApplicationServiceGetTaskResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.GetTask",
		trace.WithAttributes(
			attribute.String("application.task_uid", req.Msg.GetApplicationTaskUid()),
		))
	defer span.End()

	taskUID := stringToUUID(req.Msg.GetApplicationTaskUid())

	task, err1 := s.fish.DB().ApplicationTaskGet(ctx, taskUID)

	// Check if user has permission to view this task
	var app *typesv2.Application
	var err2 error
	if err1 == nil {
		app, err2 = s.fish.DB().ApplicationGet(ctx, task.ApplicationUid)
		if app != nil {
			span.SetAttributes(attribute.String("application.uid", app.Uid.String()))
		}
	}

	if app == nil || !rpcutil.IsUserName(ctx, app.OwnerName) && !rpcutil.CheckUserPermission(ctx, auth.ApplicationServiceGetTaskAll) {
		err := connect.NewError(connect.CodePermissionDenied, nil)
		span.RecordError(err)
		span.SetStatus(codes.Error, "Permission denied")
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Permission denied",
		}), err
	}
	if err1 != nil {
		span.RecordError(err1)
		span.SetStatus(codes.Error, err1.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Unable to get task: " + err1.Error(),
		}), connect.NewError(connect.CodeNotFound, err1)
	}
	if err2 != nil {
		span.RecordError(err2)
		span.SetStatus(codes.Error, err2.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
			Status: false, Message: "Unable to get the application: " + err2.Error(),
		}), connect.NewError(connect.CodeNotFound, err2)
	}

	span.SetAttributes(
		attribute.String("application.task", task.Task),
		attribute.String("application.task_when", task.When.String()),
	)

	return connect.NewResponse(&aquariumv2.ApplicationServiceGetTaskResponse{
		Status: true, Message: "Application task retrieved successfully",
		Data: task.ToApplicationTask(),
	}), nil
}

// Deallocate deallocates an application
func (s *ApplicationService) Deallocate(ctx context.Context, req *connect.Request[aquariumv2.ApplicationServiceDeallocateRequest]) (*connect.Response[aquariumv2.ApplicationServiceDeallocateResponse], error) {
	ctx, span := rpcTracer.Start(ctx, "rpc.ApplicationService.Deallocate",
		trace.WithAttributes(
			attribute.String("application.uid", req.Msg.GetApplicationUid()),
		))
	defer span.End()

	app, err := s.getApplicationIfUserIsOwnerOrHasAccess(ctx, req.Msg.GetApplicationUid(), auth.ApplicationServiceDeallocateAll)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
			Status: false, Message: err.Error(),
		}), err
	}

	userName := rpcutil.GetUserName(ctx)
	span.SetAttributes(attribute.String("user.name", userName))

	state, err := s.fish.DB().ApplicationDeallocate(ctx, app.Uid, userName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
			Status: false, Message: "Failed to deallocate application: " + err.Error(),
		}), connect.NewError(connect.CodeInternal, err)
	}

	span.SetAttributes(attribute.String("application.new_status", state.Status.String()))

	return connect.NewResponse(&aquariumv2.ApplicationServiceDeallocateResponse{
		Status: true, Message: "Application deallocated successfully: " + state.Description,
	}), nil
}
