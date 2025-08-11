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

// Test_simple_role_creation tests the complete lifecycle of role creation and editing
// This test verifies that roles can be created with permissions and the permissions
// are properly saved and can be retrieved when editing the role
func Test_simple_role_creation(t *testing.T) {
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

	const testRoleName = "test-role"

	afp.Run(t, "Login as admin user", func(t *testing.T) {
		// Login as admin using correct admin token
		hp.LoginUser(t, page, afp, afi, "admin", afi.AdminToken())
	})

	afp.Run(t, "Create test role with permissions", func(t *testing.T) {
		// Navigate to manage page
		hp.NavigateToPage(t, page, "manage")

		// Click on Roles tab
		err := page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Roles"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Roles tab: %v", err)
		}

		// Click Create Role button
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Create Role"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Create Role button: %v", err)
		}

		// Wait for create role modal
		err = page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Create Role"}).
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Create Role modal did not appear: %v", err)
		}

		// Fill role name
		err = page.Locator("label:has-text('Name *')").
			Locator("..").
			Locator("..").
			Locator("input").
			Fill(testRoleName)
		if err != nil {
			t.Fatalf("ERROR: Could not fill Name field: %v", err)
		}

		// Add first permission
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "+ Add Permissions"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click + Add Permissions button: %v", err)
		}

		// Wait for permission form to appear
		err = page.Locator("text=Permissions 1").WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Permission form did not appear: %v", err)
		}

		// Fill first permission resource
		err = page.Locator("label:has-text('Resource *')").
			Locator("..").
			Locator("..").
			Locator("..").
			Locator("input").
			First().
			Fill("test-resource")
		if err != nil {
			t.Fatalf("ERROR: Could not fill first permission Resource field: %v", err)
		}

		// Fill first permission action
		err = page.Locator("label:has-text('Action *')").
			Locator("..").
			Locator("..").
			Locator("..").
			Locator("input").
			First().
			Fill("read")
		if err != nil {
			t.Fatalf("ERROR: Could not fill first permission Action field: %v", err)
		}

		// Add second permission
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "+ Add Permissions"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click + Add Permissions button for second permission: %v", err)
		}

		// Wait for second permission form to appear
		err = page.Locator("text=Permissions 2").WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Second permission form did not appear: %v", err)
		}

		// Fill second permission resource
		err = page.Locator("label:has-text('Resource *')").
			Locator("..").
			Locator("..").
			Locator("..").
			Locator("input").
			Last().
			Fill("another-resource")
		if err != nil {
			t.Fatalf("ERROR: Could not fill second permission Resource field: %v", err)
		}

		// Fill second permission action
		err = page.Locator("label:has-text('Action *')").
			Locator("..").
			Locator("..").
			Locator("..").
			Locator("input").
			Last().
			Fill("write")
		if err != nil {
			t.Fatalf("ERROR: Could not fill second permission Action field: %v", err)
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
		err = page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Create Role"}).
			Last().
			WaitFor(playwright.LocatorWaitForOptions{
				State:   playwright.WaitForSelectorStateHidden,
				Timeout: playwright.Float(2000),
			})
		if err != nil {
			t.Errorf("ERROR: Modal did not close as expected: %v", err)
		}
	})

	afp.Run(t, "Verify role appears in list", func(t *testing.T) {
		// Wait for role to appear in list
		roleItem := page.GetByRole(*playwright.AriaRoleListitem).
			Filter(playwright.LocatorFilterOptions{HasText: testRoleName}).
			First()

		err := roleItem.WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Role %s did not appear in list: %v", testRoleName, err)
		}

		// Verify role shows correct number of permissions
		if err := roleItem.GetByText("Permissions: 2").WaitFor(); err != nil {
			t.Fatalf("ERROR: Role permissions count not displayed correctly: %v", err)
		}

		t.Logf("INFO: Successfully created role %s with 2 permissions", testRoleName)
	})

	afp.Run(t, "Edit role and verify permissions", func(t *testing.T) {
		// Find the role row and click edit
		roleElement := page.GetByRole(*playwright.AriaRoleListitem).
			Filter(playwright.LocatorFilterOptions{HasText: testRoleName}).
			First()

		if err := roleElement.WaitFor(); err != nil {
			t.Fatalf("ERROR: Role %s not found in list: %v", testRoleName, err)
		}

		editBtn := roleElement.GetByRole(*playwright.AriaRoleButton, playwright.LocatorGetByRoleOptions{Name: "Edit"})
		if err := editBtn.WaitFor(); err != nil {
			t.Fatalf("ERROR: Edit button not found for role %s: %v", testRoleName, err)
		}

		// Click edit button
		if err := editBtn.Click(); err != nil {
			t.Fatalf("ERROR: Could not click Edit button for role %s: %v", testRoleName, err)
		}

		// Wait for edit modal to appear
		err := page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Edit Role: " + testRoleName}).
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Edit Role modal did not appear: %v", err)
		}

		// Verify first permission has correct values
		err = page.Locator("text=Permissions 1").
			Locator("..").
			Locator("..").
			Locator("input[value='test-resource']").
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: First permission resource value 'test-resource' not found: %v", err)
		}

		err = page.Locator("text=Permissions 1").
			Locator("..").
			Locator("..").
			Locator("input[value='read']").
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: First permission action value 'read' not found: %v", err)
		}

		// Verify second permission has correct values
		err = page.Locator("text=Permissions 2").
			Locator("..").
			Locator("..").
			Locator("input[value='another-resource']").
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Second permission resource value 'another-resource' not found: %v", err)
		}

		err = page.Locator("text=Permissions 2").
			Locator("..").
			Locator("..").
			Locator("input[value='write']").
			WaitFor()
		if err != nil {
			t.Fatalf("ERROR: Second permission action value 'write' not found: %v", err)
		}

		// Close edit modal
		err = page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Cancel"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not click Cancel button in edit modal: %v", err)
		}

		// Wait for modal to close
		err = page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Edit Role: " + testRoleName}).
			WaitFor(playwright.LocatorWaitForOptions{
				State:   playwright.WaitForSelectorStateHidden,
				Timeout: playwright.Float(2000),
			})
		if err != nil {
			t.Errorf("ERROR: Edit modal did not close as expected: %v", err)
		}

		t.Logf("INFO: Successfully verified role %s permissions in edit mode", testRoleName)
	})
}
