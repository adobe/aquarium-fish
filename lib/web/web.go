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

package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/adobe/aquarium-fish/lib/log"
)

// Embed the web dashboard files
//
//go:embed dist/*
var webFiles embed.FS

// Handler creates an HTTP handler for serving the web dashboard
func Handler() http.Handler {
	logger := log.WithFunc("web", "Handler")

	// Get the dist subdirectory from the embedded filesystem
	distFS, err := fs.Sub(webFiles, "dist")
	if err != nil {
		logger.Error("Failed to create sub filesystem for dist", "err", err)
		// Return a handler that serves a simple error page
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Web dashboard not available", http.StatusServiceUnavailable)
		})
	}

	// Create file server for static assets
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := logger.With("path", r.URL.Path)

		// Clean the path
		urlPath := path.Clean(r.URL.Path)

		// Remove leading slash for filesystem lookup
		if urlPath == "/" {
			urlPath = "index.html"
		} else {
			urlPath = strings.TrimPrefix(urlPath, "/")
		}

		logger.Debug("Serving web request", "clean_path", urlPath)

		// Check if file exists
		if _, err := distFS.Open(urlPath); err != nil {
			// For SPA routes, serve index.html
			if !strings.Contains(urlPath, ".") {
				logger.Debug("Serving SPA route with index.html")
				urlPath = "index.html"

				// Set the URL path back to index.html for the file server
				r.URL.Path = "/index.html"

				// Set appropriate headers for SPA
				w.Header().Set("Cache-Control", "no-cache")
			} else {
				logger.Debug("File not found", "err", err)
				http.NotFound(w, r)
				return
			}
		} else {
			// Set the URL path for the file server
			r.URL.Path = "/" + urlPath
		}

		// Set security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Set cache headers for static assets
		if strings.Contains(urlPath, ".") && urlPath != "index.html" {
			// Cache static assets for 1 year
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		// Serve the file
		fileServer.ServeHTTP(w, r)
	})
}

// IsEmbedded returns true if the web dashboard is embedded in the binary
func IsEmbedded() bool {
	// Check if we can open the dist directory
	if _, err := webFiles.Open("dist"); err != nil {
		return false
	}
	return true
}
