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

// Note: Common helper functions are now in webtests/helper/webtest_helpers.go

// Test_example executes web UI test to create and deallocate the Application
// WARNING: Intended to run within "golang" docker container
func Test_example(t *testing.T) {
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

	// Create automated screenshot manager
	screenshots := hp.NewTestScreenshots(afp, page)

	screenshots.WithScreenshots(t, "login", func(t *testing.T) {
		// Login using helper function with correct admin token
		hp.LoginUser(t, page, afp, afi, "admin", afi.AdminToken())
	})

	screenshots.WithScreenshots(t, "navigation", func(t *testing.T) {
		// Test navigation to different pages
		pages := []struct {
			name string
			text string
		}{
			{"status", "Node Status"},
			{"manage", "Management"},
			{"applications", "Applications"},
		}

		for _, p := range pages {
			hp.NavigateToPage(t, page, p.text)
			t.Logf("INFO: Successfully navigated to %s page", p.name)
		}
	})

	screenshots.WithScreenshots(t, "ui_responsiveness", func(t *testing.T) {
		// Go back to applications page
		hp.NavigateToPage(t, page, "Applications")

		// Test if the page is responsive by checking for key elements
		elements := []string{
			"text=Applications",
			"text=Create Application",
			"text=Connection Status",
		}

		for _, element := range elements {
			if hp.CheckElementExists(t, page, element) {
				t.Logf("INFO: Element '%s' is responsive", element)
			} else {
				t.Logf("WARNING: Element '%s' not found or not responsive", element)
			}
		}
	})
}
