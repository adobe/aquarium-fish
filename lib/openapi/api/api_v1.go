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

// Package api is an API definition
package api

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// H is a shortcut for map[string]any
type H map[string]any

// Processor doing processing of the API request
type Processor struct {
	fish *fish.Fish
}

// NewV1Router creates router for APIv1
func NewV1Router(e *echo.Echo, f *fish.Fish) {
	proc := &Processor{fish: f}
	router := e.Group("")
	router.Use(
		// Regular basic auth
		echomw.BasicAuth(proc.BasicAuth),
		// Limiting body size for better security, as usual "64KB ought to be enough for anybody"
		echomw.BodyLimit("64KB"),
	)
	RegisterHandlers(router, proc)
}

// BasicAuth middleware to ensure API will not be used by crocodile
func (e *Processor) BasicAuth(username, password string, c echo.Context) (bool, error) {
	c.Set("uid", crypt.RandString(8))
	log.Debugf("API: %s: New request received: %s %s %s", username, c.Get("uid"), c.Path(), c.Request().URL.String())

	var user *types.User
	if e.fish.GetCfg().DisableAuth {
		// This logic executed during performance tests only
		var err error
		user, err = e.fish.UserGet(username)
		if err != nil {
			return false, err
		}
	} else {
		user = e.fish.UserAuth(username, password)
	}

	// Clean Auth header and set the user
	c.Response().Header().Del("Authorization")
	c.Set("user", user)

	// Will pass if user was found
	return user != nil, nil
}

// UserMeGet API call processor
func (*Processor) UserMeGet(c echo.Context) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	// Cleanup the hash to prevent malicious activity
	user.Hash = crypt.Hash{}
	return c.JSON(http.StatusOK, user)
}

// UserListGet API call processor
func (e *Processor) UserListGet(c echo.Context) error {
	// Only admin can list users
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can list users"})
		return fmt.Errorf("Only 'admin' user can list users")
	}

	out, err := e.fish.UserList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the user list: %v", err)})
		return fmt.Errorf("Unable to get the user list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// UserGet API call processor
func (e *Processor) UserGet(c echo.Context, name string) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can get user"})
		return fmt.Errorf("Only 'admin' user can get user")
	}

	out, err := e.fish.UserGet(name)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User not found: %v", err)})
		return fmt.Errorf("User not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// UserCreateUpdatePost API call processor
func (e *Processor) UserCreateUpdatePost(c echo.Context) error {
	// Only admin can create user, or user can update itself
	var data types.UserAPIPassword
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" && user.Name != data.Name {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can create user and user can update itself"})
		return fmt.Errorf("Only 'admin' user can create user and user can update itself")
	}

	password := data.Password
	if password == "" {
		password = crypt.RandString(64)
	}

	modUser, err := e.fish.UserGet(data.Name)
	if err == nil {
		// Updating existing user
		modUser.Hash = crypt.NewHash(password, nil)
		e.fish.UserSave(modUser)
	} else {
		// Creating new user
		password, modUser, err = e.fish.UserNew(data.Name, password)
		if err != nil {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create user: %v", err)})
			return fmt.Errorf("Unable to create user: %w", err)
		}
	}

	// Fill the output values
	data.CreatedAt = modUser.CreatedAt
	data.UpdatedAt = modUser.UpdatedAt
	if data.Password == "" {
		data.Password = password
	} else {
		data.Password = ""
	}

	return c.JSON(http.StatusOK, data)
}

// UserDelete API call processor
func (e *Processor) UserDelete(c echo.Context, name string) error {
	// Only admin can delete user
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can delete user"})
		return fmt.Errorf("Only 'admin' user can delete user")
	}

	if err := e.fish.UserDelete(name); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User delete failed with error: %v", err)})
		return fmt.Errorf("User delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "User removed"})
}

