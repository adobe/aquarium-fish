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

package helper

import (
	"fmt"
	"sync"
	"testing"
	"time"

	h "github.com/adobe/aquarium-fish/tests/helper"
	"github.com/playwright-community/playwright-go"
)

// TestScreenshots manages automated screenshot taking for subtests
type TestScreenshots struct {
	mu   sync.Mutex
	step int
	afp  *AFPlaywright
	page playwright.Page
}

// NewTestScreenshots creates a new automated screenshot manager
func NewTestScreenshots(afp *AFPlaywright, page playwright.Page) *TestScreenshots {
	return &TestScreenshots{
		afp:  afp,
		page: page,
	}
}

// WithScreenshots wraps a subtest with automatic screenshot taking
func (ts *TestScreenshots) WithScreenshots(t *testing.T, name string, fn func(t *testing.T)) {
	t.Helper()

	t.Run(name, func(t *testing.T) {
		// Take screenshot at beginning of subtest
		ts.takeScreenshot(t, name, "start")

		// Defer screenshot at end of subtest
		defer ts.takeScreenshot(t, name, "end")

		// Run the actual test function
		fn(t)
	})
}

// takeScreenshot takes a screenshot with automatic naming
func (ts *TestScreenshots) takeScreenshot(t *testing.T, subtestName, phase string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Increment step counter for this subtest
	ts.step++

	// Create filename: step_subtestName_phase.png
	filename := fmt.Sprintf("%02d_%s_%s.png", ts.step, subtestName, phase)

	if _, err := ts.page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(ts.afp.CaptureDir("screenshots", filename)),
	}); err != nil {
		t.Logf("WARNING: Could not take screenshot %s: %v", filename, err)
	}
}

// LoginUser is a helper function to login with given credentials
func LoginUser(t *testing.T, page playwright.Page, afp *AFPlaywright, afi *h.AFInstance, username, password string) {
	t.Helper()

	if _, err := page.Goto(afi.APIAddress(""), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("ERROR: Could not goto login page: %v", err)
	}

	// Fill login form
	if err := page.Locator("#username").Fill(username); err != nil {
		t.Fatalf("ERROR: Could not fill username: %v", err)
	}

	if err := page.Locator("#password").Fill(password); err != nil {
		t.Fatalf("ERROR: Could not fill password: %v", err)
	}

	// Submit login form
	if err := page.Locator("button[type=submit]").Click(); err != nil {
		t.Fatalf("ERROR: Could not click login button: %v", err)
	}

	// Wait for redirect to applications page
	if err := page.WaitForURL("**/applications", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(2000),
	}); err != nil {
		t.Fatalf("ERROR: Login failed for user %s: %v", username, err)
	}

	// Verify we're on the applications page
	if err := page.GetByRole(*playwright.AriaRoleHeading, playwright.PageGetByRoleOptions{Name: "Applications"}).First().WaitFor(); err != nil {
		t.Fatalf("ERROR: Applications page not loaded after login: %v", err)
	}

	t.Logf("INFO: Successfully logged in as %s", username)
}

// LogoutUser attempts to logout the current user
func LogoutUser(t *testing.T, page playwright.Page, afp *AFPlaywright, afi *h.AFInstance) {
	t.Helper()

	// Try multiple logout methods as UI might vary
	logoutMethods := []func() error{
		// Method 1: Look for user menu button and click logout
		func() error {
			userMenuButton := page.Locator("button").Filter(playwright.LocatorFilterOptions{
				HasText: "admin",
			})
			if err := userMenuButton.Click(); err != nil {
				return err
			}
			return page.Locator("text=Logout").Click()
		},
		// Method 2: Direct logout link
		func() error {
			return page.Locator("text=Logout").Click()
		},
		// Method 3: Force logout by navigating to login page
		func() error {
			_, err := page.Goto(afi.APIAddress(""), playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			})
			return err
		},
	}

	var lastErr error
	for i, method := range logoutMethods {
		if err := method(); err != nil {
			lastErr = err
			t.Logf("WARNING: Logout method %d failed: %v", i+1, err)
			continue
		}
		break
	}

	// Wait for redirect to login page
	if err := page.WaitForURL("**/login", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(2000),
	}); err != nil {
		// If we're already on login page, check for username field
		if err := page.Locator("#username").WaitFor(); err != nil {
			t.Fatalf("ERROR: Could not reach login page after logout: %v (last logout error: %v)", err, lastErr)
		}
	}

	t.Log("INFO: Successfully logged out")
}

// NavigateToPage navigates to a specific page in the dashboard
func NavigateToPage(t *testing.T, page playwright.Page, pageName string) {
	t.Helper()

	if err := page.Locator("text=" + pageName).Click(); err != nil {
		t.Fatalf("ERROR: Could not navigate to %s: %v", pageName, err)
	}

	// Wait a bit for navigation to complete
	time.Sleep(1 * time.Second)

	t.Logf("INFO: Successfully navigated to %s", pageName)
}

