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
	"testing"
	"time"

	h "github.com/adobe/aquarium-fish/tests/helper"
	pw "github.com/playwright-community/playwright-go"
)

// LoginUser is a helper function to login with given credentials
func LoginUser(t *testing.T, page pw.Page, afp *AFPlaywright, afi *h.AFInstance, username, password string) {
	t.Helper()

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
	if err := page.WaitForURL("**/applications", pw.PageWaitForURLOptions{
		Timeout: pw.Float(2000),
	}); err != nil {
		t.Fatalf("ERROR: Login failed for user %s: %v", username, err)
	}

	// Verify we're on the applications page
	if err := page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Applications"}).First().WaitFor(); err != nil {
		t.Fatalf("ERROR: Applications page not loaded after login: %v", err)
	}

	t.Logf("INFO: Successfully logged in as %s", username)
}

// LogoutUser attempts to logout the current user
func LogoutUser(t *testing.T, page pw.Page, afp *AFPlaywright, afi *h.AFInstance) {
	t.Helper()

	// Try multiple logout methods as UI might vary
	logoutMethods := []func() error{
		// Method 1: Look for user menu button and click logout
		func() error {
			userMenuButton := page.Locator("button").Filter(pw.LocatorFilterOptions{
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
			_, err := page.Goto(afi.APIAddress(""), pw.PageGotoOptions{
				WaitUntil: pw.WaitUntilStateDomcontentloaded,
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
	if err := page.WaitForURL("**/login", pw.PageWaitForURLOptions{
		Timeout: pw.Float(2000),
	}); err != nil {
		// If we're already on login page, check for username field
		if err := page.Locator("#username").WaitFor(); err != nil {
			t.Fatalf("ERROR: Could not reach login page after logout: %v (last logout error: %v)", err, lastErr)
		}
	}

	t.Log("INFO: Successfully logged out")
}

// NavigateToPage navigates to a specific page in the dashboard
func NavigateToPage(t *testing.T, page pw.Page, pageName string) {
	t.Helper()

	err := page.Locator(`[href="/` + pageName + `"]`).
		First().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not navigate to %s: %v", pageName, err)
	}

	// Wait a bit for navigation to complete
	time.Sleep(1 * time.Second)

	t.Logf("INFO: Successfully navigated to %s", pageName)
}

// CreateLabel creates a label with the given name and UID
// Returns: LabelUID
func CreateLabel(t *testing.T, page pw.Page, afp *AFPlaywright, labelName string) string {
	t.Helper()

	// Navigate to page
	NavigateToPage(t, page, "labels")

	// Click Create Label button
	err := page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Create Label"}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Create Label button: %v", err)
	}

	// Wait for modal to appear
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Create Label"}).
		Last().
		WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Create Label modal did not appear: %v", err)
	}

	// Click Load from YAML button in modal
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Load from YAML"}).
		First().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Load from YAML button in modal: %v", err)
	}

	// Create basic label definition
	definitionYAML := `
name: ""
version: 1
definitions:
  - driver: test
    resources:
      cpu: 1
      ram: 2
`
	err = page.Locator("label:has-text('YAML Configuration')").
		Locator("..").
		Locator("textarea").
		Fill(definitionYAML)
	if err != nil {
		t.Fatalf("ERROR: Could not fill Definitions field: %v", err)
	}

	// Click Load from YAML button in modal to confirm loading
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Load from YAML"}).
		First().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Load from YAML button in modal to confirm load: %v", err)
	}

	// Fill Name field
	err = page.Locator("label:has-text('Name *')").
		Locator("..").
		Locator("..").
		Locator("input").
		Fill(labelName)
	if err != nil {
		t.Fatalf("ERROR: Could not fill Name field: %v", err)
	}

	// Click Create button in modal
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Create"}).
		Last().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Create Label"}).
		Last().
		WaitFor(pw.LocatorWaitForOptions{
			State: pw.WaitForSelectorStateHidden,
		})
	if err != nil {
		t.Errorf("ERROR: Modal did not close as expected: %v", err)
	}

	// Wait for label to appear in list and getting into details to find the labelUID
	err = page.GetByRole(*pw.AriaRoleListitem).
		Filter(pw.LocatorFilterOptions{HasText: labelName + ":1"}).
		First().
		GetByRole(*pw.AriaRoleButton, pw.LocatorGetByRoleOptions{Name: "View Details"}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Label %s did not appear in list: %v", labelName, err)
	}

	// Wait for modal to appear
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Label Details: " + labelName + ":1"}).
		Last().
		WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Label Details modal did not appear: %v", err)
	}

	// Getting the UID field in the Label Details
	var labelUID string
	labelUID, err = page.Locator("p:below(p:text('UID'))").
		First().
		InnerText()
	if err != nil {
		t.Errorf("ERROR: No Label UID found in Label Details modal: %v", err)
	}

	// Close label details popup
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "×"}).
		First().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Close modal button ×: %v", err)
	}

	t.Logf("INFO: Successfully created label %s: %s", labelName, labelUID)

	return labelUID
}

