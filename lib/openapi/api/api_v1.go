/**
 * Copyright 2021 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

package api

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// H is a shortcut for map[string]any
type H map[string]any

type Processor struct {
	fish *fish.Fish
}

func NewV1Router(e *echo.Echo, fish *fish.Fish) {
	proc := &Processor{fish: fish}
	router := e.Group("")
	router.Use(
		// Regular basic auth
		echomw.BasicAuth(proc.BasicAuth),
		// Limiting body size for better security, as usual "64KB ought to be enough for anybody"
		echomw.BodyLimit("64KB"),
	)
	RegisterHandlers(router, proc)
}

func (e *Processor) BasicAuth(username, password string, c echo.Context) (bool, error) {
	user := e.fish.UserAuth(username, password)

	// Clean Auth header and set the user
	c.Response().Header().Del("Authorization")
	c.Set("user", user)

	// Will pass if user was found
	return user != nil, nil
}

func (e *Processor) UserMeGet(c echo.Context) error {
	user := c.Get("user")
	return c.JSON(http.StatusOK, user)
}

func (e *Processor) UserListGet(c echo.Context, params types.UserListGetParams) error {
	// Only admin can list users
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can list users")})
		return fmt.Errorf("Only 'admin' user can list users")
	}

	out, err := e.fish.UserFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the user list: %v", err)})
		return fmt.Errorf("Unable to get the user list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) UserGet(c echo.Context, name string) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get user")})
		return fmt.Errorf("Only 'admin' user can get user")
	}

	out, err := e.fish.UserGet(name)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User not found: %v", err)})
		return fmt.Errorf("User not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) UserCreatePost(c echo.Context) error {
	// Only admin can create user
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create user")})
		return fmt.Errorf("Only 'admin' user can create user")
	}

	var data types.UserAPIPassword
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	password, new_user, err := e.fish.UserNew(data.Name, data.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create user: %v", err)})
		return fmt.Errorf("Unable to create user: %w", err)
	}

	// Fill the output values
	data.CreatedAt = new_user.CreatedAt
	data.UpdatedAt = new_user.UpdatedAt
	if data.Password == "" {
		data.Password = password
	} else {
		data.Password = ""
	}
	data.Hash = new_user.Hash

	return c.JSON(http.StatusOK, new_user)
}

func (e *Processor) UserDelete(c echo.Context, name string) error {
	// Only admin can delete user
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can delete user")})
		return fmt.Errorf("Only 'admin' user can delete user")
	}

	if err := e.fish.UserDelete(name); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User delete failed with error: %v", err)})
		return fmt.Errorf("User delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "User removed"})
}

func (e *Processor) ResourceListGet(c echo.Context, params types.ResourceListGetParams) error {
	// Only admin can list the resources
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can list resource")})
		return fmt.Errorf("Only 'admin' user can list resource")
	}

	out, err := e.fish.ResourceFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the resource list: %v", err)})
		return fmt.Errorf("Unable to get the resource list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ResourceGet(c echo.Context, uid types.ResourceUID) error {
	// Only admin can get the resource directly
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get resource")})
		return fmt.Errorf("Only 'admin' user can get resource")
	}

	out, err := e.fish.ResourceGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Resource not found: %v", err)})
		return fmt.Errorf("Resource not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationListGet(c echo.Context, params types.ApplicationListGetParams) error {
	out, err := e.fish.ApplicationFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the application list: %v", err)})
		return fmt.Errorf("Unable to get the application list: %w", err)
	}

	// Filter the output by owner
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		var owner_out []types.Application
		for _, app := range out {
			if app.OwnerName == user.(*types.User).Name {
				owner_out = append(owner_out, app)
			}
		}
		out = owner_out
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application not found: %v", err)})
		return fmt.Errorf("Application not found: %w", err)
	}

	// Only the owner of the application (or admin) can request it
	user := c.Get("user")
	if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner and admin can request the Application")})
		return fmt.Errorf("Only the owner and admin can request the Application")
	}

	return c.JSON(http.StatusOK, app)
}

func (e *Processor) ApplicationCreatePost(c echo.Context) error {
	var data types.Application
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Set the User field out of the authorized user
	user := c.Get("user")
	data.OwnerName = user.(*types.User).Name

	if err := e.fish.ApplicationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create application: %v", err)})
		return fmt.Errorf("Unable to create application: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ApplicationResourceGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	// Only the owner of the application (or admin) can request the resource
	user := c.Get("user")
	if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner and admin can request the Application resource")})
		return fmt.Errorf("Only the owner and admin can request the Application resource")
	}

	out, err := e.fish.ResourceGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Resource not found: %v", err)})
		return fmt.Errorf("Resource not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationStateGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	// Only the owner of the application (or admin) can request the status
	user := c.Get("user")
	if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner and admin can request the Application status")})
		return fmt.Errorf("Only the owner and admin can request the Application status")
	}

	out, err := e.fish.ApplicationStateGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application status not found: %v", err)})
		return fmt.Errorf("Application status not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationTaskListGet(c echo.Context, app_uid types.ApplicationUID, params types.ApplicationTaskListGetParams) error {
	app, err := e.fish.ApplicationGet(app_uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", app_uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", app_uid, err)
	}

	// Only the owner of the application (or admin) could get the tasks
	user := c.Get("user")
	if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner of Application & admin can get the Application Tasks")})
		return fmt.Errorf("Only the owner of Application & admin can get the Application Tasks")
	}

	out, err := e.fish.ApplicationTaskFindByApplication(app_uid, params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the Application Tasks list: %v", err)})
		return fmt.Errorf("Unable to get the Application Tasks list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ApplicationTaskCreatePost(c echo.Context, app_uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(app_uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", app_uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", app_uid, err)
	}

	// Only the owner of the application (or admin) could create the tasks
	user := c.Get("user")
	if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner of Application & admin can create the Application Tasks")})
		return fmt.Errorf("Only the owner of Application & admin can create the Application Tasks")
	}

	var data types.ApplicationTask
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Set Application UID for the task forcefully to not allow creating tasks for the other Apps
	data.ApplicationUID = app_uid

	if err := e.fish.ApplicationTaskCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create ApplicationTask: %v", err)})
		return fmt.Errorf("Unable to create ApplicationTask: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ApplicationDeallocateGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	// Only the owner of the application (or admin) could deallocate it
	user := c.Get("user")
	if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner & admin can deallocate the Application resource")})
		return fmt.Errorf("Only the owner & admin can deallocate the Application resource")
	}

	out, err := e.fish.ApplicationStateGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find status for the Application: %s", uid)})
		return fmt.Errorf("Unable to find status for the Application: %s, %w", uid, err)
	}
	if !e.fish.ApplicationStateIsActive(out.Status) {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to deallocate the Application with status: %s", out.Status)})
		return fmt.Errorf("Unable to deallocate the Application with status: %s", out.Status)
	}

	new_status := types.ApplicationStatusDEALLOCATE
	if out.Status != types.ApplicationStatusALLOCATED {
		// The Application was not yet Allocated so just mark it as Recalled
		new_status = types.ApplicationStatusRECALLED
	}
	as := &types.ApplicationState{ApplicationUID: uid, Status: new_status,
		Description: fmt.Sprintf("Requested by user %s", user.(*types.User).Name),
	}
	err = e.fish.ApplicationStateCreate(as)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to deallocate the Application: %s", uid)})
		return fmt.Errorf("Unable to deallocate the Application: %s, %w", uid, err)
	}

	return c.JSON(http.StatusOK, as)
}

func (e *Processor) LabelListGet(c echo.Context, params types.LabelListGetParams) error {
	out, err := e.fish.LabelFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the label list: %v", err)})
		return fmt.Errorf("Unable to get the label list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LabelGet(c echo.Context, uid types.LabelUID) error {
	out, err := e.fish.LabelGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label not found: %v", err)})
		return fmt.Errorf("Label not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LabelCreatePost(c echo.Context) error {
	// Only admin can create label
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create label")})
		return fmt.Errorf("Only 'admin' user can create label")
	}

	var data types.Label
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}
	if err := e.fish.LabelCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create label: %v", err)})
		return fmt.Errorf("Unable to create label: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) LabelDelete(c echo.Context, uid types.LabelUID) error {
	// Only admin can delete label
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can delete Label")})
		return fmt.Errorf("Only 'admin' user can delete label")
	}

	err := e.fish.LabelDelete(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label delete failed with error: %v", err)})
		return fmt.Errorf("Label delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "Label removed"})
}

func (e *Processor) NodeListGet(c echo.Context, params types.NodeListGetParams) error {
	out, err := e.fish.NodeFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the node list: %v", err)})
		return fmt.Errorf("Unable to get the node list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) NodeThisGet(c echo.Context) error {
	node := e.fish.GetNode()

	return c.JSON(http.StatusOK, node)
}

func (e *Processor) NodeThisMaintenanceGet(c echo.Context, params types.NodeThisMaintenanceGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' can set node maintenance")})
		return fmt.Errorf("Only 'admin' user can set node maintenance")
	}

	// Set shutdown delay first
	if params.ShutdownDelay != nil {
		dur, err := time.ParseDuration(*params.ShutdownDelay)
		if err != nil {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong duration format: %v", err)})
			return fmt.Errorf("Wrong duration format: %v", err)
		}
		e.fish.ShutdownDelaySet(dur)
	}

	// Then set maintenance mode
	if params.Enable == nil {
		e.fish.MaintenanceSet(true)
	} else {
		e.fish.MaintenanceSet(*params.Enable)
	}

	// Shutdown last, technically will work immediately if maintenance enable is false
	if params.Shutdown != nil {
		e.fish.ShutdownSet(*params.Shutdown)
	}

	return c.JSON(http.StatusOK, params)
}

func (e *Processor) NodeThisProfilingIndexGet(c echo.Context) error {
	return e.NodeThisProfilingGet(c, "")
}

func (e *Processor) NodeThisProfilingGet(c echo.Context, handler string) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' can see profiling info")})
		return fmt.Errorf("Only 'admin' user can see profiling info")
	}

	switch handler {
	case "":
		// Show index if no handler name provided
		pprof.Index(c.Response().Writer, c.Request())
	case "allocs", "block", "goroutine", "heap", "mutex", "threadcreate":
		// PProf usual handlers
		pprof.Handler(handler).ServeHTTP(c.Response(), c.Request())
	case "cmdline":
		pprof.Cmdline(c.Response(), c.Request())
	case "profile":
		pprof.Profile(c.Response(), c.Request())
	case "symbol":
		pprof.Symbol(c.Response(), c.Request())
	case "trace":
		pprof.Trace(c.Response(), c.Request())
	default:
		return fmt.Errorf("Unable to find requested profiling handler")
	}

	return nil
}

func (e *Processor) VoteListGet(c echo.Context, params types.VoteListGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get votes")})
		return fmt.Errorf("Only 'admin' user can get votes")
	}

	out, err := e.fish.VoteFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the vote list: %v", err)})
		return fmt.Errorf("Unable to get the vote list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LocationListGet(c echo.Context, params types.LocationListGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get locations")})
		return fmt.Errorf("Only 'admin' user can get locations")
	}

	out, err := e.fish.LocationFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the location list: %v", err)})
		return fmt.Errorf("Unable to get the location list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) LocationCreatePost(c echo.Context) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create location")})
		return fmt.Errorf("Only 'admin' user can create location")
	}

	var data types.Location
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	if err := e.fish.LocationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create location: %v", err)})
		return fmt.Errorf("Unable to create location: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ServiceMappingGet(c echo.Context, uid types.ServiceMappingUID) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get service mapping")})
		return fmt.Errorf("Only 'admin' user can get service mapping")
	}

	out, err := e.fish.ServiceMappingGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping not found: %v", err)})
		return fmt.Errorf("ServiceMapping not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ServiceMappingListGet(c echo.Context, params types.ServiceMappingListGetParams) error {
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can get service mappings")})
		return fmt.Errorf("Only 'admin' user can get service mappings")
	}

	out, err := e.fish.ServiceMappingFind(params.Filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the servicemappings list: %v", err)})
		return fmt.Errorf("Unable to get the servicemappings list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

func (e *Processor) ServiceMappingCreatePost(c echo.Context) error {
	var data types.ServiceMapping
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	user := c.Get("user")
	if data.ApplicationUID != uuid.Nil {
		// Only the owner and admin can create servicemapping for his application
		app, err := e.fish.ApplicationGet(data.ApplicationUID)
		if err != nil {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", data.ApplicationUID)})
			return fmt.Errorf("Unable to find the Application: %s, %w", data.ApplicationUID, err)
		}

		if app.OwnerName != user.(*types.User).Name && user.(*types.User).Name != "admin" {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only the owner & admin can assign service mapping to the Application")})
			return fmt.Errorf("Only the owner & admin can assign service mapping to the Application")
		}
	} else if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can create service mapping with undefined Application")})
		return fmt.Errorf("Only 'admin' user can create service mapping with undefined Application")
	}

	if err := e.fish.ServiceMappingCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create service mapping: %v", err)})
		return fmt.Errorf("Unable to create service mapping: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

func (e *Processor) ServiceMappingDelete(c echo.Context, uid types.ServiceMappingUID) error {
	// Only admin can delete ServiceMapping
	user := c.Get("user")
	if user.(*types.User).Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Only 'admin' user can delete service mapping")})
		return fmt.Errorf("Only 'admin' user can delete service mapping")
	}

	if err := e.fish.ServiceMappingDelete(uid); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping %s delete failed with error: %v", uid, err)})
		return fmt.Errorf("ServiceMapping %s delete failed with error: %w", uid, err)
	}

	return c.JSON(http.StatusOK, H{"message": "ServiceMapping removed"})
}