// CreateApplication creates an application with the given YAML configuration (legacy)
func CreateApplication(t *testing.T, page playwright.Page, afp *AFPlaywright, yamlConfig, labelUid string) {
	t.Helper()

	// Click Create Application button
	if err := page.Locator("text=Create Application").Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create Application button: %v", err)
	}

	// Wait for modal to appear
	if err := page.Locator("text=Create Application").Last().WaitFor(); err != nil {
		t.Fatalf("ERROR: Create Application modal did not appear: %v", err)
	}

	// Fill YAML configuration
	if err := page.Locator("textarea").Fill(yamlConfig); err != nil {
		t.Fatalf("ERROR: Could not fill YAML configuration: %v", err)
	}

	// Click Create button in modal
	if err := page.Locator("text=Create").Last().Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	if err := page.Locator("text=Create Application").Last().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(2000),
	}); err != nil {
		t.Errorf("ERROR: Modal did not close as expected: %v", err)
	}

	// Wait for application to appear in list
	if err := page.Locator("text=" + labelUid).First().WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(2000),
	}); err != nil {
		t.Fatalf("ERROR: Application %s did not appear in list: %v", labelUid, err)
	}

	t.Logf("INFO: Successfully created application %s", labelUid)
}

// CreateApplicationForm creates an application using the new form interface
func CreateApplicationForm(t *testing.T, page playwright.Page, afp *AFPlaywright, labelUid string, metadata map[string]string) {
	t.Helper()

	// Click Create Application button
	if err := page.Locator("text=Create Application").Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create Application button: %v", err)
	}

	// Wait for modal to appear
	if err := page.Locator("text=Create Application").Last().WaitFor(); err != nil {
		t.Fatalf("ERROR: Create Application modal did not appear: %v", err)
	}

	// Fill Label UID field
	if err := page.Locator("input[type=text]").Filter(playwright.LocatorFilterOptions{
		HasText: "Label Uid",
	}).Fill(labelUid); err != nil {
		// Try alternative selector
		if err := page.Locator("label:has-text('Label Uid')").Locator("..").Locator("input").Fill(labelUid); err != nil {
			t.Fatalf("ERROR: Could not fill Label UID field: %v", err)
		}
	}

	// Fill metadata as JSON
	if len(metadata) > 0 {
		metadataJSON := "{"
		first := true
		for k, v := range metadata {
			if !first {
				metadataJSON += ","
			}
			metadataJSON += fmt.Sprintf(`"%s":"%s"`, k, v)
			first = false
		}
		metadataJSON += "}"

		if err := page.Locator("label:has-text('Metadata')").Locator("..").Locator("textarea").Fill(metadataJSON); err != nil {
			t.Logf("WARNING: Could not fill metadata field: %v", err)
		}
	}

	// Click Create button in modal
	if err := page.Locator("button:has-text('Create')").Last().Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	if err := page.Locator("text=Create Application").Last().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Errorf("ERROR: Modal did not close as expected: %v", err)
	}

	// Wait for application to appear in list
	if err := page.Locator("text=" + labelUid).First().WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("ERROR: Application %s did not appear in list: %v", labelUid, err)
	}

	t.Logf("INFO: Successfully created application %s", labelUid)
}

// CreateLabel creates a label with the given name and UID
func CreateLabel(t *testing.T, page playwright.Page, afp *AFPlaywright, labelName string) {
	t.Helper()

	// Navigate to manage page
	NavigateToPage(t, page, "Labels")

	// Click Create Label button
	if err := page.Locator("text=Create Label").Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create Label button: %v", err)
	}

	// Wait for modal to appear
	if err := page.Locator("text=Create Label").Last().WaitFor(); err != nil {
		t.Fatalf("ERROR: Create Label modal did not appear: %v", err)
	}

	// Create basic label definition
	definitionYAML := `
name: test-label
version: 1
definitions:
  - driver: test
    resources:
      cpu: 1
      ram: 2
`
	// Click Load from YAML button in modal
	if err := page.Locator("button:has-text('Load from YAML')").First().Click(); err != nil {
		t.Fatalf("ERROR: Could not click Load from YAML button in modal: %v", err)
	}

	if err := page.Locator("label:has-text('YAML Configuration')").Locator("..").Locator("textarea").Fill(definitionYAML); err != nil {
		t.Fatalf("ERROR: Could not fill Definitions field: %v", err)
	}

	// Click Load from YAML button in modal to confirm loading
	if err := page.Locator("button:has-text('Load from YAML')").Last().Click(); err != nil {
		t.Fatalf("ERROR: Could not click Load from YAML button in modal to confirm load: %v", err)
	}

	// Fill Name field
	if err := page.Locator("label:has-text('Name')").Locator("..").Locator("input").Fill(labelName); err != nil {
		t.Fatalf("ERROR: Could not fill Name field: %v", err)
	}

	// Click Create button in modal
	if err := page.Locator("button:has-text('Create')").Last().Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	if err := page.Locator("text=Create Label").Last().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Errorf("ERROR: Modal did not close as expected: %v", err)
	}

	// Wait for label to appear in list
	if err := page.Locator("text=" + labelName).First().WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("ERROR: Label %s did not appear in list: %v", labelName, err)
	}

	t.Logf("INFO: Successfully created label %s", labelName)
}

