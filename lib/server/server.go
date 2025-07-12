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

// Package server provides
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/rpc"
	"github.com/adobe/aquarium-fish/lib/server/meta"
)

// Wrapper wraps both HTTP and RPC servers for coordinated shutdown
type Wrapper struct {
	httpServer *http.Server
	rpcServer  *rpc.Server
}

// Shutdown gracefully shuts down both RPC and HTTP servers
func (sw *Wrapper) Shutdown(ctx context.Context) error {
	// First shutdown RPC server (which handles streaming connections)
	if err := sw.rpcServer.Shutdown(ctx); err != nil {
		log.WithFunc("server", "Shutdown").Error("Error during RPC server shutdown", "err", err)
	}

	// Then shutdown HTTP server
	if err := sw.httpServer.Shutdown(ctx); err != nil {
		log.WithFunc("server", "Shutdown").Error("Error during HTTP server shutdown", "err", err)
		return err
	}

	return nil
}

// Init startups the API server to listen for incoming requests
func Init(f *fish.Fish, apiAddress, caPath, certPath, keyPath string) (*Wrapper, error) {
	logger := log.WithFunc("server", "Init")
	caPool := x509.NewCertPool()
	if caBytes, err := os.ReadFile(caPath); err == nil {
		caPool.AppendCertsFromPEM(caBytes)
	}

	// Create meta server
	metaServer := meta.NewV1Router(f)

	// Collect RPC services from gate drivers
	gateServices := drivers.GetGateRPCServices()

	// Create a RPC server with gate services
	rpcServer := rpc.NewServer(f, gateServices)

	// Create a multiplexer to handle both HTTP and gRPC traffic
	mux := http.NewServeMux()

	// Handle gRPC/Connect-Web traffic on /grpc/*
	mux.Handle("/grpc/", http.StripPrefix("/grpc", rpcServer.Handler()))

	// Handle metadata requests on /meta/v1/data
	mux.Handle("/meta/", http.StripPrefix("/meta", metaServer))

	// Handle pprof debug endpoints if compiled as debug
	serverConnectPprofIfDebug(mux)

	// Wrap with OpenTelemetry HTTP instrumentation if monitoring is enabled
	var handler http.Handler = mux
	if monitor := f.GetMonitor(); monitor != nil && monitor.IsEnabled() {
		handler = otelhttp.NewHandler(handler, "aquarium-fish-api")
		logger.Info("API: OpenTelemetry HTTP instrumentation enabled")
	}

	s := &http.Server{
		Addr:    apiAddress,
		Handler: handler,
		TLSConfig: &tls.Config{ // #nosec G402 , keep the compatibility high since not public access
			ClientAuth: tls.RequestClientCert, // Need for the client certificate auth
			ClientCAs:  caPool,                // Verify client certificate with the cluster CA
		},

		// Security settings - optimized for streaming operations
		ReadHeaderTimeout: 30 * time.Second,
		// For streaming operations, we need much longer timeouts or no timeout at all
		// ReadTimeout: 0 means no timeout (infinite), which is appropriate for streaming
		ReadTimeout:  0,                 // No timeout for streaming operations
		WriteTimeout: 300 * time.Second, // 5 minutes for streaming responses
		IdleTimeout:  600 * time.Second, // 10 minutes to prevent connection close on inactivity
	}

	errChan := make(chan error)

	tlsListener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		logger.Error("Unable to start listener", "err", err)
		return &Wrapper{httpServer: s, rpcServer: rpcServer}, fmt.Errorf("API: Unable to start listener: %v", err)
	}

	// There is a bit of chance that API server will not startup properly,
	// but just sending quit to fish with error before that should be enough
	go func() {
		defer tlsListener.Close()

		if err := s.ServeTLS(tlsListener, certPath, keyPath); err != http.ErrServerClosed {
			logger.Error("Unable to start API server", "err", err)
			errChan <- err
			f.Quit <- syscall.SIGQUIT
		}
	}()

	// WARN: Used by integration tests
	logger.Info("API listening", "addr", tlsListener.Addr())

	return &Wrapper{httpServer: s, rpcServer: rpcServer}, nil
}
