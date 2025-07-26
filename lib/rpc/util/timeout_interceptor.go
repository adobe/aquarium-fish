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

package util

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/adobe/aquarium-fish/lib/log"
)

// TimeoutInterceptor adds timeout to unary operations
type TimeoutInterceptor struct {
	timeout time.Duration
}

// NewTimeoutInterceptor creates a new timeout interceptor
func NewTimeoutInterceptor(timeout time.Duration) *TimeoutInterceptor {
	return &TimeoutInterceptor{
		timeout: timeout,
	}
}

// WrapUnary wraps unary operations with timeout
func (i *TimeoutInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		logger := log.WithFunc("rpc", "timeoutInterceptor").With("procedure", req.Spec().Procedure)

		// Check if context already has a deadline
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			logger.Debug("Context already has deadline, skipping timeout interceptor")
			return next(ctx, req)
		}

		// Add timeout to context
		timeoutCtx, cancel := context.WithTimeout(ctx, i.timeout)
		defer cancel()

		// Call next with timeout context
		resp, err := next(timeoutCtx, req)

		// Check if timeout occurred
		if timeoutCtx.Err() == context.DeadlineExceeded {
			logger.Debug("Request timed out", "timeout", i.timeout)
			return nil, connect.NewError(connect.CodeDeadlineExceeded, timeoutCtx.Err())
		}

		return resp, err
	}
}

// WrapStreamingClient is not implemented as we only want to timeout unary operations
func (i *TimeoutInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	// Pass through without modification for streaming operations
	return next
}

// WrapStreamingHandler is not implemented as we only want to timeout unary operations
func (i *TimeoutInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	// Pass through without modification for streaming operations
	return next
}
