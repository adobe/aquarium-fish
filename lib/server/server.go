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
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/adobe/aquarium-fish/lib/drivers"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/rpc"
	"github.com/adobe/aquarium-fish/lib/server/meta"
)

// Init startups the API server to listen for incoming requests
func Init(f *fish.Fish, apiAddress, caPath, certPath, keyPath string) (*http.Server, error) {
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

	s := &http.Server{
		Addr:    apiAddress,
		Handler: mux,
		TLSConfig: &tls.Config{ // #nosec G402 , keep the compatibility high since not public access
			ClientAuth: tls.RequestClientCert, // Need for the client certificate auth
			ClientCAs:  caPool,                // Verify client certificate with the cluster CA
		},

		// Security settings
		ReadHeaderTimeout: 5 * time.Second,
	}

	errChan := make(chan error)

	tlsListener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return s, log.Error("API: Unable to start listener:", err)
	}

	// There is a bit of chance that API server will not startup properly,
	// but just sending quit to fish with error before that should be enough
	go func() {
		defer tlsListener.Close()

		if err := s.ServeTLS(tlsListener, certPath, keyPath); err != http.ErrServerClosed {
			log.Error("API: Unable to start API server:", err)
			errChan <- err
			f.Quit <- syscall.SIGQUIT
		}
	}()

	log.Info("API listening on:", tlsListener.Addr())

	return s, nil
}
