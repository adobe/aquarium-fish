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
	"context"
	"net/http"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
)

// Server represents the Connect server
type Server struct {
	fish *fish.Fish
	mux  *http.ServeMux
}

// NewServer creates a new Connect server
func NewServer(f *fish.Fish) *Server {
	s := &Server{
		fish: f,
		mux:  http.NewServeMux(),
	}

	// Create interceptors
	authInterceptor := NewAuthInterceptor(f)
	rbacInterceptor := NewRBACInterceptor(auth.GetEnforcer())

	// Common interceptor options
	interceptors := connect.WithInterceptors(authInterceptor, rbacInterceptor)

	// Register services
	s.mux.Handle(aquariumv2connect.NewUserServiceHandler(
		&UserService{fish: f},
		interceptors,
	))

	s.mux.Handle(aquariumv2connect.NewRoleServiceHandler(
		&RoleService{fish: f},
		interceptors,
	))

	s.mux.Handle(aquariumv2connect.NewApplicationServiceHandler(
		&ApplicationService{fish: f},
		interceptors,
	))

	s.mux.Handle(aquariumv2connect.NewLabelServiceHandler(
		&LabelService{fish: f},
		interceptors,
	))

	s.mux.Handle(aquariumv2connect.NewNodeServiceHandler(
		&NodeService{fish: f},
		interceptors,
	))

	return s
}

// Handler returns the server's HTTP handler
func (s *Server) Handler() http.Handler {
	// Support both HTTP/1.1 and HTTP/2
	return h2c.NewHandler(s.mux, &http2.Server{})
}

// ListenAndServe starts the server
func (s *Server) ListenAndServe(addr string, certFile, keyFile string) error {
	log.Info("Starting Connect server on", addr)

	handler := s.Handler()

	if certFile != "" && keyFile != "" {
		return http.ListenAndServeTLS(addr, certFile, keyFile, handler) //nolint:gosec // G114 - We don't need timeouts here
	}
	return http.ListenAndServe(addr, handler) //nolint:gosec // G114 - We don't need timeouts here
}

// Shutdown gracefully shuts down the server
func (*Server) Shutdown(_ /*ctx*/ context.Context) error {
	// TODO: Implement graceful shutdown
	return nil
}
