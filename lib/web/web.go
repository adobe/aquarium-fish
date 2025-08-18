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
	"context"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

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
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Web dashboard not available", http.StatusServiceUnavailable)
		})
	}

	// Check if index.html exists (indicating web assets are built)
	if _, err := distFS.Open("index.html"); err != nil {
		logger.Warn("Web dashboard assets not built - index.html not found", "err", err)
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Aquarium Fish - Web Dashboard Unavailable</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; text-align: center; }
        .container { max-width: 600px; margin: 0 auto; }
        .error { color: #e74c3c; }
        .info { color: #3498db; margin-top: 20px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Aquarium Fish</h1>
        <p class="error">Web Dashboard Unavailable</p>
        <p>The web dashboard assets have not been built yet.</p>
        <p class="info">To build the web dashboard, run: <code>cd web && ./build.sh</code></p>
        <p class="info">API endpoints are still available at <code>/grpc/*</code></p>
    </div>
</body>
</html>`))
		})
	}

	// Create file server for static assets
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Establish timeout for the request
		_, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel() // Ensure the context is canceled when the handler exits

		logger := logger.With("path", r.URL.Path)

		// Clean the path
		urlPath := path.Clean(r.URL.Path)

		// Remove leading slash for filesystem lookup
		if urlPath == "/" {
			urlPath = "index.html"
		} else {
			urlPath = strings.TrimPrefix(urlPath, "/")
		}

		// Check if file exists
		if _, err := distFS.Open(urlPath); err != nil {
			// For SPA routes, serve index.html
			if strings.Contains(urlPath, ".") {
				logger.Debug("File not found", "err", err)
				http.NotFound(w, r)
				return
			}

			logger.Debug("Serving SPA route with index.html")
			urlPath = "index.html"

			// Set appropriate headers for SPA
			w.Header().Set("Cache-Control", "no-cache")
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

		// Create new request with correct path for file server
		if urlPath == "index.html" {
			urlPath = ""
		}
		r.URL.Path = "/" + urlPath

		logger.Debug("Serving web request", "clean_path", urlPath)

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