// CreateApplication creates a new application
func CreateApplication(t *testing.T, page pw.Page, afp *AFPlaywright, labelUID string, metadata map[string]string) {
	t.Helper()

	// Navigate to page
	NavigateToPage(t, page, "applications")

	// Click Create Application button
	err := page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Create Application"}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Create Application button: %v", err)
	}

	// Wait for modal to appear
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Create Application"}).
		Last().
		WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Create Application modal did not appear: %v", err)
	}

	// Select Label from available options
	_, err = page.GetByRole(*pw.AriaRoleCombobox).
		SelectOption(pw.SelectOptionValues{Values: pw.StringSlice(labelUID)})
	if err != nil {
		t.Fatalf("ERROR: Could not select Label UID option %q: %v", labelUID, err)
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

		err = page.Locator("label:has-text('Metadata')").
			Locator("..").
			Locator("textarea").
			Fill(metadataJSON)
		if err != nil {
			t.Logf("WARNING: Could not fill metadata field: %v", err)
		}
	}

	// Click Create button in modal
	err = page.Locator("button:has-text('Create')").
		Last().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Create Application"}).
		Last().
		WaitFor(pw.LocatorWaitForOptions{
			State:   pw.WaitForSelectorStateHidden,
			Timeout: pw.Float(2000),
		})
	if err != nil {
		t.Errorf("ERROR: Modal did not close as expected: %v", err)
	}

	t.Logf("INFO: Successfully created application")
}

// DeallocateApplication deallocates an application by its label UID
func DeallocateApplication(t *testing.T, page pw.Page, labelName string) {
	t.Helper()

	appElement := page.GetByRole(*pw.AriaRoleListitem).
		Filter(pw.LocatorFilterOptions{HasText: labelName + ":1"}).
		First()

	// Find the application row and click deallocate
	if err := appElement.WaitFor(); err != nil {
		t.Fatalf("ERROR: Application with label %q did not appear in list: %v", labelName, err)
	}

	deallocateBtn := appElement.GetByRole(*pw.AriaRoleButton, pw.LocatorGetByRoleOptions{Name: "Deallocate"})
	if err := deallocateBtn.WaitFor(); err != nil {
		t.Fatalf("ERROR: Deallocate button not found for Label %q: %v", labelName, err)
	}

	// Set up dialog handler before clicking
	page.OnDialog(func(dialog pw.Dialog) {
		if err := dialog.Accept(); err != nil {
			t.Logf("WARNING: Could not accept dialog: %v", err)
		}
	})

	// Click deallocate button
	if err := deallocateBtn.Click(); err != nil {
		t.Fatalf("ERROR: Could not click Deallocate button for Label %q: %v", labelName, err)
	}

	t.Logf("INFO: Successfully deallocated application for Label %q", labelName)
}

// CreateUser creates a new user with the given parameters
func CreateUser(t *testing.T, page pw.Page, afp *AFPlaywright, username, password string, roles []string) {
	t.Helper()

	// Navigate to manage page
	NavigateToPage(t, page, "management")

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
	if err := page.Locator("text=Create User").Last().WaitFor(pw.LocatorWaitForOptions{
		State:   pw.WaitForSelectorStateHidden,
		Timeout: pw.Float(5000),
	}); err != nil {
		t.Logf("WARNING: Create User modal did not close as expected: %v", err)
	}

	// Wait for user to appear in list
	if err := page.Locator("text=" + username).First().WaitFor(pw.LocatorWaitForOptions{
		Timeout: pw.Float(5000),
	}); err != nil {
		t.Fatalf("ERROR: Created user %s did not appear in list: %v", username, err)
	}

	t.Logf("INFO: Successfully created user %s", username)
}

// TakeScreenshot takes a screenshot with the given filename
func TakeScreenshot(t *testing.T, page pw.Page, afp *AFPlaywright, filename string) {
	t.Helper()

	if _, err := page.Screenshot(pw.PageScreenshotOptions{
		Path: pw.String(afp.CaptureDir("screenshots", filename)),
	}); err != nil {
		t.Logf("WARNING: Could not take screenshot %s: %v", filename, err)
	}
}

// WaitForElement waits for an element to be present on the page
func WaitForElement(t *testing.T, page pw.Page, selector string, timeout time.Duration) {
	t.Helper()

	if err := page.Locator(selector).WaitFor(pw.LocatorWaitForOptions{
		Timeout: pw.Float(float64(timeout.Milliseconds())),
	}); err != nil {
		t.Fatalf("ERROR: Element %s not found within timeout: %v", selector, err)
	}
}

// CheckElementExists checks if an element exists without failing the test
func CheckElementExists(t *testing.T, page pw.Page, selector string) bool {
	t.Helper()

	err := page.Locator(selector).WaitFor(pw.LocatorWaitForOptions{
		Timeout: pw.Float(5000),
	})
	return err == nil
}

// VerifyApplicationInList verifies that an application appears in the list with correct details
func VerifyApplicationInList(t *testing.T, page pw.Page, labelName, owner, status string) {
	t.Helper()

	appElement := page.GetByRole(*pw.AriaRoleListitem).
		Filter(pw.LocatorFilterOptions{HasText: labelName + ":1"}).
		First()

	// Wait for application to appear in list
	err := appElement.WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Application with label %q did not appear in list: %v", labelName, err)
	}

	// Verify application owner
	if err := appElement.GetByText("Owner: " + owner).WaitFor(); err != nil {
		t.Fatalf("ERROR: Application owner not displayed correctly: %v", err)
	}

	// Verify application status
	if err := appElement.GetByText(status).WaitFor(); err != nil {
		t.Fatalf("ERROR: Application status not displayed correctly: %v", err)
	}

	t.Logf("INFO: Successfully verified application %s in list", labelName)
}