// DeallocateApplication deallocates an application by its label UID
func DeallocateApplication(t *testing.T, page playwright.Page, labelUid string) {
	t.Helper()

	// Find the application row and click deallocate
	appRow := page.Locator("text=" + labelUid).First().Locator("..")
	deallocateBtn := appRow.Locator("text=Deallocate")

	if err := deallocateBtn.WaitFor(); err != nil {
		t.Fatalf("ERROR: Deallocate button not found for %s: %v", labelUid, err)
	}

	// Set up dialog handler before clicking
	page.OnDialog(func(dialog playwright.Dialog) {
		if err := dialog.Accept(); err != nil {
			t.Logf("WARNING: Could not accept dialog: %v", err)
		}
	})

	// Click deallocate button
	if err := deallocateBtn.Click(); err != nil {
		t.Fatalf("ERROR: Could not click Deallocate button for %s: %v", labelUid, err)
	}

	// Wait for status change
	time.Sleep(3 * time.Second)

	t.Logf("INFO: Successfully deallocated application %s", labelUid)
}

// CreateUser creates a new user with the given parameters
func CreateUser(t *testing.T, page playwright.Page, afp *AFPlaywright, username, password string, roles []string) {
	t.Helper()

	// Navigate to manage page
	NavigateToPage(t, page, "Management")

	// Click on Users tab
	if err := page.Locator("text=Users").Click(); err != nil {
		t.Fatalf("ERROR: Could not click Users tab: %v", err)
	}

	// Click Create User button
	if err := page.Locator("text=Create User").Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create User button: %v", err)
	}

	// Wait for create user modal
	if err := page.Locator("text=Create User").Last().WaitFor(); err != nil {
		t.Fatalf("ERROR: Create User modal did not appear: %v", err)
	}

	// Fill user creation form
	if err := page.Locator("input[type=text]").Fill(username); err != nil {
		t.Fatalf("ERROR: Could not fill username: %v", err)
	}

	if err := page.Locator("input[type=password]").Fill(password); err != nil {
		t.Fatalf("ERROR: Could not fill password: %v", err)
	}

	// Select roles
	for _, role := range roles {
		roleCheckbox := page.Locator("text=" + role).Locator("..").Locator("input[type=checkbox]")
		if err := roleCheckbox.Check(); err != nil {
			t.Logf("WARNING: Could not select role %s: %v", role, err)
		}
	}

	// Click Create button in modal
	if err := page.Locator("text=Create").Last().Click(); err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	if err := page.Locator("text=Create User").Last().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Logf("WARNING: Create User modal did not close as expected: %v", err)
	}

	// Wait for user to appear in list
	if err := page.Locator("text=" + username).First().WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(5000),
	}); err != nil {
		t.Fatalf("ERROR: Created user %s did not appear in list: %v", username, err)
	}

	t.Logf("INFO: Successfully created user %s", username)
}

// TakeScreenshot takes a screenshot with the given filename
func TakeScreenshot(t *testing.T, page playwright.Page, afp *AFPlaywright, filename string) {
	t.Helper()

	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(afp.CaptureDir("screenshots", filename)),
	}); err != nil {
		t.Logf("WARNING: Could not take screenshot %s: %v", filename, err)
	}
}

// WaitForElement waits for an element to be present on the page
func WaitForElement(t *testing.T, page playwright.Page, selector string, timeout time.Duration) {
	t.Helper()

	if err := page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(float64(timeout.Milliseconds())),
	}); err != nil {
		t.Fatalf("ERROR: Element %s not found within timeout: %v", selector, err)
	}
}

// CheckElementExists checks if an element exists without failing the test
func CheckElementExists(t *testing.T, page playwright.Page, selector string) bool {
	t.Helper()

	err := page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(5000),
	})
	return err == nil
}

// VerifyApplicationInList verifies that an application appears in the list with correct details
func VerifyApplicationInList(t *testing.T, page playwright.Page, labelUid, owner string) {
	t.Helper()

	// Wait for application to appear
	if err := page.Locator("text=" + labelUid).First().WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(2000),
	}); err != nil {
		t.Fatalf("ERROR: Application %s not found in list: %v", labelUid, err)
	}

	// Verify application details
	appRow := page.Locator("text=" + labelUid).First().Locator("..")
	if err := appRow.Locator("text=" + owner).WaitFor(); err != nil {
		t.Fatalf("ERROR: Application owner not displayed correctly: %v", err)
	}

	t.Logf("INFO: Successfully verified application %s in list", labelUid)
}
