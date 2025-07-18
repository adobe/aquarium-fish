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
	"strings"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
)

// AuthHandler is a HTTP middleware that handles authentication
type AuthHandler struct {
	db *database.Database
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(db *database.Database) *AuthHandler {
	return &AuthHandler{db: db}
}

// Handler implements HTTP middleware for authentication
func (h *AuthHandler) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithFunc("rpc", "auth")
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			logger.Debug("No Basic auth header found")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		payload, err := base64.StdEncoding.DecodeString(auth[6:])
		if err != nil {
			logger.Debug("Failed to decode auth header", "err", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		parts := strings.SplitN(string(payload), ":", 2)
		if len(parts) != 2 {
			logger.Debug("Invalid auth format")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		username, password := parts[0], parts[1]
		logger.Debug("New HTTP request received", "user", username, "url_path", r.URL.Path)

		user := h.db.UserAuth(r.Context(), username, password)
		if user == nil {
			logger.Debug("Authentication failed for user", "user", username)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add user to context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}
