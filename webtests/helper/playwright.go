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
	"os"
	"path/filepath"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// AFPlaywright saves state of the running Aquarium Fish for particular test
type AFPlaywright struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	page    playwright.Page
	expect  playwright.PlaywrightAssertions

	captureDir string

	isChromium bool
	isFirefox  bool
	isWebKit   bool

	browserName string
	browserType playwright.BrowserType
}

// Default context options for most tests
var DEFAULT_CONTEXT_OPTIONS = playwright.BrowserNewContextOptions{
	AcceptDownloads: playwright.Bool(true),
	HasTouch:        playwright.Bool(true),
}

// NewPlaywright initializes Playwright context helper
func NewPlaywright(tb testing.TB, workspace string, options playwright.BrowserNewContextOptions) (*AFPlaywright, playwright.Page) {
	var err error
	afp := &AFPlaywright{
		captureDir:  filepath.Join(workspace, "playwright"),
		browserName: getBrowserName(),
	}

	afp.pw, err = playwright.Run()
	if err != nil {
		tb.Fatalf("ERROR: Could not start Playwright: %v", err)
	}

	if afp.browserName == "chromium" || afp.browserName == "" {
		afp.browserType = afp.pw.Chromium
	} else if afp.browserName == "firefox" {
		afp.browserType = afp.pw.Firefox
	} else if afp.browserName == "webkit" {
		afp.browserType = afp.pw.WebKit
	}

	afp.browser, err = afp.browserType.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(os.Getenv("HEADFUL") == ""),
	})
	if err != nil {
		tb.Fatalf("ERROR: Could not launch: %v", err)
	}

	afp.expect = playwright.NewPlaywrightAssertions(1000)

	afp.isChromium = afp.browserName == "chromium" || afp.browserName == ""
	afp.isFirefox = afp.browserName == "firefox"
	afp.isWebKit = afp.browserName == "webkit"

	tb.Cleanup(func() {
		if err = afp.browser.Close(); err != nil {
			tb.Fatalf("ERROR: Could not close browser: %v", err)
		}
		if err = afp.pw.Stop(); err != nil {
			tb.Fatalf("ERROR: Could not stop Playwright: %v", err)
		}
		afp.Cleanup(tb)
	})

	// Creating context
	afp.newBrowserContext(tb, options)

	return afp, afp.NewPage(tb)
}

func (afp *AFPlaywright) NewPage(tb testing.TB) playwright.Page {
	page, err := afp.context.NewPage()
	if err != nil {
		tb.Fatalf("ERROR: Could not create page: %v", err)
	}
	return page
}

// CaptureDir returns dir where to store all the test data
func (afp *AFPlaywright) CaptureDir(path ...string) string {
	paths := append([]string{afp.captureDir}, path...)
	out := filepath.Join(paths...)
	os.MkdirAll(filepath.Dir(out), 0755)
	return out
}

// Cleanup after the test execution
func (afp *AFPlaywright) Cleanup(tb testing.TB) {
	tb.Helper()
	tb.Log("INFO: Cleaning up playwright:", afp.browserName)

	if tb.Failed() {
		tb.Log("INFO: Keeping captures for checking:", afp.captureDir)
		return
	}
	os.RemoveAll(afp.captureDir)
}

func (afp *AFPlaywright) newBrowserContext(tb testing.TB, options playwright.BrowserNewContextOptions) {
	tb.Helper()

	// Predefined vars
	options.IgnoreHttpsErrors = playwright.Bool(true)
	options.ColorScheme = playwright.ColorSchemeDark
	options.RecordVideo = &playwright.RecordVideo{
		Dir: afp.CaptureDir("video"),
	}

	var err error
	if afp.context, err = afp.browser.NewContext(options); err != nil {
		tb.Fatalf("ERROR: Could not create new context: %v", err)
	}
	// We use 1s by default for timeout
	afp.context.SetDefaultTimeout(1000)
	afp.context.SetDefaultNavigationTimeout(1000)

	tb.Cleanup(func() {
		if err := afp.context.Close(); err != nil {
			tb.Errorf("ERROR: Could not close context: %v", err)
		}
	})
}

func getBrowserName() string {
	browserName, hasEnv := os.LookupEnv("BROWSER")
	if hasEnv {
		return browserName
	}
	return "chromium"
}
