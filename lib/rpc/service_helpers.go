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

	"github.com/adobe/aquarium-fish/lib/auth"
)

// checkPermission verifies if the user has the required permission
// Returns true if the user has permission, false otherwise
func checkPermission(ctx context.Context, method string) bool {
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

// isUserName ensures the provided name and user's name are the same
func isUserName(ctx context.Context, name string) bool {
	return name == getUserName(ctx)
}

// getUserName returns logged in user name or empty string
func getUserName(ctx context.Context) string {
	user := GetUserFromContext(ctx)
	if user == nil {
		return ""
	}

	return user.Name
}
