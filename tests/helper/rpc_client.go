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

package helper

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
)

// RPCClientType represents the type of ConnectRPC client to create
type RPCClientType int

const (
	// RPCClientREST creates a REST-like client using HTTP GET
	RPCClientREST RPCClientType = iota
	// RPCClientGRPC creates a gRPC client
	RPCClientGRPC
	// RPCClientGRPCWeb creates a gRPC-Web client
	RPCClientGRPCWeb
)

// basicAuth returns the base64 encoded string of username:password
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// NewRPCClient creates a new HTTP client and returns it along with the appropriate connect options
// for the specified client type and authentication credentials.
func NewRPCClient(username, password string, clientType RPCClientType) (*http.Client, []connect.ClientOption) {
	var cli *http.Client

	// For gRPC client type, we need HTTP/2 support for bidirectional streaming
	if clientType == RPCClientGRPC {
		// Create TLS config for HTTP/2 support
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,                       //nolint:gosec // G402 - used in tests, so not big deal
			NextProtos:         []string{"h2", "http/1.1"}, // Prefer HTTP/2
		}

		// Create HTTP/2 transport over TLS
		tr := &http2.Transport{
			TLSClientConfig: tlsConfig,
		}

		cli = &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second, // Longer timeout for streaming operations
		}
	} else {
		// For REST and gRPC-Web, use regular HTTP transport
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // G402 - used in tests, so not big deal
		}

		cli = &http.Client{
			Timeout:   time.Second * 5,
			Transport: tr,
		}
	}

	// Create base options with authentication
	baseOptions := []connect.ClientOption{
		connect.WithInterceptors(newStreamingAuthInterceptor(username, password)),
	}

	// Add client type specific options
	switch clientType {
	case RPCClientREST:
		baseOptions = append(baseOptions, connect.WithHTTPGet())
	case RPCClientGRPC:
		baseOptions = append(baseOptions, connect.WithGRPC())
	case RPCClientGRPCWeb:
		baseOptions = append(baseOptions, connect.WithGRPCWeb())
	}

	return cli, baseOptions
}

// streamingAuthInterceptor implements the full Interceptor interface for streaming auth
type streamingAuthInterceptor struct {
	username string
	password string
}

func newStreamingAuthInterceptor(username, password string) *streamingAuthInterceptor {
	return &streamingAuthInterceptor{
		username: username,
		password: password,
	}
}

func (i *streamingAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Basic "+basicAuth(i.username, i.password))
		return next(ctx, req)
	}
}

func (i *streamingAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Basic "+basicAuth(i.username, i.password))
		return conn
	}
}

func (i *streamingAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		// Server-side streaming handler (not needed for client test)
		return next(ctx, conn)
	}
}