// ApplicationResourceAccessPut API call processor
func (e *Processor) ApplicationResourceAccessPut(c echo.Context, uid types.ApplicationResourceUID) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}

	res, err := e.fish.ApplicationResourceGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ApplicationResource not found: %v", err)})
		return fmt.Errorf("ApplicationResource not found: %w", err)
	}

	// Only the owner and admin can create access for ApplicationResource
	app, err := e.fish.ApplicationGet(res.ApplicationUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", res.ApplicationUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", res.ApplicationUID, err)
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner & admin can assign service mapping to the Application"})
		return fmt.Errorf("Only the owner & admin can assign service mapping to the Application")
	}

	pwd := crypt.RandString(64)
	// The proxy password is temporary (for the lifetime of the Resource) and one-time
	// so lack of salt will not be a big deal - the params will contribute to salt majorily.
	pwdHash := crypt.NewHash(pwd, []byte{}).Hash
	key, err := crypt.GenerateSSHKey()
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": "Unable to generate SSH key"})
		return fmt.Errorf("Unable to generate SSH key: %w", err)
	}
	pubkey, err := crypt.GetSSHPubKeyFromPem(key)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": "Unable to generate SSH public key"})
		return fmt.Errorf("Unable to generate SSH public key: %w", err)
	}
	rAccess := types.ApplicationResourceAccess{
		ApplicationResourceUID: res.UID,
		// Storing address of the proxy to give the user idea of where to connect to.
		// Later when cluster will be here - it could contain a different node IP instead, because
		// this particular one could not be able to serve the connection.
		Address:  e.fish.GetCfg().ProxySSHAddress,
		Username: user.Name,
		// We should not store clear password, so convert it to salted hash
		Password: fmt.Sprintf("%x", pwdHash),
		// Key need to be stored as public key
		Key: string(pubkey),
	}
	e.fish.ApplicationResourceAccessCreate(&rAccess)

	// Now database has had the hashed credentials stored, we store the original
	// values to return so user have access to the actual credentials.
	rAccess.Password = pwd
	rAccess.Key = string(key)

	return c.JSON(http.StatusOK, rAccess)
}

// ApplicationListGet API call processor
func (e *Processor) ApplicationListGet(c echo.Context) error {
	out, err := e.fish.ApplicationList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the application list: %v", err)})
		return fmt.Errorf("Unable to get the application list: %w", err)
	}

	// Filter the output by owner
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		var ownerOut []types.Application
		for _, app := range out {
			if app.OwnerName == user.Name {
				ownerOut = append(ownerOut, app)
			}
		}
		out = ownerOut
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationGet API call processor
func (e *Processor) ApplicationGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application not found: %v", err)})
		return fmt.Errorf("Application not found: %w", err)
	}

	// Only the owner of the application (or admin) can request it
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner and admin can request the Application"})
		return fmt.Errorf("Only the owner and admin can request the Application")
	}

	return c.JSON(http.StatusOK, app)
}

// ApplicationCreatePost API call processor
func (e *Processor) ApplicationCreatePost(c echo.Context) error {
	var data types.Application
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Set the User field out of the authorized user
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	data.OwnerName = user.Name

	if err := e.fish.ApplicationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create application: %v", err)})
		return fmt.Errorf("Unable to create application: %w", err)
	}

	log.Debug("API: Created new Application:", data.UID)

	return c.JSON(http.StatusOK, data)
}

