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
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/auth"
)

// RBACInterceptor implements RBAC validation using Casbin enforcer
type RBACInterceptor struct {
	enforcer *auth.Enforcer
}

const rbacServiceContextKey = contextKey("rbac_service")
const rbacMethodContextKey = contextKey("rbac_method")

// GetServiceFromContext retrieves the service from context
func GetServiceFromContext(ctx context.Context) string {
	service, _ := ctx.Value(rbacServiceContextKey).(string)
	return service
}

// GetMethodFromContext retrieves the service from context
func GetMethodFromContext(ctx context.Context) string {
	method, _ := ctx.Value(rbacMethodContextKey).(string)
	return method
}

// NewRBACInterceptor creates a new RBAC interceptor
func NewRBACInterceptor(enforcer *auth.Enforcer) *RBACInterceptor {
	return &RBACInterceptor{
		enforcer: enforcer,
	}
}

// WrapUnary implements connect.Interceptor
func (i *RBACInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		service, method := getServiceMethod(req.Spec().Procedure)
		if err := i.checkPermission(ctx, service, method); err != nil {
			return nil, err
		}
		// Add rbac service & method to context
		ctx = context.WithValue(ctx, rbacServiceContextKey, service)
		ctx = context.WithValue(ctx, rbacMethodContextKey, method)
		return next(ctx, req)
	}
}

// WrapStreamingClient implements connect.Interceptor
func (i *RBACInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		// We don't support streaming yet, so just pass through
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements connect.Interceptor
func (i *RBACInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		service, method := getServiceMethod(conn.Spec().Procedure)
		if err := i.checkPermission(ctx, service, method); err != nil {
			return err
		}
		// Add rbac service & method to context
		ctx = context.WithValue(ctx, rbacServiceContextKey, service)
		ctx = context.WithValue(ctx, rbacMethodContextKey, method)
		return next(ctx, conn)
	}
}

// checkPermission checks if the current user has permission to access the procedure
func (i *RBACInterceptor) checkPermission(ctx context.Context, service, method string) error {
	// Get user roles from context
	user := GetUserFromContext(ctx)
	if user == nil {
		return connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("no user found in context"))
	}

	// Check if any role has permission
	if !i.enforcer.CheckPermission(user.Roles, service, method) {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied"))
	}

	return nil
}

// getServiceMethod returns service and  method of the procedure
func getServiceMethod(procedure string) (string, string) {
	// Format: /package.service/method
	parts := strings.Split(strings.TrimPrefix(procedure, "/"), "/")
	if len(parts) != 2 {
		return "", ""
	}

	service := parts[0]
	sub := strings.Split(service, ".")
	if len(sub) > 1 {
		service = sub[len(sub)-1]
	}

	method := parts[1]

	return service, method
}
