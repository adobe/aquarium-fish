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

package util

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/adobe/aquarium-fish/lib/crypt"
	"github.com/adobe/aquarium-fish/lib/database"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

func TestAuthHandler(t *testing.T) {
	// Setup in-memory database with temp directory
	tempDir := t.TempDir()
	db, err := database.New(tempDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Create test user
	testUser := &typesv2.User{
		Name: "testuser",
	}
	testPassword := "testpass"
	hash := crypt.NewHash(testPassword, nil)
	if err := testUser.SetHash(hash); err != nil {
		t.Fatalf("Failed to set user hash: %v", err)
	}
	if err := db.UserCreate(context.Background(), testUser); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create auth handler
	authHandler := NewAuthHandler(db)

	// Test handler that should only be reached after successful authentication
	yamlProcessingCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		yamlProcessingCalled = true
		// Verify user is in context
		user := GetUserFromContext(r.Context())
		if user == nil {
			t.Error("Expected user in context after authentication")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if user.Name != "testuser" {
			t.Errorf("Expected user 'testuser', got '%s'", user.Name)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Success"))
	})

	// Wrap with auth handler
	handler := authHandler.Handler(testHandler)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
		shouldCallNext bool
	}{
		{
			name:           "Valid authentication",
			authHeader:     "Basic " + base64.StdEncoding.EncodeToString([]byte("testuser:testpass")),
			expectedStatus: http.StatusOK,
			shouldCallNext: true,
		},
		{
			name:           "No auth header",
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
			shouldCallNext: false,
		},
		{
			name:           "Invalid auth format",
			authHeader:     "Bearer token123",
			expectedStatus: http.StatusUnauthorized,
			shouldCallNext: false,
		},
		{
			name:           "Invalid credentials",
			authHeader:     "Basic " + base64.StdEncoding.EncodeToString([]byte("testuser:wrongpass")),
			expectedStatus: http.StatusUnauthorized,
			shouldCallNext: false,
		},
		{
			name:           "Malformed basic auth",
			authHeader:     "Basic " + base64.StdEncoding.EncodeToString([]byte("invalidformat")),
			expectedStatus: http.StatusUnauthorized,
			shouldCallNext: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			yamlProcessingCalled = false

			req := httptest.NewRequest("POST", "/aquarium.v2.UserService/GetMe",
				strings.NewReader(`{"test": "data"}`))
			if test.authHeader != "" {
				req.Header.Set("Authorization", test.authHeader)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != test.expectedStatus {
				t.Errorf("Expected status %d, got %d", test.expectedStatus, w.Code)
			}

			if yamlProcessingCalled != test.shouldCallNext {
				t.Errorf("Expected next handler called: %v, got: %v", test.shouldCallNext, yamlProcessingCalled)
			}
		})
	}
}

// TestAuthBeforeYAML verifies that authentication happens before YAML processing
func TestAuthBeforeYAML(t *testing.T) {
	// Setup in-memory database with temp directory
	tempDir := t.TempDir()
	db, err := database.New(tempDir)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// Create auth handler
	authHandler := NewAuthHandler(db)

	yamlProcessingCalled := false
	yamlHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		yamlProcessingCalled = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("YAML processed"))
	})

	// Build middleware chain: Auth -> YAML
	handler := authHandler.Handler(yamlHandler)

	// Test with malicious YAML payload and no auth
	maliciousYaml := `
deeply:
  nested:
    yaml:
      with:
        lots:
          of:
            nesting:
              to:
                consume:
                  resources: true
`

	req := httptest.NewRequest("POST", "/aquarium.v2.UserService/Create",
		strings.NewReader(maliciousYaml))
	req.Header.Set("Content-Type", "application/yaml")
	// No Authorization header

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should be rejected at auth level before YAML processing
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	if yamlProcessingCalled {
		t.Error("YAML processing should not have been called for unauthenticated request")
	}
}
