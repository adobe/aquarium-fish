/**
 * Copyright 2021-2025 Adobe. All rights reserved.
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

// Package meta provides META-API for the resources
package meta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/util"
)

// H is a shortcut for map[string]any
type H map[string]any

// Processor doing processing of the META-API request
type Processor struct {
	fish *fish.Fish
}

// NewV1Router creates router for META-APIv1
func NewV1Router(f *fish.Fish) *Processor {
	return &Processor{fish: f}
}

// Return which processes the return data and represents it as requestor want to see it
func (*Processor) Return(w http.ResponseWriter, r *http.Request, code int, obj map[string]any) {
	format := r.URL.Query().Get("format")
	if len(format) == 0 {
		format = "json"
	}
	prefix := r.URL.Query().Get("prefix")

	data, err := util.SerializeMetadata(format, prefix, obj)
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to serialize metadata: %v", err), http.StatusBadRequest)
		return
	}

	mime := "text/plain; charset=utf-8"
	if format == "json" {
		mime = "application/json; charset=utf-8"
	}

	w.Header().Set("Content-Type", mime)
	w.WriteHeader(code)
	w.Write(data)
}

// DataGetList returns metadata assigned to the Resource
func (p *Processor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only the existing local resource access it's metadata
	res, err := p.fish.DB().ApplicationResourceGetByIP(strings.TrimSpace(strings.Split(r.RemoteAddr, ":")[0]))
	if err != nil {
		log.Warn("API META: Unauthorized access to meta:", err)
		p.Return(w, r, http.StatusUnauthorized, H{"message": "Unauthorized"})
		return
	}

	var metadata map[string]any

	if err = json.Unmarshal([]byte(res.Metadata), &metadata); err != nil {
		log.Errorf("Unable to parse metadata of Resource: %s %s: %w", res.Uid, res.Metadata, err)
		p.Return(w, r, http.StatusNotFound, H{"message": "Unable to parse metadata json"})
		return
	}

	p.Return(w, r, http.StatusOK, metadata)
}