// ApplicationResourceGet API call processor
func (e *Processor) ApplicationResourceGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	// Only the owner of the application (or admin) can request the resource
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner and admin can request the Application resource"})
		return fmt.Errorf("Only the owner and admin can request the Application resource")
	}

	out, err := e.fish.ApplicationResourceGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ApplicationResource not found: %v", err)})
		return fmt.Errorf("ApplictionResource not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationStateGet API call processor
func (e *Processor) ApplicationStateGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	// Only the owner of the application (or admin) can request the status
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner and admin can request the Application status"})
		return fmt.Errorf("Only the owner and admin can request the Application status")
	}

	out, err := e.fish.ApplicationStateGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application status not found: %v", err)})
		return fmt.Errorf("Application status not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationTaskListGet API call processor
func (e *Processor) ApplicationTaskListGet(c echo.Context, appUID types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(appUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", appUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", appUID, err)
	}

	// Only the owner of the application (or admin) could get the tasks
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner of Application & admin can get the Application Tasks"})
		return fmt.Errorf("Only the owner of Application & admin can get the Application Tasks")
	}

	out, err := e.fish.ApplicationTaskListByApplication(appUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the Application Tasks list: %v", err)})
		return fmt.Errorf("Unable to get the Application Tasks list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationTaskCreatePost API call processor
func (e *Processor) ApplicationTaskCreatePost(c echo.Context, appUID types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(appUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", appUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", appUID, err)
	}

	// Only the owner of the application (or admin) could create the tasks
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner of Application & admin can create the Application Tasks"})
		return fmt.Errorf("Only the owner of Application & admin can create the Application Tasks")
	}

	var data types.ApplicationTask
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Set Application UID for the task forcefully to not allow creating tasks for the other Apps
	data.ApplicationUID = appUID

	if err := e.fish.ApplicationTaskCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create ApplicationTask: %v", err)})
		return fmt.Errorf("Unable to create ApplicationTask: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

// ApplicationTaskGet API call processor
func (e *Processor) ApplicationTaskGet(c echo.Context, taskUID types.ApplicationTaskUID) error {
	task, err := e.fish.ApplicationTaskGet(taskUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", taskUID)})
		return fmt.Errorf("Unable to find the ApplicationTask: %s, %w", taskUID, err)
	}

	app, err := e.fish.ApplicationGet(task.ApplicationUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", task.ApplicationUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", task.ApplicationUID, err)
	}

	// Only the owner of the application (or admin) could get the attached task
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner of Application & admin can get the ApplicationTask"})
		return fmt.Errorf("Only the owner of Application & admin can get the ApplicationTask")
	}

	return c.JSON(http.StatusOK, task)
}

// ApplicationDeallocateGet API call processor
func (e *Processor) ApplicationDeallocateGet(c echo.Context, uid types.ApplicationUID) error {
	app, err := e.fish.ApplicationGet(uid)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	// Only the owner of the application (or admin) could deallocate it
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if app.OwnerName != user.Name && user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner & admin can deallocate the Application resource"})
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

	newStatus := types.ApplicationStatusDEALLOCATE
	if out.Status != types.ApplicationStatusALLOCATED {
		// The Application was not yet Allocated so just mark it as Recalled
		newStatus = types.ApplicationStatusRECALLED
	}
	as := &types.ApplicationState{ApplicationUID: uid, Status: newStatus,
		Description: fmt.Sprintf("Requested by user %s", user.Name),
	}
	err = e.fish.ApplicationStateCreate(as)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to deallocate the Application: %s", uid)})
		return fmt.Errorf("Unable to deallocate the Application: %s, %w", uid, err)
	}

	return c.JSON(http.StatusOK, as)
}

