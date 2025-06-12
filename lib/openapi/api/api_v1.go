/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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

// Package api is an API definition
package api

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

// H is a shortcut for map[string]any
type H map[string]any

// Processor doing processing of the API request
type Processor struct {
	fish     *fish.Fish
	enforcer *auth.Enforcer
}

// NewV1Router creates router for APIv1
func NewV1Router(e *echo.Echo, f *fish.Fish) error {
	enforcer := auth.GetEnforcer()
	if enforcer == nil {
		return fmt.Errorf("enforcer not initialized")
	}

	proc := &Processor{fish: f, enforcer: enforcer}
	router := e.Group("")
	router.Use(
		// Regular basic auth
		echomw.BasicAuth(proc.BasicAuth),
		// Limiting body size for better security, as usual "64KB ought to be enough for anybody"
		echomw.BodyLimit("64KB"),
	)
	RegisterHandlers(router, proc)
	return nil
}

// BasicAuth middleware to ensure API will not be used by crocodile
func (e *Processor) BasicAuth(username, password string, c echo.Context) (bool, error) {
	c.Set("uid", crypt.RandString(8))
	log.Debugf("API: %s: New request received: %s %s %s", username, c.Get("uid"), c.Path(), c.Request().URL.String())

	var user *types.User
	if e.fish.GetCfg().DisableAuth {
		// This logic executed during performance tests only
		var err error
		user, err = e.fish.DB().UserGet(username)
		if err != nil {
			return false, err
		}
	} else {
		user = e.fish.DB().UserAuth(username, password)
	}

	// Clean Auth header and set the user
	c.Response().Header().Del("Authorization")
	c.Set("user", user)

	// Will pass if user was found
	return user != nil, nil
}

func getRbacService(c echo.Context) string {
	if service, ok := c.Get("rbac_service").(string); ok {
		return service
	}
	return ""
}

func getRbacMethods(c echo.Context) []string {
	if methods, ok := c.Get("rbac_methods").([]string); ok {
		return methods
	}
	return []string{}
}

// checkPermission checks if the user has permission to perform the action on the object
// It will pass the provided methods through the filter and will return only allowed ones
func (e *Processor) checkPermission(c echo.Context, methods []string) (allowed []string) {
	service := getRbacService(c)

	if user, ok := c.Get("user").(*types.User); ok {
		for _, method := range methods {
			if e.enforcer.CheckPermission(user.Roles, service, method) {
				allowed = append(allowed, method)
			}
		}
	}

	return allowed
}

// isMethodAllowed ensures any of the provided methods are within the allowed list
func isMethodAllowed(c echo.Context, methods ...string) bool {
	allowedMethods := getRbacMethods(c)
	for _, method := range methods {
		if slices.Contains(allowedMethods, method) {
			return true
		}
	}
	return false
}

// getUserName returns logged in user name or empty string
func getUserName(c echo.Context) string {
	user, ok := c.Get("user").(*types.User)
	if !ok {
		return ""
	}

	return user.Name
}

