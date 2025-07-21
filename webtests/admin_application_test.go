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

// Test_AdminApplicationLifecycle tests the complete lifecycle of application creation and deallocation by admin user
// This test verifies that applications appear in the list without page refresh and can be deallocated
func Test_AdminApplicationLifecycle(t *testing.T) {
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

	screenshots.WithScreenshots(t, "admin_login", func(t *testing.T) {
		// Login as admin using correct admin token
		hp.LoginUser(t, page, afp, afi, "admin", afi.AdminToken())
	})

	screenshots.WithScreenshots(t, "create_label", func(t *testing.T) {
		// First create a label that applications can use
		hp.CreateLabel(t, page, afp, "test-label", "test-label-uid")
	})

	screenshots.WithScreenshots(t, "create_application", func(t *testing.T) {
		// Create application using form interface
		hp.CreateApplicationForm(t, page, afp, "test-label-uid", map[string]string{
			"TEST_VAR":    "test-value",
			"DESCRIPTION": "Test application created by admin",
		})

		// Verify application details in list
		hp.VerifyApplicationInList(t, page, "test-label-uid", "admin")
	})

	screenshots.WithScreenshots(t, "deallocate_application", func(t *testing.T) {
		// Deallocate the application using helper
		hp.DeallocateApplication(t, page, "test-label-uid")
	})

	screenshots.WithScreenshots(t, "test_list_updates", func(t *testing.T) {
		// Create another label first
		hp.CreateLabel(t, page, afp, "test-label-2", "test-label-uid-2")

		// Create second application to test list updates
		hp.CreateApplicationForm(t, page, afp, "test-label-uid-2", map[string]string{
			"TEST_VAR":    "test-value-2",
			"DESCRIPTION": "Second test application",
		})

		// Verify second application appears in list
		hp.VerifyApplicationInList(t, page, "test-label-uid-2", "admin")

		t.Log("INFO: List updates working correctly without page refresh")
	})
}
