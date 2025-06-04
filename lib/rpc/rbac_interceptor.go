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
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/adobe/aquarium-fish/lib/auth"
)

// RBACInterceptor implements RBAC validation using Casbin enforcer
type RBACInterceptor struct {
	enforcer *auth.Enforcer
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
		if err := i.checkPermission(ctx, req.Spec().Procedure); err != nil {
			return nil, err
		}
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
		if err := i.checkPermission(ctx, conn.Spec().Procedure); err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// checkPermission checks if the current user has permission to access the procedure
func (i *RBACInterceptor) checkPermission(ctx context.Context, procedure string) error {
	// Get method descriptor
	methodDesc, err := i.getMethodDescriptor(procedure)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get method descriptor: %w", err))
	}

	// RBAC extensions
	var allowUnauthenticated bool

	// Get RBAC options
	opts := methodDesc.Options()
	if opts != nil {
		// If opts are specified - then we need to process them
		methodDesc.Options().ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if fd.Number() == 50000 { // Our RBAC extension number
				msg := v.Message()
				/*// Example to extend the options for RBAC
				rolesField := msg.Get(msg.Descriptor().Fields().ByName("allowed_roles")).List()
				for i := 0; i < rolesField.Len(); i++ {
					allowedRoles = append(allowedRoles, rolesField.Get(i).String())
				}*/
				// Get allow_unauthenticated
				allowUnauthenticated = msg.Get(msg.Descriptor().Fields().ByName("allow_unauthenticated")).Bool()
				return false
			}
			return true
		})
	}

	// Allow unauthenticated access if specified
	if allowUnauthenticated {
		return nil
	}

	// Get user roles from context
	user := GetUserFromContext(ctx)
	if user == nil {
		return connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("no user found in context"))
	}

	// Get service name from procedure
	serviceName := strings.Split(procedure, "/")[1] // Format: /package.service/method

	// Check if any role has permission
	if !i.enforcer.CheckPermission(user.Roles, serviceName, string(methodDesc.Name())) {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied"))
	}

	return nil
}

// getMethodDescriptor returns the method descriptor for the given procedure
func (i *RBACInterceptor) getMethodDescriptor(procedure string) (protoreflect.MethodDescriptor, error) {
	// Format: /package.service/method
	parts := strings.Split(strings.TrimPrefix(procedure, "/"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid procedure name: %s", procedure)
	}

	servicePath := parts[0]
	methodName := parts[1]

	// Find service descriptor
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(servicePath))
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	serviceDesc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("not a service descriptor: %s", servicePath)
	}

	// Find method descriptor
	methodDesc := serviceDesc.Methods().ByName(protoreflect.Name(methodName))
	if methodDesc == nil {
		return nil, fmt.Errorf("method not found: %s", methodName)
	}

	return methodDesc, nil
}
