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

package helper

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"time"

	"connectrpc.com/connect"
)

// ConnectClientType represents the type of ConnectRPC client to create
type ConnectClientType int

const (
	// ConnectClientREST creates a REST-like client using HTTP GET
	ConnectClientREST ConnectClientType = iota
	// ConnectClientGRPC creates a gRPC client
	ConnectClientGRPC
	// ConnectClientGRPCWeb creates a gRPC-Web client
	ConnectClientGRPCWeb
)

// basicAuth returns the base64 encoded string of username:password
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// NewConnectClient creates a new HTTP client and returns it along with the appropriate connect options
// for the specified client type and authentication credentials.
func NewConnectClient(username, password string, clientType ConnectClientType) (*http.Client, []connect.ClientOption) {
	// Create transport with TLS config
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	// Create HTTP client
	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: tr,
	}

	// Create base options with authentication
	baseOptions := []connect.ClientOption{
		connect.WithInterceptors(
			connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
				return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
					req.Header().Set("Authorization", "Basic "+basicAuth(username, password))
					return next(ctx, req)
				}
			}),
		),
	}

	// Add client type specific options
	switch clientType {
	case ConnectClientREST:
		baseOptions = append(baseOptions, connect.WithHTTPGet())
	case ConnectClientGRPC:
		baseOptions = append(baseOptions, connect.WithGRPC())
	case ConnectClientGRPCWeb:
		baseOptions = append(baseOptions, connect.WithGRPCWeb())
	}

	return cli, baseOptions
}
