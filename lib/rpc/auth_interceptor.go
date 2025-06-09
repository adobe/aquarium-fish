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

package rpc

import (
	"context"
	"encoding/base64"
	"strings"

	"connectrpc.com/connect"

	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	"github.com/adobe/aquarium-fish/lib/openapi/types"
)

type contextKey string

const userContextKey = contextKey("user")

// GetUserFromContext retrieves the user from context
func GetUserFromContext(ctx context.Context) *types.User {
	user, _ := ctx.Value(userContextKey).(*types.User)
	return user
}

// AuthInterceptor handles authentication for Connect RPCs
type AuthInterceptor struct {
	fish *fish.Fish
}

// NewAuthInterceptor creates a new auth interceptor
func NewAuthInterceptor(f *fish.Fish) *AuthInterceptor {
	return &AuthInterceptor{fish: f}
}

// WrapUnary implements the connect.Interceptor interface
func (i *AuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		auth := req.Header().Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		payload, err := base64.StdEncoding.DecodeString(auth[6:])
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}

		parts := strings.SplitN(string(payload), ":", 2)
		if len(parts) != 2 {
			return nil, connect.NewError(connect.CodeUnauthenticated, nil)
		}

		username, password := parts[0], parts[1]
		log.Debugf("RPC: %s: New HTTP request received: %s", username, req.Spec().Procedure)

		var user *types.User
		if i.fish.GetCfg().DisableAuth {
			// This logic executed during performance tests only
			user, err = i.fish.DB().UserGet(username)
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}
		} else {
			user = i.fish.DB().UserAuth(username, password)
			if user == nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, nil)
			}
		}

		// Add user to context
		ctx = context.WithValue(ctx, userContextKey, user)

		return next(ctx, req)
	}
}

// WrapStreamingClient implements the connect.Interceptor interface
func (i *AuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return connect.StreamingClientFunc(func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		auth := conn.RequestHeader().Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			return nil
		}

		payload, err := base64.StdEncoding.DecodeString(auth[6:])
		if err != nil {
			return nil
		}

		parts := strings.SplitN(string(payload), ":", 2)
		if len(parts) != 2 {
			return nil
		}

		username, password := parts[0], parts[1]
		log.Debugf("RPC: %s: New gRPC-Client request received: %s", username, conn.Spec().Procedure)

		var user *types.User
		if i.fish.GetCfg().DisableAuth {
			// This logic executed during performance tests only
			user, err = i.fish.DB().UserGet(username)
			if err != nil {
				return nil
			}
		} else {
			user = i.fish.DB().UserAuth(username, password)
			if user == nil {
				return nil
			}
		}

		// Add user to context
		ctx = context.WithValue(ctx, userContextKey, user)

		return conn
	})
}

// WrapStreamingHandler implements the connect.Interceptor interface
func (i *AuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		auth := conn.RequestHeader().Get("Authorization")
		if !strings.HasPrefix(auth, "Basic ") {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		payload, err := base64.StdEncoding.DecodeString(auth[6:])
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, err)
		}

		parts := strings.SplitN(string(payload), ":", 2)
		if len(parts) != 2 {
			return connect.NewError(connect.CodeUnauthenticated, nil)
		}

		username, password := parts[0], parts[1]
		log.Debugf("RPC: %s: New gRPC request received: %s", username, conn.Spec().Procedure)

		var user *types.User
		if i.fish.GetCfg().DisableAuth {
			// This logic executed during performance tests only
			user, err = i.fish.DB().UserGet(username)
			if err != nil {
				return connect.NewError(connect.CodeUnauthenticated, err)
			}
		} else {
			user = i.fish.DB().UserAuth(username, password)
			if user == nil {
				return connect.NewError(connect.CodeUnauthenticated, nil)
			}
		}

		// Add user to context
		ctx = context.WithValue(ctx, userContextKey, user)

		return nil
	}
}
