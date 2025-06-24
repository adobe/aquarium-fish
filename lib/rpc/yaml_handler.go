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
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/adobe/aquarium-fish/lib/log"
)

// isYAMLContentType checks if the content type indicates YAML
func isYAMLContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "application/yaml") ||
		strings.HasPrefix(contentType, "application/x-yaml") ||
		strings.HasPrefix(contentType, "text/yaml") ||
		strings.HasPrefix(contentType, "text/x-yaml")
}

// YAMLToJSONHandler is a HTTP middleware that converts YAML to JSON
func YAMLToJSONHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request has YAML content type
		contentType := r.Header.Get("Content-Type")
		if !isYAMLContentType(contentType) {
			next.ServeHTTP(w, r)
			return
		}

		// Read the body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		r.Body.Close()

		// Convert YAML to JSON
		var yamlData any
		if err := yaml.Unmarshal(body, &yamlData); err != nil {
			log.Debugf("YAML: Failed to unmarshal YAML: %v", err)
			http.Error(w, "Invalid YAML format", http.StatusBadRequest)
			return
		}

		// Convert to JSON
		jsonData, err := json.Marshal(yamlData)
		if err != nil {
			log.Debugf("YAML: Failed to marshal to JSON: %v", err)
			http.Error(w, "Failed to convert YAML to JSON", http.StatusInternalServerError)
			return
		}

		log.Debugf("YAML: Successfully converted YAML to JSON for %s", r.URL.Path)

		// Update request
		r.Body = io.NopCloser(bytes.NewReader(jsonData))
		r.ContentLength = int64(len(jsonData))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Original-Content-Type", contentType)

		next.ServeHTTP(w, r)
	})
}
