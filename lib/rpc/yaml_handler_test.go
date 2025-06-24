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

package rpc

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsYAMLContentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"application/yaml", true},
		{"application/x-yaml", true},
		{"text/yaml", true},
		{"text/x-yaml", true},
		{"APPLICATION/YAML", true},
		{"application/yaml; charset=utf-8", true},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}

	for _, test := range tests {
		result := isYAMLContentType(test.contentType)
		if result != test.expected {
			t.Errorf("isYAMLContentType(%q) = %v, want %v", test.contentType, result, test.expected)
		}
	}
}

func TestYAMLToJSONHandler(t *testing.T) {
	// Create a simple handler that echoes back the request
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		w.Header().Set("X-Original-Content-Type", r.Header.Get("X-Original-Content-Type"))
		w.Write(body)
	})

	// Wrap with YAML handler
	handler := YAMLToJSONHandler(nextHandler)

	tests := []struct {
		name           string
		contentType    string
		body           string
		expectedStatus int
		expectedBody   string
		expectedCT     string
		expectedOrigCT string
	}{
		{
			name:           "YAML to JSON conversion",
			contentType:    "application/yaml",
			body:           "name: test\nversion: 1\ndata:\n  key: value",
			expectedStatus: http.StatusOK,
			expectedBody:   `{"data":{"key":"value"},"name":"test","version":1}`,
			expectedCT:     "application/json",
			expectedOrigCT: "application/yaml",
		},
		{
			name:           "JSON passthrough",
			contentType:    "application/json",
			body:           `{"name":"test","version":1}`,
			expectedStatus: http.StatusOK,
			expectedBody:   `{"name":"test","version":1}`,
			expectedCT:     "application/json",
			expectedOrigCT: "",
		},
		{
			name:           "Invalid YAML",
			contentType:    "application/yaml",
			body:           "invalid: yaml: content\n  bad indentation",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", bytes.NewBufferString(test.body))
			req.Header.Set("Content-Type", test.contentType)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != test.expectedStatus {
				t.Errorf("Expected status %d, got %d", test.expectedStatus, w.Code)
			}

			if test.expectedStatus == http.StatusOK {
				// For successful cases, check the response
				if test.expectedBody != "" {
					body := w.Body.String()
					// Parse both as JSON to compare structure (order might differ)
					var expectedJSON, actualJSON any
					if err := json.Unmarshal([]byte(test.expectedBody), &expectedJSON); err != nil {
						t.Fatalf("Failed to parse expected JSON: %v", err)
					}
					if err := json.Unmarshal([]byte(body), &actualJSON); err != nil {
						t.Fatalf("Failed to parse actual JSON: %v", err)
					}

					expectedBytes, _ := json.Marshal(expectedJSON)
					actualBytes, _ := json.Marshal(actualJSON)
					if string(expectedBytes) != string(actualBytes) {
						t.Errorf("Expected body %s, got %s", string(expectedBytes), string(actualBytes))
					}
				}

				if test.expectedCT != "" {
					ct := w.Header().Get("Content-Type")
					if ct != test.expectedCT {
						t.Errorf("Expected Content-Type %s, got %s", test.expectedCT, ct)
					}
				}

				if test.expectedOrigCT != "" {
					origCT := w.Header().Get("X-Original-Content-Type")
					if origCT != test.expectedOrigCT {
						t.Errorf("Expected X-Original-Content-Type %s, got %s", test.expectedOrigCT, origCT)
					}
				}
			}
		})
	}
}
