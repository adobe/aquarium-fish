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
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/adobe/aquarium-fish/lib/database"
	"github.com/adobe/aquarium-fish/lib/log"
)

// rateLimitEntry tracks request count and timing for rate limiting
type rateLimitEntry struct {
	count     int32
	firstTime time.Time
	mutex     sync.RWMutex
}

// isWithinWindow checks if the entry is within the rate limit window
func (e *rateLimitEntry) isWithinWindow(window time.Duration) bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return time.Since(e.firstTime) <= window
}

// increment increases the count and returns the new count
func (e *rateLimitEntry) increment() int32 {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.count++
	return e.count
}

// reset clears the entry
func (e *rateLimitEntry) reset() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.count = 0
	e.firstTime = time.Now()
}

// UserRateLimitHandler handles rate limiting per authenticated user
type UserRateLimitHandler struct {
	db              *database.Database
	defaultLimit    int32
	window          time.Duration
	entries         map[string]*rateLimitEntry
	mutex           sync.RWMutex
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupWg       sync.WaitGroup
}

// NewUserRateLimitHandler creates a new user rate limit handler
func NewUserRateLimitHandler(db *database.Database, defaultLimit int32, window time.Duration) *UserRateLimitHandler {
	h := &UserRateLimitHandler{
		db:              db,
		defaultLimit:    defaultLimit,
		window:          window,
		entries:         make(map[string]*rateLimitEntry),
		cleanupInterval: window, // Cleanup at same interval as window
		stopCleanup:     make(chan struct{}),
	}
	h.startCleanupRoutine()
	return h
}

// startCleanupRoutine starts a goroutine to clean up expired entries
func (h *UserRateLimitHandler) startCleanupRoutine() {
	h.cleanupWg.Add(1)
	go func() {
		defer h.cleanupWg.Done()
		ticker := time.NewTicker(h.cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-h.stopCleanup:
				return
			case <-ticker.C:
				h.cleanupExpiredEntries()
			}
		}
	}()
}

// cleanupExpiredEntries removes entries that are outside the rate limit window
func (h *UserRateLimitHandler) cleanupExpiredEntries() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	now := time.Now()
	for key, entry := range h.entries {
		entry.mutex.RLock()
		expired := now.Sub(entry.firstTime) > h.window
		entry.mutex.RUnlock()

		if expired {
			delete(h.entries, key)
		}
	}
}

// getUserRateLimit gets the rate limit for a specific user
func (h *UserRateLimitHandler) getUserRateLimit(ctx context.Context, userName string) int32 {
	// Try to get user config from database
	user, err := h.db.UserGet(ctx, userName)
	if err != nil {
		// If we can't get user config, use default
		return h.defaultLimit
	}

	// Check if user has custom rate limit configuration
	if user.Config != nil {
		if rateLimit := user.Config.RateLimit; rateLimit != nil {
			return *rateLimit
		}
	}

	return h.defaultLimit
}

// Handler implements HTTP middleware for user rate limiting
func (h *UserRateLimitHandler) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithFunc("rpc", "userRateLimit").With("url_path", r.URL.Path)

		// Get user from context (set by auth handler)
		user := GetUserFromContext(r.Context())
		if user == nil {
			// No user in context means auth handler didn't set it
			// This shouldn't happen if middleware chain is correct
			logger.Debug("No user in context for rate limiting")
			next.ServeHTTP(w, r)
			return
		}

		userName := user.Name
		userLimit := h.getUserRateLimit(r.Context(), userName)

		// Checking if user's limit is set to -1 which means no limit
		if userLimit == -1 {
			next.ServeHTTP(w, r)
			return
		}

		// Get or create entry for user
		h.mutex.RLock()
		entry, exists := h.entries[userName]
		h.mutex.RUnlock()

		if !exists {
			h.mutex.Lock()
			entry, exists = h.entries[userName]
			if !exists {
				entry = &rateLimitEntry{
					count:     0,
					firstTime: time.Now(),
				}
				h.entries[userName] = entry
			}
			h.mutex.Unlock()
		}

		// Check if entry is within window
		if !entry.isWithinWindow(h.window) {
			// Reset the entry if outside window
			entry.reset()
		}

		// Increment count
		currentCount := entry.increment()

		// Check if limit exceeded
		if currentCount > userLimit {
			logger.Debug("User rate limit exceeded", "user", userName, "count", currentCount, "limit", userLimit)
			w.Header().Set("X-Ratelimit-Limit", fmt.Sprintf("%d", userLimit))
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", fmt.Sprintf("%d", entry.firstTime.Add(h.window).Unix()))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		w.Header().Set("X-Ratelimit-Limit", fmt.Sprintf("%d", userLimit))
		w.Header().Set("X-Ratelimit-Remaining", fmt.Sprintf("%d", userLimit-currentCount))
		w.Header().Set("X-Ratelimit-Reset", fmt.Sprintf("%d", entry.firstTime.Add(h.window).Unix()))

		next.ServeHTTP(w, r)
	})
}

