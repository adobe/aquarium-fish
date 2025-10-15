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

// authResponseWriter is a helper to capture response data from middleware
type authResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (w *authResponseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.ResponseWriter.WriteHeader(code)
		w.written = true
	}
}

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

// GetJWTSecret returns the JWT secret, generating it if necessary
func GetJWTSecret() []byte {
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
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(_ /*token*/ *jwt.Token) (any, error) {
		return GetJWTSecret(), nil
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

// AuthHandler is a HTTP middleware that handles authentication
type AuthHandler struct {
	db            *database.Database
	ipRateLimiter *IPRateLimitHandler
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(db *database.Database, ipRateLimiter *IPRateLimitHandler) *AuthHandler {
	return &AuthHandler{
		db:            db,
		ipRateLimiter: ipRateLimiter,
	}
}

// Handler implements HTTP middleware for authentication
func (h *AuthHandler) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithFunc("rpc", "auth").With("url_path", r.URL.Path)

		service, method := getServiceMethodFromPath(r.URL.Path)

		// Ignore the service/method when it's in auth authExclude list
		if auth.IsEcludedFromAuth(service, method) {
			logger.Debug("Skipping Auth for excluded method")
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")

		var user *typesv2.User
		var authFailed bool

		// Check for JWT Bearer token first
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString := authHeader[7:]
			logger.Debug("JWT Bearer token found")

			// Parse JWT token
			claims, err := ParseJWTToken(tokenString)
			if err != nil {
				logger.Debug("Failed to parse JWT token", "err", err)
				authFailed = true
			} else {
				// Get user from database
				user, err = h.db.UserGet(r.Context(), claims.UserName)
				if err != nil {
					logger.Debug("Failed to get user from JWT", "username", claims.UserName, "err", err)
					authFailed = true
				} else {
					logger.Debug("JWT authentication successful", "user", user.Name)
				}
			}
		} else if strings.HasPrefix(authHeader, "Basic ") {
			// Fall back to Basic auth for compatibility
			payload, err := base64.StdEncoding.DecodeString(authHeader[6:])
			if err != nil {
				logger.Debug("Failed to decode auth header", "err", err)
				authFailed = true
			} else {
				parts := strings.SplitN(string(payload), ":", 2)
				if len(parts) != 2 {
					logger.Debug("Invalid auth format")
					authFailed = true
				} else {
					username, password := parts[0], parts[1]
					logger.Debug("Basic auth request received", "user", username)

					user = h.db.UserAuth(r.Context(), username, password)
					if user == nil {
						logger.Debug("Authentication failed for user", "user", username)
						authFailed = true
					} else {
						logger.Debug("Basic authentication successful", "user", user.Name)
					}
				}
			}
		} else {
			logger.Debug("No valid auth header found")
			authFailed = true
		}

		// If authentication failed, apply IP rate limiting for unauthenticated requests
		if authFailed {
			if h.ipRateLimiter != nil {
				logger.Debug("Applying IP rate limiting for unauthenticated request")

				// Create a response writer to capture IP rate limit response
				authRespWriter := &authResponseWriter{ResponseWriter: w, statusCode: 200}

				// Create a dummy handler that just sets success
				rateLimitPassed := false
				dummyHandler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
					rateLimitPassed = true
				})

				// Apply IP rate limiting
				h.ipRateLimiter.Handler(dummyHandler).ServeHTTP(authRespWriter, r)

				// If rate limit was exceeded, the response was already written by IP rate limiter
				if !rateLimitPassed {
					logger.Debug("IP rate limit exceeded for unauthenticated request")
					return
				}
			}

			// Rate limit passed (or no rate limiter configured), return unauthorized
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Enrich user with group configurations before adding to context
		h.db.EnrichUserWithGroupConfig(r.Context(), user)

		// Add user to context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}
