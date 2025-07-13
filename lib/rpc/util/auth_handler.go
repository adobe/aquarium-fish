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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
	"github.com/golang-jwt/jwt/v4"
)

// Global JWT secret (generated once on startup)
var (
	jwtSecret     []byte
	jwtSecretOnce sync.Once
)

// JWT claims structure
type JWTClaims struct {
	UserName string   `json:"user_name"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// getJWTSecret returns the JWT secret, generating it if necessary
func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		// TODO: Store jwt secret somewhere or use priv/pub key
		jwtSecret = make([]byte, 32)
		if _, err := rand.Read(jwtSecret); err != nil {
			panic(fmt.Sprintf("failed to generate JWT secret: %v", err))
		}
	})
	return jwtSecret
}

// ParseJWTToken parses a JWT token and returns the claims
func ParseJWTToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	})

	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// GetJWTSecret returns the JWT secret for use by other packages
func GetJWTSecret() []byte {
	return getJWTSecret()
}

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
		logger := log.WithFunc("rpc", "auth").With("url_path", r.URL.Path)

		service, method := getServiceMethodFromPath(r.URL.Path)

		// Ignore the service/method when it's in auth authExclude list
		if auth.IsEcludedFromAuth(service, method) {
			logger.Debug("Skipping auth for excluded method")
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")

		var user *typesv2.User

		// Check for JWT Bearer token first
		if strings.HasPrefix(auth, "Bearer ") {
			tokenString := auth[7:]
			logger.Debug("JWT Bearer token found")

			// Parse JWT token
			claims, err := ParseJWTToken(tokenString)
			if err != nil {
				logger.Debug("Failed to parse JWT token", "err", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Get user from database
			user, err = h.db.UserGet(r.Context(), claims.UserName)
			if err != nil {
				logger.Debug("Failed to get user from JWT", "username", claims.UserName, "err", err)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			logger.Debug("JWT authentication successful", "user", user.Name)
		} else if strings.HasPrefix(auth, "Basic ") {
			// Fall back to Basic auth for compatibility
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
			logger.Debug("Basic auth request received", "user", username)

			user = h.db.UserAuth(r.Context(), username, password)
			if user == nil {
				logger.Debug("Authentication failed for user", "user", username)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			logger.Debug("Basic authentication successful", "user", user.Name)
		} else {
			logger.Debug("No valid auth header found")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Add user to context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}