// Shutdown stops the cleanup routine
func (h *UserRateLimitHandler) Shutdown() {
	close(h.stopCleanup)
	h.cleanupWg.Wait()
}

// IPRateLimitHandler handles rate limiting per IP for unauthenticated requests
type IPRateLimitHandler struct {
	limit           int32
	window          time.Duration
	entries         map[string]*rateLimitEntry
	mutex           sync.RWMutex
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupWg       sync.WaitGroup
}

// NewIPRateLimitHandler creates a new IP rate limit handler
func NewIPRateLimitHandler(limit int32, window time.Duration) *IPRateLimitHandler {
	h := &IPRateLimitHandler{
		limit:           limit,
		window:          window,
		entries:         make(map[string]*rateLimitEntry),
		cleanupInterval: window, // Cleanup at same interval as window
		stopCleanup:     make(chan struct{}),
	}
	h.startCleanupRoutine()
	return h
}

// startCleanupRoutine starts a goroutine to clean up expired entries
func (h *IPRateLimitHandler) startCleanupRoutine() {
	h.cleanupWg.Add(1)
	go func() {
		defer h.cleanupWg.Done()
		ticker := time.NewTicker(h.cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-h.stopCleanup:
				return
			case <-ticker.C:
				h.cleanupExpiredEntries()
			}
		}
	}()
}

// cleanupExpiredEntries removes entries that are outside the rate limit window
func (h *IPRateLimitHandler) cleanupExpiredEntries() {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	now := time.Now()
	for key, entry := range h.entries {
		entry.mutex.RLock()
		expired := now.Sub(entry.firstTime) > h.window
		entry.mutex.RUnlock()

		if expired {
			delete(h.entries, key)
		}
	}
}

// getClientIP extracts the client IP from the request
func (*IPRateLimitHandler) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP from the list
		if ips := strings.Split(xff, ","); len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}

	return r.RemoteAddr
}

// Handler implements HTTP middleware for IP rate limiting
func (h *IPRateLimitHandler) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := log.WithFunc("rpc", "ipRateLimit").With("url_path", r.URL.Path)

		clientIP := h.getClientIP(r)

		// Get or create entry for IP
		h.mutex.RLock()
		entry, exists := h.entries[clientIP]
		h.mutex.RUnlock()

		if !exists {
			h.mutex.Lock()
			entry, exists = h.entries[clientIP]
			if !exists {
				entry = &rateLimitEntry{
					count:     0,
					firstTime: time.Now(),
				}
				h.entries[clientIP] = entry
			}
			h.mutex.Unlock()
		}

		// Check if entry is within window
		if !entry.isWithinWindow(h.window) {
			// Reset the entry if outside window
			entry.reset()
		}

		// Increment count
		currentCount := entry.increment()

		// Check if limit exceeded
		if currentCount > h.limit {
			logger.Debug("IP rate limit exceeded", "ip", clientIP, "count", currentCount, "limit", h.limit)
			w.Header().Set("X-Ratelimit-Limit", fmt.Sprintf("%d", h.limit))
			w.Header().Set("X-Ratelimit-Remaining", "0")
			w.Header().Set("X-Ratelimit-Reset", fmt.Sprintf("%d", entry.firstTime.Add(h.window).Unix()))
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		w.Header().Set("X-Ratelimit-Limit", fmt.Sprintf("%d", h.limit))
		w.Header().Set("X-Ratelimit-Remaining", fmt.Sprintf("%d", h.limit-currentCount))
		w.Header().Set("X-Ratelimit-Reset", fmt.Sprintf("%d", entry.firstTime.Add(h.window).Unix()))

		next.ServeHTTP(w, r)
	})
}

// Shutdown stops the cleanup routine
func (h *IPRateLimitHandler) Shutdown() {
	close(h.stopCleanup)
	h.cleanupWg.Wait()
}
