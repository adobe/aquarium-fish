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

package tests

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_rbac_permissions verifies that:
// 1. A new user without any role cannot access any resources
// 2. A user with User role can:
//   - List labels
//   - Create and manage their own applications
//   - Cannot see or manage other users' applications
//
// 3. A user with Administrator role can access and manage all resources
func Test_rbac_permissions(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: tr,
	}

	// Create test users
	var regularUser, powerUser types.UserAPIPassword
	regularUser.Name = "regular-user"
	regularUser.Password = "regular-pass"
	powerUser.Name = "power-user"
	powerUser.Password = "power-pass"

	t.Run("Admin: Create test users", func(t *testing.T) {
		// Create regular user
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/")).
			JSON(regularUser).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()

		// Create power user
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/")).
			JSON(powerUser).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	// Test regular user without any role
	t.Run("Regular user without role: Access denied", func(t *testing.T) {
		// Try to list labels
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Try to create application
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(map[string]string{"label_UID": uuid.New().String()}).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Try to list users
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/user/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Try to list roles
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/role/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Try to list votes
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/vote/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Try to list nodes
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Try to check current node
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()
	})

	// Assign User role to regular user
	t.Run("Admin: Assign User role to regular user", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/"+regularUser.Name+"/roles")).
			JSON([]string{"User"}).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	// Create a test label
	var label types.Label
	t.Run("Admin: Create test label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)
	})

	// Test regular user with User role
	var regularUserApp types.Application
	t.Run("Regular user with User role: Basic operations", func(t *testing.T) {
		// Should be able to list labels
		var labels []types.Label
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) == 0 {
			t.Fatal("Expected to see labels")
		}

		// Should be able to create application
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(map[string]string{"label_UID": label.UID.String()}).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&regularUserApp)

		// Should be able to view own application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+regularUserApp.UID.String())).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End()

		// Should NOT be able to list users
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/user/")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()

		// Should NOT be able to create labels
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label-2", "version":1, "definitions": [{"driver":"test"}]}`).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusForbidden).
			End()
	})

	// Create another application as admin
	var adminApp types.Application
	t.Run("Admin: Create another application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(map[string]string{"label_UID": label.UID.String()}).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&adminApp)
	})

	t.Run("Regular user: Cannot access admin's application", func(t *testing.T) {
		// Should NOT be able to view admin's application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+adminApp.UID.String())).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusBadRequest). // Returns 400 because the app is not found for this user
			End()

		// Should NOT be able to deallocate admin's application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+adminApp.UID.String()+"/deallocate")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusBadRequest).
			End()
	})

	// Assign Administrator role to power user
	t.Run("Admin: Assign Administrator role to power user", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/"+powerUser.Name+"/roles")).
			JSON([]string{"Administrator"}).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Power user with Administrator role: Full access", func(t *testing.T) {
		// Should be able to list users
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/user/")).
			BasicAuth(powerUser.Name, powerUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End()

		// Should be able to create labels
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"admin-label", "version":1, "definitions": [{"driver":"test"}]}`).
			BasicAuth(powerUser.Name, powerUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End()

		// Should be able to view regular user's application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+regularUserApp.UID.String())).
			BasicAuth(powerUser.Name, powerUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End()

		// Should be able to view admin's application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+adminApp.UID.String())).
			BasicAuth(powerUser.Name, powerUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	// Test application cleanup
	t.Run("Cleanup: Deallocate applications", func(t *testing.T) {
		// Regular user deallocates their application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+regularUserApp.UID.String()+"/deallocate")).
			BasicAuth(regularUser.Name, regularUser.Password).
			Expect(t).
			Status(http.StatusOK).
			End()

		// Admin deallocates their application
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+adminApp.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})
}