// isUserName ensures the provided name and user's name are the same
func isUserName(c echo.Context, name string) bool {
	return name == getUserName(c)
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
	out, err := e.fish.DB().UserList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the user list: %v", err)})
		return fmt.Errorf("Unable to get the user list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// UserGet API call processor
func (e *Processor) UserGet(c echo.Context, name string) error {
	if !isUserName(c, name) && !isMethodAllowed(c, auth.UserServiceGetAll) {
		c.JSON(http.StatusForbidden, H{"message": "Insufficient permissions"})
		return fmt.Errorf("Insufficient permissions")
	}

	out, err := e.fish.DB().UserGet(name)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User not found: %v", err)})
		return fmt.Errorf("User not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// UserCreateUpdatePost API call processor
func (e *Processor) UserCreateUpdatePost(c echo.Context) error {
	canCreate := isMethodAllowed(c, auth.UserServiceCreate)
	canUpdate := isMethodAllowed(c, auth.UserServiceUpdate, auth.UserServiceUpdateAll)

	var data types.UserAPIPassword
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Check if user exists to determine if this is create or update
	_, err := e.fish.DB().UserGet(data.Name)
	isCreate := err != nil

	// Check permissions
	if isCreate && !canCreate {
		c.JSON(http.StatusForbidden, H{"message": "Insufficient permissions to create user"})
		return fmt.Errorf("Insufficient permissions to create user")
	}
	if !isCreate && !canUpdate {
		c.JSON(http.StatusForbidden, H{"message": "Insufficient permissions to update user"})
		return fmt.Errorf("Insufficient permissions to update user")
	}

	password := data.Password
	if password == "" {
		password = crypt.RandString(64)
	}

	modUser, err := e.fish.DB().UserGet(data.Name)
	if err == nil {
		// Updating existing user
		// No user parameters except for password could be modified here for security reasons
		modUser.Hash = crypt.NewHash(password, nil)
		e.fish.DB().UserSave(modUser)
	} else {
		// Creating new user
		password, modUser, err = e.fish.DB().UserNew(data.Name, password)
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
	if err := e.fish.DB().UserDelete(name); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User delete failed with error: %v", err)})
		return fmt.Errorf("User delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "User deleted successfully"})
}

// RoleListGet API call processor
func (e *Processor) RoleListGet(c echo.Context) error {
	roles, err := e.fish.DB().RoleList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get role list: %v", err)})
		return fmt.Errorf("Unable to get role list: %w", err)
	}

	return c.JSON(http.StatusOK, roles)
}

// RoleGet API call processor
func (e *Processor) RoleGet(c echo.Context, name string) error {
	role, err := e.fish.DB().RoleGet(name)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Role not found: %v", err)})
		return fmt.Errorf("Role not found: %w", err)
	}

	return c.JSON(http.StatusOK, role)
}

// RoleCreateUpdatePost API call processor
func (e *Processor) RoleCreateUpdatePost(c echo.Context) error {
	canCreate := isMethodAllowed(c, auth.RoleServiceCreate)
	canUpdate := isMethodAllowed(c, auth.RoleServiceUpdate)

	var role types.Role
	if err := c.Bind(&role); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Check if role exists to determine if this is create or update
	_, err := e.fish.DB().RoleGet(role.Name)
	isCreate := err != nil

	if isCreate {
		if !canCreate {
			c.JSON(http.StatusForbidden, H{"message": "Insufficient permissions to create role"})
			return fmt.Errorf("Insufficient permissions to create Role")
		}
		// Create role
		if err := e.fish.DB().RoleCreate(&role); err != nil {
			c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Failed to create Role: %v", err)})
			return fmt.Errorf("Failed to cave Role: %w", err)
		}
	} else {
		if !canUpdate {
			c.JSON(http.StatusForbidden, H{"message": "Insufficient permissions to update role"})
			return fmt.Errorf("Insufficient permissions to update Role")
		}
		// Save role
		if err := e.fish.DB().RoleSave(&role); err != nil {
			c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Failed to save Role: %v", err)})
			return fmt.Errorf("Failed to save Role: %w", err)
		}
	}

	return c.JSON(http.StatusOK, role)
}

// RoleDelete API call processor
func (e *Processor) RoleDelete(c echo.Context, name string) error {
	if err := e.fish.DB().RoleDelete(name); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Role delete failed with error: %v", err)})
		return fmt.Errorf("Role delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "Role deleted successfully"})
}

// UserRolesPost API call processor
func (e *Processor) UserRolesPost(c echo.Context, name string) error {
	var roles []string
	if err := c.Bind(&roles); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Get user
	user, err := e.fish.DB().UserGet(name)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("User not found: %v", err)})
		return fmt.Errorf("User not found: %w", err)
	}

	// Update user roles
	user.Roles = roles
	if err := e.fish.DB().UserSave(user); err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Failed to update user roles: %v", err)})
		return fmt.Errorf("Failed to update user roles: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "User roles updated successfully"})
}

// ApplicationResourceAccessPut API call processor
func (e *Processor) ApplicationResourceAccessPut(c echo.Context, uid types.ApplicationResourceUID) error {
	// TODO: Move to Gate since it's a part of ProxySSH gate logic
	res, err := e.fish.DB().ApplicationResourceGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ApplicationResource not found: %v", err)})
		return fmt.Errorf("ApplicationResource not found: %w", err)
	}

	// Only the owner and users with resource access permission can create access for ApplicationResource
	app, err := e.fish.DB().ApplicationGet(res.ApplicationUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", res.ApplicationUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", res.ApplicationUID, err)
	}
	if !isUserName(c, app.OwnerName) {
		c.JSON(http.StatusBadRequest, H{"message": "Only authorized owner can request access to an ApplicationResource"})
		return fmt.Errorf("Only authorized owner can request access to an ApplicationResource")
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
		// TODO: Later when cluster will be here - it could contain a different node IP instead,
		// because this particular one could not be able to serve the connection. Probably need to
		// get node from the ApplicationResource and put it's address in place, but also need to
		// find it's ProxySSH gate config and port, so becomes quite a bit complicated...
		Address:  "TODO", //e.fish.GetCfg().ProxySSHAddress,
		Username: getUserName(c),
		// We should not store clear password, so convert it to salted hash
		Password: fmt.Sprintf("%x", pwdHash),
		// Key need to be stored as public key
		Key: string(pubkey),
	}
	e.fish.DB().ApplicationResourceAccessCreate(&rAccess)

	// Now database has had the hashed credentials stored, we store the original
	// values to return so user have access to the actual credentials.
	rAccess.Password = pwd
	rAccess.Key = string(key)

	return c.JSON(http.StatusOK, rAccess)
}

// ApplicationListGet API call processor
func (e *Processor) ApplicationListGet(c echo.Context) error {
	out, err := e.fish.DB().ApplicationList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the application list: %v", err)})
		return fmt.Errorf("Unable to get the application list: %w", err)
	}

	// Filter the output by owner unless user has permission to view all applications
	if !isMethodAllowed(c, auth.ApplicationServiceListAll) {
		userName := getUserName(c)

		var ownerOut []types.Application
		for _, app := range out {
			if app.OwnerName == userName {
				ownerOut = append(ownerOut, app)
			}
		}
		out = ownerOut
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationGet API call processor
func (e *Processor) ApplicationGet(c echo.Context, uid types.ApplicationUID) error {
	// Only the owner of the application or users with view permission can request it
	app, err := e.fish.DB().ApplicationGet(uid)
	if app == nil || !isUserName(c, app.OwnerName) {
		if !isMethodAllowed(c, auth.ApplicationServiceGetAll) {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner and authorized users can request the Application"})
			return fmt.Errorf("Only the owner and authorized users can request the Application")
		}
	}
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application not found: %v", err)})
		return fmt.Errorf("Application not found: %w", err)
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
	data.OwnerName = getUserName(c)

	if err := e.fish.DB().ApplicationCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create application: %v", err)})
		return fmt.Errorf("Unable to create application: %w", err)
	}

	log.Debug("API: Created new Application:", data.UID)

	return c.JSON(http.StatusOK, data)
}

// ApplicationResourceGet API call processor
func (e *Processor) ApplicationResourceGet(c echo.Context, uid types.ApplicationUID) error {
	// Only the owner of the application or users with resource view permission can request the resource
	app, err := e.fish.DB().ApplicationGet(uid)
	if app == nil || !isUserName(c, app.OwnerName) {
		if !isMethodAllowed(c, auth.ApplicationServiceGetResourceAll) {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner and authorized users can request the Application resource"})
			return fmt.Errorf("Only the owner and authorized users can request the Application resource")
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	out, err := e.fish.DB().ApplicationResourceGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ApplicationResource not found: %v", err)})
		return fmt.Errorf("ApplictionResource not found: %w", err)
	}

	// It's not a good idea to show the resource authentication params, internal use only
	out.Authentication = nil

	return c.JSON(http.StatusOK, out)
}

// ApplicationStateGet API call processor
func (e *Processor) ApplicationStateGet(c echo.Context, uid types.ApplicationUID) error {
	// Only the owner of the application or users with state view permission can request the status
	app, err := e.fish.DB().ApplicationGet(uid)
	if app == nil || !isUserName(c, app.OwnerName) {
		if !isMethodAllowed(c, auth.ApplicationServiceGetStateAll) {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner and authorized users can request the Application status"})
			return fmt.Errorf("Only the owner and authorized users can request the Application status")
		}
	}
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	out, err := e.fish.DB().ApplicationStateGetByApplication(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Application status not found: %v", err)})
		return fmt.Errorf("Application status not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationTaskListGet API call processor
func (e *Processor) ApplicationTaskListGet(c echo.Context, appUID types.ApplicationUID) error {
	// Only the owner of the application or users with task view permission can get the tasks
	app, err := e.fish.DB().ApplicationGet(appUID)
	if app == nil || !isUserName(c, app.OwnerName) {
		if !isMethodAllowed(c, auth.ApplicationServiceListTaskAll) {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner of Application & authorized users can get the Application Tasks"})
			return fmt.Errorf("Only the owner of Application & authorized users can get the Application Tasks")
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", appUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", appUID, err)
	}

	out, err := e.fish.DB().ApplicationTaskListByApplication(appUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the Application Tasks list: %v", err)})
		return fmt.Errorf("Unable to get the Application Tasks list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ApplicationTaskCreatePost API call processor
func (e *Processor) ApplicationTaskCreatePost(c echo.Context, appUID types.ApplicationUID) error {
	// Only the owner of the application or users with task create permission can create tasks
	app, err := e.fish.DB().ApplicationGet(appUID)
	if app == nil || !isUserName(c, app.OwnerName) {
		c.JSON(http.StatusBadRequest, H{"message": "Only the owner of Application & authorized users can create the Application Tasks"})
		return fmt.Errorf("Only the owner of Application & authorized users can create the Application Tasks")
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", appUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", appUID, err)
	}

	var data types.ApplicationTask
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	// Set Application UID for the task forcefully to not allow creating tasks for the other Apps
	data.ApplicationUID = appUID

	if err := e.fish.DB().ApplicationTaskCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create ApplicationTask: %v", err)})
		return fmt.Errorf("Unable to create ApplicationTask: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

// ApplicationTaskGet API call processor
func (e *Processor) ApplicationTaskGet(c echo.Context, taskUID types.ApplicationTaskUID) error {
	task, err := e.fish.DB().ApplicationTaskGet(taskUID)
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the ApplicationTask: %s", taskUID)})
		return fmt.Errorf("Unable to find the ApplicationTask: %s, %w", taskUID, err)
	}

	// Only the owner of the application or users with task view permission can get the task
	app, err := e.fish.DB().ApplicationGet(task.ApplicationUID)
	if app == nil || !isUserName(c, app.OwnerName) {
		if !isMethodAllowed(c, auth.ApplicationTaskServiceGetAll) {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner of Application & authorized users can get the ApplicationTask"})
			return fmt.Errorf("Only the owner of Application & authorized users can get the ApplicationTask")
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", task.ApplicationUID)})
		return fmt.Errorf("Unable to find the Application: %s, %w", task.ApplicationUID, err)
	}

	return c.JSON(http.StatusOK, task)
}

// ApplicationDeallocateGet API call processor
func (e *Processor) ApplicationDeallocateGet(c echo.Context, uid types.ApplicationUID) error {
	// Only the owner of the application or users with deallocate permission can deallocate it
	app, err := e.fish.DB().ApplicationGet(uid)
	if app == nil || !isUserName(c, app.OwnerName) {
		if !isMethodAllowed(c, auth.ApplicationServiceDeallocateAll) {
			c.JSON(http.StatusBadRequest, H{"message": "Only the owner & authorized users can deallocate the Application resource"})
			return fmt.Errorf("Only the owner & authorized users can deallocate the Application resource")
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", uid)})
		return fmt.Errorf("Unable to find the Application: %s, %w", uid, err)
	}

	as, err := e.fish.DB().ApplicationDeallocate(uid, getUserName(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to deallocate the Application: %s", uid)})
		return fmt.Errorf("Unable to deallocate the Application: %s, %w", uid, err)
	}

	return c.JSON(http.StatusOK, as)
}

// LabelListGet API call processor
func (e *Processor) LabelListGet(c echo.Context, params types.LabelListGetParams) error {
	out, err := e.fish.DB().LabelList(params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the label list: %v", err)})
		return fmt.Errorf("Unable to get the label list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// LabelGet API call processor
func (e *Processor) LabelGet(c echo.Context, uid types.LabelUID) error {
	out, err := e.fish.DB().LabelGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label not found: %v", err)})
		return fmt.Errorf("Label not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// LabelCreatePost API call processor
func (e *Processor) LabelCreatePost(c echo.Context) error {
	var data types.Label
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}
	if err := e.fish.DB().LabelCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create label: %v", err)})
		return fmt.Errorf("Unable to create label: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

// LabelDelete API call processor
func (e *Processor) LabelDelete(c echo.Context, uid types.LabelUID) error {
	err := e.fish.DB().LabelDelete(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("Label delete failed with error: %v", err)})
		return fmt.Errorf("Label delete failed with error: %w", err)
	}

	return c.JSON(http.StatusOK, H{"message": "Label removed"})
}

// NodeListGet API call processor
func (e *Processor) NodeListGet(c echo.Context) error {
	out, err := e.fish.DB().NodeList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the node list: %v", err)})
		return fmt.Errorf("Unable to get the node list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// NodeThisGet API call processor
func (e *Processor) NodeThisGet(c echo.Context) error {
	node := e.fish.DB().GetNode()

	return c.JSON(http.StatusOK, node)
}

// NodeThisMaintenanceGet API call processor
func (e *Processor) NodeThisMaintenanceGet(c echo.Context, params types.NodeThisMaintenanceGetParams) error {
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
	out := e.fish.VoteActiveList()

	return c.JSON(http.StatusOK, out)
}

// ServiceMappingGet API call processor
func (e *Processor) ServiceMappingGet(c echo.Context, uid types.ServiceMappingUID) error {
	// TODO: move to Gate since part of ProxySocks gate
	out, err := e.fish.DB().ServiceMappingGet(uid)
	if err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping not found: %v", err)})
		return fmt.Errorf("ServiceMapping not found: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ServiceMappingListGet API call processor
func (e *Processor) ServiceMappingListGet(c echo.Context) error {
	// TODO: move to Gate since part of ProxySocks gate
	out, err := e.fish.DB().ServiceMappingList()
	if err != nil {
		c.JSON(http.StatusInternalServerError, H{"message": fmt.Sprintf("Unable to get the servicemappings list: %v", err)})
		return fmt.Errorf("Unable to get the servicemappings list: %w", err)
	}

	return c.JSON(http.StatusOK, out)
}

// ServiceMappingCreatePost API call processor
func (e *Processor) ServiceMappingCreatePost(c echo.Context) error {
	// TODO: move to Gate since part of ProxySocks gate
	var data types.ServiceMapping
	if err := c.Bind(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"error": fmt.Sprintf("Wrong request body: %v", err)})
		return fmt.Errorf("Wrong request body: %w", err)
	}

	if data.ApplicationUID != uuid.Nil {
		// Check if user has permission to manage service mappings for this application
		app, err := e.fish.DB().ApplicationGet(data.ApplicationUID)
		if err != nil {
			c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to find the Application: %s", data.ApplicationUID)})
			return fmt.Errorf("Unable to find the Application: %s, %w", data.ApplicationUID, err)
		}

		// User needs either application ownership or special permission
		if !isUserName(c, app.OwnerName) {
			c.JSON(http.StatusForbidden, H{"message": "Insufficient permissions"})
			return fmt.Errorf("Insufficient permissions")
		}
	}

	if err := e.fish.DB().ServiceMappingCreate(&data); err != nil {
		c.JSON(http.StatusBadRequest, H{"message": fmt.Sprintf("Unable to create service mapping: %v", err)})
		return fmt.Errorf("Unable to create service mapping: %w", err)
	}

	return c.JSON(http.StatusOK, data)
}

// ServiceMappingDelete API call processor
func (e *Processor) ServiceMappingDelete(c echo.Context, uid types.ServiceMappingUID) error {
	// TODO: move to Gate since part of ProxySocks gate
	if err := e.fish.DB().ServiceMappingDelete(uid); err != nil {
		c.JSON(http.StatusNotFound, H{"message": fmt.Sprintf("ServiceMapping %s delete failed with error: %v", uid, err)})
		return fmt.Errorf("ServiceMapping %s delete failed with error: %w", uid, err)
	}

	return c.JSON(http.StatusOK, H{"message": "ServiceMapping removed"})
}
