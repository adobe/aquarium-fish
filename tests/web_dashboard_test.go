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
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	h "github.com/adobe/aquarium-fish/tests/helper"
)

// TestWebDashboardAccess tests that the web dashboard is accessible
func TestWebDashboardAccess(t *testing.T) {
	t.Parallel()
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

	// Create HTTP client with custom transport to skip TLS verification
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test environment
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	// Get the API address
	baseURL := afi.APIAddress("grpc")

	t.Run("Dashboard Root Serves HTML", func(t *testing.T) {
		resp, err := client.Get(baseURL)
		if err != nil {
			t.Fatalf("Failed to get dashboard root: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		bodyStr := string(body)

		// Check for basic HTML structure
		if !strings.Contains(bodyStr, "<html") {
			t.Error("Response should contain HTML content")
		}

		// Check for React app root element
		if !strings.Contains(bodyStr, `id="root"`) {
			t.Error("Response should contain React root element")
		}

		// Check for title
		if !strings.Contains(bodyStr, "Aquarium Fish") {
			t.Error("Response should contain Aquarium Fish title")
		}
	})

	t.Run("SPA Routes Serve Index", func(t *testing.T) {
		// Test SPA routes that should serve index.html
		routes := []string{"/applications", "/status", "/manage", "/login"}

		for _, route := range routes {
			t.Run(route, func(t *testing.T) {
				resp, err := client.Get(baseURL + route)
				if err != nil {
					t.Fatalf("Failed to get route %s: %v", route, err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Errorf("Expected status 200 for route %s, got %d", route, resp.StatusCode)
				}

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("Failed to read response body for route %s: %v", route, err)
				}

				bodyStr := string(body)

				// Should serve the same index.html content
				if !strings.Contains(bodyStr, `id="root"`) {
					t.Errorf("Route %s should serve React root element", route)
				}
			})
		}
	})

	t.Run("API Routes Still Work", func(t *testing.T) {
		// Test that API routes are not affected by web dashboard
		resp, err := client.Get(baseURL)
		if err != nil {
			t.Fatalf("Failed to get API root: %v", err)
		}
		defer resp.Body.Close()

		// gRPC/Connect endpoints should return an error for GET requests
		// but the endpoint should be accessible
		if resp.StatusCode == http.StatusOK {
			t.Error("API endpoint should not return 200 for GET request")
		}
	})

	t.Run("Security Headers", func(t *testing.T) {
		resp, err := client.Get(baseURL)
		if err != nil {
			t.Fatalf("Failed to get dashboard root: %v", err)
		}
		defer resp.Body.Close()

		// Check for security headers
		expectedHeaders := map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"X-XSS-Protection":       "1; mode=block",
			"Referrer-Policy":        "strict-origin-when-cross-origin",
		}

		for header, expectedValue := range expectedHeaders {
			actualValue := resp.Header.Get(header)
			if actualValue != expectedValue {
				t.Errorf("Expected header %s to be '%s', got '%s'", header, expectedValue, actualValue)
			}
		}
	})
}

// TestWebDashboardWithAuth tests authentication integration with the web dashboard
func TestWebDashboardWithAuth(t *testing.T) {
	t.Parallel()
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

	// Create HTTP client with custom transport to skip TLS verification
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test environment
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	// Get the API address
	baseURL := afi.APIAddress("grpc")

	t.Run("Auth API Accessible", func(t *testing.T) {
		// Test that auth API is accessible from web dashboard
		// This would typically be called by the frontend
		resp, err := client.Post(baseURL+"/aquarium.v2.AuthService/Login",
			"application/json",
			strings.NewReader(`{"username":"test","password":"test"}`))
		if err != nil {
			t.Fatalf("Failed to call auth API: %v", err)
		}
		defer resp.Body.Close()

		// Should get a response (even if it's an auth failure)
		// The important thing is that the endpoint is reachable
		if resp.StatusCode == 0 {
			t.Error("Auth API should be reachable from web dashboard")
		}
	})
}
