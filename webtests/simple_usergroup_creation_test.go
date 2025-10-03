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

	h "github.com/adobe/aquarium-fish/tests/helper"
	hp "github.com/adobe/aquarium-fish/webtests/helper"

	"github.com/playwright-community/playwright-go"
)

// Test_simple_usergroup_creation tests the complete lifecycle of user group creation and editing
// This test verifies that user groups can be created with users and configuration, and the data
// is properly saved and can be retrieved when editing the user group
func Test_simple_usergroup_creation(t *testing.T) {
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	afp, page := hp.NewPlaywright(t, afi.Workspace(), playwright.BrowserNewContextOptions{
		IgnoreHttpsErrors: playwright.Bool(true),
		ColorScheme:       playwright.ColorSchemeDark,
	})

	// Go to WebUI
	if _, err := page.Goto(afi.APIAddress(""), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("ERROR: Could not goto Web Dashboard page: %v", err)
	}

	const testUserGroupName = "test-usergroup"
	const testUser1 = "user1"
	const testUser2 = "user2"

	afp.Run(t, "Login as admin user", func(t *testing.T) {
		// Login as admin using correct admin token
		hp.LoginUser(t, page, "admin", afi.AdminToken())
	})

	afp.Run(t, "Create test users for the group", func(t *testing.T) {
		// Create first test user
		hp.CreateUser(t, page, testUser1, "password123", []string{"admin"})

		// Create second test user
		hp.CreateUser(t, page, testUser2, "password456", []string{"admin"})
	})

	afp.Run(t, "Create test user group with users and config", func(t *testing.T) {
		// Navigate to manage page
		hp.NavigateToPage(t, page, "manage")

		// Click on User Groups tab
		err := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "User Groups"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click User Groups tab: %v", err)
		}

		// Click Create User Group button
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Create User Group"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Create User Group button: %v", err)
		}

		// Wait for create user group modal
		err = page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Create User Group"}).
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Create User Group modal did not appear: %v", err)
		}

		// Fill user group name
		err = page.Locator("label:has-text('Name *')").
			Locator("..").
			Locator("..").
			Locator("input").
			Fill(testUserGroupName)
		if err != nil {
			t.Fatalf("ERROR: Could not fill Name field: %v", err)
		}

		// Add first user
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "+ Add Users"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click + Add Users button: %v", err)
		}

		// Fill first user
		err = page.Locator("label:has-text('Users *')").
			Locator("..").
			Locator("..").
			Locator("input").
			First().
			Fill(testUser1)
		if err != nil {
			t.Fatalf("ERROR: Could not fill first user field: %v", err)
		}

		// Add second user
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "+ Add Users"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click + Add Users button for second user: %v", err)
		}

		// Fill second user
		err = page.Locator("label:has-text('Users *')").
			Locator("..").
			Locator("..").
			Locator("input").
			Last().
			Fill(testUser2)
		if err != nil {
			t.Fatalf("ERROR: Could not fill second user field: %v", err)
		}

		// Add configuration
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Add Config"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Add Config button: %v", err)
		}

		// Wait for config section to appear and fill rate limit
		err = page.Locator("label:has-text('Rate Limit')").
			Locator("..").
			Locator("..").
			Locator("input").
			Fill("100")
		if err != nil {
			t.Fatalf("ERROR: Could not fill Rate Limit field: %v", err)
		}

		// Fill streams limit
		err = page.Locator("label:has-text('Streams Limit')").
			Locator("..").
			Locator("..").
			Locator("input").
			Fill("5")
		if err != nil {
			t.Fatalf("ERROR: Could not fill Streams Limit field: %v", err)
		}

		// Ensure no notifications are blocking the view
		hp.CloseAllNotifications(t, page)

		// Click Create button in modal
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Create"}).
			Last().
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
		}

		// Wait for modal to close
		err = page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Create User Group"}).
			Last().
			WaitFor(playwright.LocatorWaitForOptions{
				State:   playwright.WaitForSelectorStateHidden,
				Timeout: playwright.Float(2000),
			})
		if err != nil {
			t.Errorf("ERROR: Modal did not close as expected: %v", err)
		}
	})

	afp.Run(t, "Verify user group appears in list", func(t *testing.T) {
		// Wait for user group to appear in list
		userGroupItem := page.GetByRole(*playwright.AriaRoleListitem).
			Filter(playwright.LocatorFilterOptions{HasText: testUserGroupName}).
			First()

		err := userGroupItem.WaitFor()
		if err != nil {
			t.Fatalf("ERROR: User group %s did not appear in list: %v", testUserGroupName, err)
		}

		// Verify user group shows correct number of users
		if err := userGroupItem.GetByText("Users: 2").WaitFor(); err != nil {
			t.Fatalf("ERROR: User group users count not displayed correctly: %v", err)
		}

		// Verify user group shows config is present
		if err := userGroupItem.GetByText("Config: Yes").WaitFor(); err != nil {
			t.Fatalf("ERROR: User group config status not displayed correctly: %v", err)
		}

		t.Logf("INFO: Successfully created user group %s with 2 users and config", testUserGroupName)
	})

	afp.Run(t, "Edit user group and verify data persistence", func(t *testing.T) {
		// Find the user group row and click edit
		userGroupElement := page.GetByRole(*playwright.AriaRoleListitem).
			Filter(playwright.LocatorFilterOptions{HasText: testUserGroupName}).
			First()

		if err := userGroupElement.WaitFor(); err != nil {
			t.Fatalf("ERROR: User group %s not found in list: %v", testUserGroupName, err)
		}

		editBtn := userGroupElement.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{Name: "Edit"})
		if err := editBtn.WaitFor(); err != nil {
			t.Fatalf("ERROR: Edit button not found for user group %s: %v", testUserGroupName, err)
		}

		// Click edit button
		if err := editBtn.Click(); err != nil {
			t.Fatalf("ERROR: Could not click Edit button for user group %s: %v", testUserGroupName, err)
		}

		// Wait for edit modal to appear
		err := page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Edit User Group: " + testUserGroupName}).
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Edit User Group modal did not appear: %v", err)
		}

		// Verify first user has correct value
		userInputs := page.Locator("label:has-text('Users *')").
			Locator("..").
			Locator("..").
			Locator("input")

		err = userInputs.First().
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: First user input not found: %v", err)
		}

		firstUserValue, err := userInputs.First().InputValue()
		if err != nil {
			t.Fatalf("ERROR: Could not get first user input value: %v", err)
		}
		if firstUserValue != testUser1 {
			t.Fatalf("ERROR: First user value expected %s, got %s", testUser1, firstUserValue)
		}

		// Verify second user has correct value
		secondUserValue, err := userInputs.Last().InputValue()
		if err != nil {
			t.Fatalf("ERROR: Could not get second user input value: %v", err)
		}
		if secondUserValue != testUser2 {
			t.Fatalf("ERROR: Second user value expected %s, got %s", testUser2, secondUserValue)
		}

		// Verify config values are preserved
		rateLimitInput := page.Locator("label:has-text('Rate Limit')").
			Locator("..").
			Locator("..").
			Locator("input")

		err = rateLimitInput.WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Rate Limit input not found: %v", err)
		}

		rateLimitValue, err := rateLimitInput.InputValue()
		if err != nil {
			t.Fatalf("ERROR: Could not get rate limit input value: %v", err)
		}
		if rateLimitValue != "100" {
			t.Fatalf("ERROR: Rate limit value expected 100, got %s", rateLimitValue)
		}

		streamsLimitInput := page.Locator("label:has-text('Streams Limit')").
			Locator("..").
			Locator("..").
			Locator("input")

		streamsLimitValue, err := streamsLimitInput.InputValue()
		if err != nil {
			t.Fatalf("ERROR: Could not get streams limit input value: %v", err)
		}
		if streamsLimitValue != "5" {
			t.Fatalf("ERROR: Streams limit value expected 5, got %s", streamsLimitValue)
		}

		// Close edit modal
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Cancel"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Cancel button in edit modal: %v", err)
		}

		// Wait for modal to close
		err = page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Edit User Group: " + testUserGroupName}).
			WaitFor(playwright.LocatorWaitForOptions{
				State:   playwright.WaitForSelectorStateHidden,
				Timeout: playwright.Float(2000),
			})
		if err != nil {
			t.Errorf("ERROR: Edit modal did not close as expected: %v", err)
		}

		t.Logf("INFO: Successfully verified user group %s data persistence in edit mode", testUserGroupName)
	})
}
