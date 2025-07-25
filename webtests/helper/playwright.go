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

// Package helper allows to run playwright WebUI tests with Aquarium Fish
package helper

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// AFPlaywright saves state of the running Aquarium Fish for particular test
type AFPlaywright struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	context playwright.BrowserContext
	page    playwright.Page

	captureDir string

	isChromium bool
	isFirefox  bool
	isWebKit   bool

	browserName string
	browserType playwright.BrowserType

	// Automatic tests screenshoting
	stepMu sync.Mutex
	step   int
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

	switch afp.browserName {
	case "firefox":
		afp.browserType = afp.pw.Firefox
		afp.isFirefox = true
	case "webkit":
		afp.browserType = afp.pw.WebKit
		afp.isWebKit = true
	default:
		afp.browserType = afp.pw.Chromium
		afp.isChromium = true
	}

	// By default tests are running headless, but there could be a need to run them with UI
	afp.browser, err = afp.browserType.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(os.Getenv("HEADFUL") == ""),
	})
	if err != nil {
		tb.Fatalf("ERROR: Could not launch: %v", err)
	}

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

	// Probably with multiple pages in the future we will need to keep a slice of them to screenshot
	afp.page = afp.newPage(tb)

	return afp, afp.page
}

func (afp *AFPlaywright) Run(t *testing.T, name string, fn func(t *testing.T)) {
	t.Helper()

	t.Run(name, func(t *testing.T) {
		// Take screenshot at beginning of subtest
		afp.Screenshot(t, "start")

		// Defer screenshot at end of subtest
		defer afp.Screenshot(t, "end")

		// Run the actual test function
		fn(t)
	})
}

// Screenshot takes a screenshot with automatic naming
func (afp *AFPlaywright) Screenshot(t *testing.T, phase string) {
	afp.stepMu.Lock()
	defer afp.stepMu.Unlock()

	// Increment step counter for this subtest
	afp.step++

	subtestName := path.Base(t.Name())

	// Create filename: step_subtestName_phase.png
	filename := fmt.Sprintf("%02d-%s-%s.png", afp.step, subtestName, phase)

	if _, err := afp.page.Screenshot(playwright.PageScreenshotOptions{
		Path: playwright.String(afp.CaptureDir("screenshots", filename)),
	}); err != nil {
		t.Logf("WARNING: Could not take screenshot %s: %v", filename, err)
	}
}

func (afp *AFPlaywright) newPage(tb testing.TB) playwright.Page {
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
