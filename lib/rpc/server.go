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
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/drivers/gate"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
)

// Server represents the Connect server
type Server struct {
	fish             *fish.Fish
	mux              *http.ServeMux
	streamingService *StreamingService
}

// NewServer creates a new Connect server
func NewServer(f *fish.Fish, additionalServices []gate.RPCService) *Server {
	s := &Server{
		fish: f,
		mux:  http.NewServeMux(),
	}

	// Register services WITHOUT interceptors (auth/rbac is handled at HTTP level)
	s.mux.Handle(aquariumv2connect.NewUserServiceHandler(
		&UserService{fish: f},
	))

	s.mux.Handle(aquariumv2connect.NewRoleServiceHandler(
		&RoleService{fish: f},
	))

	s.mux.Handle(aquariumv2connect.NewApplicationServiceHandler(
		&ApplicationService{fish: f},
	))

	s.mux.Handle(aquariumv2connect.NewLabelServiceHandler(
		&LabelService{fish: f},
	))

	s.mux.Handle(aquariumv2connect.NewNodeServiceHandler(
		&NodeService{fish: f},
	))

	// Create and store streaming service
	streamingService := NewStreamingService(f)
	s.streamingService = streamingService
	s.mux.Handle(aquariumv2connect.NewStreamingServiceHandler(
		streamingService,
	))

	// Register additional services from gate drivers
	for _, svc := range additionalServices {
		log.Debug().Msgf("RPC: Registering additional service: %s", svc.Path)
		s.mux.Handle(svc.Path, svc.Handler)
	}

	return s
}

// Handler returns the server's HTTP handler
func (s *Server) Handler() http.Handler {
	// Create auth and RBAC handlers
	authHandler := rpcutil.NewAuthHandler(s.fish.DB())
	rbacHandler := rpcutil.NewRBACHandler(auth.GetEnforcer())

	// Build middleware chain: Auth -> RBAC -> YAML -> Connect RPC
	// I found that ConnectRPC interceptors are not very good for auth needs,
	// so moved those to the handlers even before it gets to the RPC side
	handler := authHandler.Handler(
		rbacHandler.Handler(
			rpcutil.YAMLToJSONHandler(s.mux),
		),
	)

	// Support both HTTP/1.1 and HTTP/2
	return h2c.NewHandler(handler, &http2.Server{})
}

// ListenAndServe starts the server
func (s *Server) ListenAndServe(addr string, certFile, keyFile string) error {
	log.Info().Msgf("Starting Connect server on %s", addr)

	handler := s.Handler()

	if certFile != "" && keyFile != "" {
		return http.ListenAndServeTLS(addr, certFile, keyFile, handler) //nolint:gosec // G114 - We don't need timeouts here
	}
	return http.ListenAndServe(addr, handler) //nolint:gosec // G114 - We don't need timeouts here
}

// Shutdown gracefully shuts down the server and all streaming connections
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("RPC: Starting graceful server shutdown...")

	// First, gracefully shutdown all streaming connections
	// Use half the available context timeout for streaming shutdown
	deadline, hasDeadline := ctx.Deadline()
	streamingTimeout := 15 * time.Second // Default to 15 seconds
	if hasDeadline {
		remaining := time.Until(deadline)
		if remaining > 30*time.Second {
			streamingTimeout = 15 * time.Second // Use 15 seconds if we have plenty of time
		} else if remaining > 10*time.Second {
			streamingTimeout = remaining / 2 // Use half the remaining time
		} else {
			streamingTimeout = remaining - 5*time.Second // Leave 5 seconds for other cleanup
			if streamingTimeout < 1*time.Second {
				streamingTimeout = 1 * time.Second // Minimum 1 second
			}
		}
	}

	log.Info().Msgf("RPC: Shutting down streaming connections with %v timeout...", streamingTimeout)
	if s.streamingService != nil {
		// Create a timeout context
		streamingCtx, cancel := context.WithTimeout(ctx, streamingTimeout)
		defer cancel()

		s.streamingService.GracefulShutdown(streamingCtx)
	}

	log.Info().Msg("RPC: Server shutdown completed")
	return nil
}
