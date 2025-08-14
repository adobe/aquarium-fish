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

// Test_simple_application_admin tests the complete lifecycle of application creation and deallocation by admin user
// This test verifies that applications appear in the list without page refresh and can be deallocated
func Test_simple_application_admin(t *testing.T) {
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

	const testLabel = "test-label"

	afp.Run(t, "Login as admin user", func(t *testing.T) {
		// Login as admin using correct admin token
		hp.LoginUser(t, page, "admin", afi.AdminToken())
	})

	var labelUID string
	afp.Run(t, "Create test label", func(t *testing.T) {
		// First create a label that applications can use
		labelUID = hp.CreateLabel(t, page, testLabel)
	})

	afp.Run(t, "Create application", func(t *testing.T) {
		// Create application using form interface
		hp.CreateApplication(t, page, labelUID, map[string]string{
			"TEST_VAR":    "test-value",
			"DESCRIPTION": "Test application created by admin",
		})
	})

	afp.Run(t, "Check list of apps got update that Application is Allocated", func(t *testing.T) {
		// Verify application details in list
		hp.VerifyApplicationInList(t, page, testLabel, "admin", "Allocated")
	})

	afp.Run(t, "Deallocate application", func(t *testing.T) {
		// Deallocate the application using helper
		hp.DeallocateApplication(t, page, testLabel)
	})

	afp.Run(t, "Check list of apps got update that Application is Deallocated", func(t *testing.T) {
		// Verify second application appears in list
		hp.VerifyApplicationInList(t, page, testLabel, "admin", "Deallocated")
	})
}