// LabelListGet API call processor
func (e *Processor) LabelListGet(c echo.Context, params types.LabelListGetParams) error {
	// Deprecated functionality:
	// For backward compatibility and easier migration support "name=" and "version=" filter
	// It's dirty, so no doubt it will fail for complicated cases - so migrate to proper filters
	if params.Filter != nil {
		// Name label usually doesn't contain spaces, so using as separator
		filterSplit := strings.Split(*params.Filter, " ")
		for _, item := range filterSplit {
			log.Debug("DEPRECATED: Processing filter item:", item)
			if params.Name == nil && strings.HasPrefix(item, "name=") {
				val := strings.Trim(strings.SplitN(item, "=", 2)[1], "\"'")
				params.Name = &val
			}
			if params.Version == nil && strings.HasPrefix(item, "version=") {
				val := strings.Trim(strings.SplitN(item, "=", 2)[1], "\"'")
				params.Version = &val
			}
		}
		params.Filter = nil
	}
	out, err := e.fish.LabelList(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the label list: %v", err)})
		return fmt.Errorf("Unable to get the label list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// LabelGet API call processor
func (e *Processor) LabelGet(c echo.Context, uid types.LabelUID) error {
	out, err := e.fish.LabelGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label not found: %v", err)})
		return fmt.Errorf("Label not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// LabelCreatePost API call processor
func (e *Processor) LabelCreatePost(c echo.Context) error {
	// Only admin can create label
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can create label"})
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

// LabelDelete API call processor
func (e *Processor) LabelDelete(c echo.Context, uid types.LabelUID) error {
	// Only admin can delete label
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can delete Label"})
		return fmt.Errorf("Only 'admin' user can delete label")
	}

	err := e.fish.LabelDelete(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label delete failed with error: %v", err)})
		return fmt.Errorf("Label delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "Label removed"})
}

// NodeListGet API call processor
func (e *Processor) NodeListGet(c echo.Context) error {
	out, err := e.fish.NodeList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the node list: %v", err)})
		return fmt.Errorf("Unable to get the node list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// NodeThisGet API call processor
func (e *Processor) NodeThisGet(c echo.Context) error {
	node := e.fish.GetNode()

	return c.JSON(http.StatusOK, node)
}

// NodeThisMaintenanceGet API call processor
func (e *Processor) NodeThisMaintenanceGet(c echo.Context, params types.NodeThisMaintenanceGetParams) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' can set node maintenance"})
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

// NodeThisProfilingIndexGet API call processor
func (e *Processor) NodeThisProfilingIndexGet(c echo.Context) error {
	return e.NodeThisProfilingGet(c, "")
}

// NodeThisProfilingGet API call processor
func (*Processor) NodeThisProfilingGet(c echo.Context, handler string) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' can see profiling info"})
		return fmt.Errorf("Only 'admin' can see profiling info")
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
		c.JSON(http.StatusNotFound, H{"message": "Unable to find requested profiling handler"})
		return fmt.Errorf("Unable to find requested profiling handler")
	}

	return nil
}

// VoteListGet API call processor
func (e *Processor) VoteListGet(c echo.Context) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can get votes"})
		return fmt.Errorf("Only 'admin' user can get votes")
	}

	out := e.fish.VoteActiveList()

	return c.JSON(http.StatusOK, out)
}

// ServiceMappingGet API call processor
func (e *Processor) ServiceMappingGet(c echo.Context, uid types.ServiceMappingUID) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can get service mapping"})
		return fmt.Errorf("Only 'admin' user can get service mapping")
	}

	out, err := e.fish.ServiceMappingGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping not found: %v", err)})
		return fmt.Errorf("ServiceMapping not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ServiceMappingListGet API call processor
func (e *Processor) ServiceMappingListGet(c echo.Context) error {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can get service mappings"})
		return fmt.Errorf("Only 'admin' user can get service mappings")
	}

	out, err := e.fish.ServiceMappingList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the servicemappings list: %v", err)})
		return fmt.Errorf("Unable to get the servicemappings list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ServiceMappingCreatePost API call processor
func (e *Processor) ServiceMappingCreatePost(c echo.Context) error {
	var data types.ServiceMapping
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if data.ApplicationUID != uuid.Nil {
		// Only the owner and admin can create servicemapping for his application
		app, err := e.fish.ApplicationGet(data.ApplicationUID)
		if err != nil {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", data.ApplicationUID)})
			return fmt.Errorf("Unable to find the Application: %s, %w", data.ApplicationUID, err)
		}

		if app.OwnerName != user.Name && user.Name != "admin" {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner & admin can assign service mapping to the Application"})
			return fmt.Errorf("Only the owner & admin can assign service mapping to the Application")
		}
	} else if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can create service mapping with undefined Application"})
		return fmt.Errorf("Only 'admin' user can create service mapping with undefined Application")
	}

	if err := e.fish.ServiceMappingCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create service mapping: %v", err)})
		return fmt.Errorf("Unable to create service mapping: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

// ServiceMappingDelete API call processor
func (e *Processor) ServiceMappingDelete(c echo.Context, uid types.ServiceMappingUID) error {
	// Only admin can delete ServiceMapping
	user, ok := c.Get("user").(*types.User)
	if !ok {
		c.JSON(http.StatusBadRequest, H{"message": "Not authentified"})
		return fmt.Errorf("Not authentified")
	}
	if user.Name != "admin" {
		c.JSON(http.StatusBadRequest, H{"message": "Only 'admin' user can delete service mapping"})
		return fmt.Errorf("Only 'admin' user can delete service mapping")
	}

	if err := e.fish.ServiceMappingDelete(uid); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping %s delete failed with error: %v", uid, err)})
		return fmt.Errorf("ServiceMapping %s delete failed with error: %w", uid, err)
	}

	return c.JSON(http.StatusOK, H{"message": "ServiceMapping removed"})
}
