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

package util

import (
	"context"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/database"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

var interceptors connect.Option

type contextKey string

const (
	userContextKey        = contextKey("user")
	rbacServiceContextKey = contextKey("rbac_service")
	rbacMethodContextKey  = contextKey("rbac_method")
)

// GetUserFromContext retrieves the user from context
func GetUserFromContext(ctx context.Context) *typesv2.User {
	if user, ok := ctx.Value(userContextKey).(*typesv2.User); ok {
		return user
	}
	return nil
}

// GetServiceFromContext retrieves the service from context
func GetServiceFromContext(ctx context.Context) string {
	if service, ok := ctx.Value(rbacServiceContextKey).(string); ok {
		return service
	}
	return ""
}

// GetMethodFromContext retrieves the service from context
func GetMethodFromContext(ctx context.Context) string {
	if method, ok := ctx.Value(rbacMethodContextKey).(string); ok {
		return method
	}
	return ""
}

// CheckUserPermission verifies if the user has the required permission
// Returns true if the user has permission, false otherwise
func CheckUserPermission(ctx context.Context, method string) bool {
	enforcer := auth.GetEnforcer()
	if enforcer == nil {
		return false
	}

	user := GetUserFromContext(ctx)
	if user == nil {
		return false
	}

	service := GetServiceFromContext(ctx)

	// Check if user has permission
	return enforcer.CheckPermission(user.Roles, service, method)
}

// IsUserName ensures the provided name and user's name are the same
func IsUserName(ctx context.Context, name string) bool {
	return name == GetUserName(ctx)
}

// GetUserName returns logged in user name or empty string
func GetUserName(ctx context.Context) string {
	user := GetUserFromContext(ctx)
	if user == nil {
		return ""
	}

	return user.Name
}

// GetInterceptors returns common set of interceptors for RPC
func GetInterceptors(db *database.Database) connect.Option {
	if interceptors == nil {
		// Create interceptors
		authInterceptor := NewAuthInterceptor(db)
		rbacInterceptor := NewRBACInterceptor(auth.GetEnforcer())

		// Store in module var for future use
		interceptors = connect.WithInterceptors(
			authInterceptor,
			rbacInterceptor,
		)
	}
	return interceptors
}
