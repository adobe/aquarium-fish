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

	pw "github.com/playwright-community/playwright-go"
)

// LoginUser is a helper function to login with given credentials
func LoginUser(t *testing.T, page pw.Page, username, password string) {
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
func LogoutUser(t *testing.T, page pw.Page) {
	t.Helper()

	// Look for user menu button and click logout
	err := page.GetByRole(*pw.AriaRoleButton).
		Filter(pw.LocatorFilterOptions{
			HasText: "Open user menu",
		}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Unable to find user menu button: %v", err)
	}

	// Click sign out button from menu
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Sign Out"}).Click()
	if err != nil {
		t.Fatalf("ERROR: Unable to click user menu logout button: %v", err)
	}

	// Wait for redirect to login page
	err = page.WaitForURL("**/login")
	if err != nil {
		t.Fatalf("ERROR: Unable to reach login page location: %v", err)
	}

	// Check for username field
	err = page.Locator("#username").WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Could not find username field on login page: %v", err)
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
func CreateLabel(t *testing.T, page pw.Page, labelName string) string {
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

	// Ensure no notifications is here blocking the view
	CloseAllNotifications(t, page)

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
	labelItem := page.GetByRole(*pw.AriaRoleListitem).
		Filter(pw.LocatorFilterOptions{HasText: labelName + ":1"}).
		First()

	err = labelItem.WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Label %s did not appear in list: %v", labelName, err)
	}

	err = labelItem.GetByRole(*pw.AriaRoleButton, pw.LocatorGetByRoleOptions{Name: "View Details"}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Label %s View Details button: %v", labelName, err)
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
func CreateApplication(t *testing.T, page pw.Page, labelUID string, metadata map[string]string) {
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
func CreateUser(t *testing.T, page pw.Page, username, password string, roles []string) {
	t.Helper()

	// Navigate to manage page
	NavigateToPage(t, page, "manage")

	// Click on Users tab
	err := page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Users"}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Users tab: %v", err)
	}

	// Click Create User button
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Create User"}).
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Create User button: %v", err)
	}

	// Wait for create user modal
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Create User"}).
		WaitFor()
	if err != nil {
		t.Fatalf("ERROR: Create User modal did not appear: %v", err)
	}

	// Fill user creation form
	err = page.Locator("label:has-text('Name')").
		Locator("..").
		Locator("..").
		Locator("input").
		Fill(username)
	if err != nil {
		t.Fatalf("ERROR: Could not fill Name field: %v", err)
	}

	err = page.Locator("label:has-text('Password')").
		Locator("..").
		Locator("..").
		Locator("input").
		Fill(password)
	if err != nil {
		t.Fatalf("ERROR: Could not fill Password field: %v", err)
	}

	// Put roles in
	rolesElement := page.Locator("label:has-text('Roles')").
		Locator("..").
		Locator("..")
	for _, role := range roles {
		err = rolesElement.GetByRole(*pw.AriaRoleButton, pw.LocatorGetByRoleOptions{Name: "+ Add Roles"}).
			Click()
		if err != nil {
			t.Fatalf("ERROR: Could not add new Role field: %v", err)
		}
		err = rolesElement.GetByRole(*pw.AriaRoleTextbox).
			Last().
			Fill(role)
		if err != nil {
			t.Fatalf("ERROR: Could not fill the Role field: %v", err)
		}
	}

	// Ensure no notifications is here blocking the view
	CloseAllNotifications(t, page)

	// Click Create button in modal
	err = page.GetByRole(*pw.AriaRoleButton, pw.PageGetByRoleOptions{Name: "Create"}).
		Last().
		Click()
	if err != nil {
		t.Fatalf("ERROR: Could not click Create button in modal: %v", err)
	}

	// Wait for modal to close
	err = page.GetByRole(*pw.AriaRoleHeading, pw.PageGetByRoleOptions{Name: "Create User"}).
		Last().
		WaitFor(pw.LocatorWaitForOptions{
			State:   pw.WaitForSelectorStateHidden,
			Timeout: pw.Float(2000),
		})
	if err != nil {
		t.Logf("WARNING: Create User modal did not close as expected: %v", err)
	}

	// Wait for user to appear in list
	userItem := page.GetByRole(*pw.AriaRoleListitem).
		Filter(pw.LocatorFilterOptions{HasText: username}).
		First()

	err = userItem.WaitFor()
	if err != nil {
		t.Fatalf("ERROR: User %s did not appear in list: %v", username, err)
	}
	if err := page.Locator("text=" + username).First().WaitFor(pw.LocatorWaitForOptions{
		Timeout: pw.Float(2000),
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

// CloseAllNotifications checks here is no error notifications and closes to clear the view
func CloseAllNotifications(t *testing.T, page pw.Page) {
	t.Helper()
	notificationsElement := page.Locator("#notifications")

	// Check there is no error notifications
	foundErrors, _ := notificationsElement.Locator(".notification-error").All()
	for _, notification := range foundErrors {
		content, _ := notification.TextContent()
		t.Errorf("ERROR: Found error user notification: %q", content)
	}

	// Close all notifications
	err := notificationsElement.
		GetByRole(*pw.AriaRoleButton).
		Filter(pw.LocatorFilterOptions{HasText: "Clear All"}).
		Click()
	if err != nil {
		t.Logf("INFO: No notifications to close")
	}
}
