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

package tests

import (
	"testing"
	"time"

	h "github.com/adobe/aquarium-fish/tests/helper"
	hp "github.com/adobe/aquarium-fish/webtests/helper"

	"github.com/playwright-community/playwright-go"
)

// Test_NonAdminUserWorkflow tests the complete workflow for non-admin users
// This test verifies user creation, logout/login functionality, and basic UI access
func Test_NonAdminUserWorkflow(t *testing.T) {
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

	afp, page := hp.NewPlaywright(t, afi.Workspace(), playwright.BrowserNewContextOptions{
		IgnoreHttpsErrors: playwright.Bool(true),
		ColorScheme:       playwright.ColorSchemeDark,
	})

	// Create automated screenshot manager
	screenshots := hp.NewTestScreenshots(afp, page)

	const testUsername = "testuser"
	const testPassword = "testpass123"

	screenshots.WithScreenshots(t, "admin_login_and_user_creation", func(t *testing.T) {
		// Login as admin with correct admin token
		hp.LoginUser(t, page, afp, afi, "admin", afi.AdminToken())

		// Create test user with User role
		hp.CreateUser(t, page, afp, testUsername, testPassword, []string{"User"})

		t.Log("INFO: User created successfully")
	})

	screenshots.WithScreenshots(t, "logout_and_login_as_testuser", func(t *testing.T) {
		// Logout admin user
		hp.LogoutUser(t, page, afp, afi)

		// Login as test user
		hp.LoginUser(t, page, afp, afi, testUsername, testPassword)

		t.Log("INFO: Successfully logged in as test user")
	})

	screenshots.WithScreenshots(t, "test_navigation", func(t *testing.T) {
		// Test navigation to different pages
		navigationTests := []struct {
			linkText string
			pageName string
		}{
			{"Applications", "Applications"},
			{"Node Status", "Status"},
			{"Management", "Management"},
		}

		for _, nav := range navigationTests {
			hp.NavigateToPage(t, page, nav.linkText)
			t.Logf("INFO: Successfully navigated to %s page", nav.pageName)
		}

		// Go back to applications page for next test
		hp.NavigateToPage(t, page, "Applications")
	})

	screenshots.WithScreenshots(t, "create_application_as_testuser", func(t *testing.T) {
		// Check if Create Application button is available
		if !hp.CheckElementExists(t, page, "text=Create Application") {
			t.Fatalf("ERROR: Create Application button not available for test user")
		}

		// YAML configuration for test user application
		yamlConfig := `labelUid: "testuser-app-label"
metadata:
  USER_TYPE: "test-user"
  CREATED_BY: "` + testUsername + `"
  DESCRIPTION: "Application created by test user"`

		// Create application using helper
		hp.CreateApplication(t, page, afp, yamlConfig, "testuser-app-label")

		// Verify application details
		hp.VerifyApplicationInList(t, page, "testuser-app-label", testUsername)

		t.Log("INFO: Test user successfully created application")
	})

	screenshots.WithScreenshots(t, "test_application_management", func(t *testing.T) {
		// Test if user can deallocate their own application
		if hp.CheckElementExists(t, page, "text=Deallocate") {
			t.Log("INFO: Test user has deallocate permissions")
		} else {
			t.Log("INFO: Deallocate button not available for test user - this might be expected")
		}

		// Test SSH Access button
		if hp.CheckElementExists(t, page, "text=SSH Access") {
			t.Log("INFO: Test user has SSH access permissions")
		} else {
			t.Log("INFO: SSH Access button not available for test user")
		}

		// Test application filtering
		filterSelect := page.Locator("select").Filter(playwright.LocatorFilterOptions{
			HasText: "Mine",
		})

		if _, err := filterSelect.SelectOption(playwright.SelectOptionValues{
			Values: &[]string{"mine"},
		}); err != nil {
			t.Logf("WARNING: Could not test application filtering: %v", err)
		} else {
			// Verify only user's applications are shown
			hp.WaitForElement(t, page, "text=testuser-app-label", 5*time.Second)
		}

		t.Log("INFO: Non-admin user workflow completed successfully")
	})
}
