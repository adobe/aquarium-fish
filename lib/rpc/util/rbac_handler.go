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
	"fmt"
	"net/http"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/log"
)

// RBACHandler is a HTTP middleware that handles authorization
type RBACHandler struct {
	enforcer *auth.Enforcer
}

// NewRBACHandler creates a new RBAC handler
func NewRBACHandler(enforcer *auth.Enforcer) *RBACHandler {
	return &RBACHandler{enforcer: enforcer}
}

// Handler implements HTTP middleware for authorization
func (h *RBACHandler) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service, method := getServiceMethodFromPath(r.URL.Path)
		if service == "" || method == "" {
			log.WithFunc("rpc", "rbac").Debug("Invalid service/method path", "path", r.URL.Path)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		if err := h.checkPermission(r.Context(), service, method); err != nil {
			log.WithFunc("rpc", "rbac").Debug("Permission denied", "err", err)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Add rbac service & method to context
		ctx := context.WithValue(r.Context(), rbacServiceContextKey, service)
		ctx = context.WithValue(ctx, rbacMethodContextKey, method)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// checkPermission checks if the current user has permission to access the procedure
func (h *RBACHandler) checkPermission(ctx context.Context, service, method string) error {
	// Ignore the service/method when it's in auth rbacExclude list
	if auth.IsEcludedFromRBAC(service, method) {
		return nil
	}

	// Get user roles from context
	user := GetUserFromContext(ctx)
	if user == nil {
		return fmt.Errorf("no user found in context")
	}

	// Check if any role has permission
	if !h.enforcer.CheckPermission(user.Roles, service, method) {
		return fmt.Errorf("permission denied")
	}

	return nil
}
