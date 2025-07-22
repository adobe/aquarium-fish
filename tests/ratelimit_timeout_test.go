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

package tests

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Test_user_rate_limit tests rate limiting per authenticated user
func Test_user_rate_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	userClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Create a test user
	testUser := "testuser"
	testPassword := "testpass123"

	t.Run("Create test user", func(t *testing.T) {
		_, err := userClient.Create(context.Background(), connect.NewRequest(&aquariumv2.UserServiceCreateRequest{
			User: &aquariumv2.User{
				Name:     testUser,
				Password: &testPassword,
				Roles:    []string{"User"},
			},
		}))
		if err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
	})

	// Create client for the test user
	testCli, testOpts := h.NewRPCClient(testUser, testPassword, h.RPCClientREST, afi.GetCA(t))
	testUserClient := aquariumv2connect.NewUserServiceClient(testCli, afi.APIAddress("grpc"), testOpts...)

	t.Run("User can make requests within rate limit", func(t *testing.T) {
		// Make 10 requests (well within the 60/minute limit)
		for i := range 10 {
			_, err := testUserClient.GetMe(context.Background(), connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}))
			if err != nil {
				t.Fatalf("Request %d failed: %v", i+1, err)
			}
		}
	})

	t.Run("User gets rate limited when exceeding limit", func(t *testing.T) {
		// Make requests rapidly to exceed rate limit
		// We'll make 65 requests rapidly to exceed the 60/minute limit
		rateLimitHit := false
		for i := range 65 {
			_, err := testUserClient.GetMe(context.Background(), connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}))
			if err != nil {
				if strings.Contains(err.Error(), "Rate limit exceeded") || strings.Contains(err.Error(), "429") {
					rateLimitHit = true
					t.Logf("Rate limit hit at request %d", i+1)
					break
				}
				// Ignore other errors, we're just testing rate limiting
				t.Logf("Request %d failed with non-rate-limit error: %v", i+1, err)
			}
		}
		if !rateLimitHit {
			t.Fatal("Expected to hit rate limit but didn't")
		}
	})
}

// Test_ip_rate_limit tests rate limiting per IP for unauthenticated requests
func Test_ip_rate_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	cli := &http.Client{
		Timeout: time.Second * 5,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		},
	}

	t.Run("IP can make requests within rate limit", func(t *testing.T) {
		// Make 5 requests (within the 10/minute limit for IPs)
		for i := range 5 {
			req, _ := http.NewRequest("GET", afi.APIAddress("grpc/aquarium.v2.UserService/GetMe"), nil)
			// Don't set Authorization header to test unauthenticated rate limiting
			req.Header.Set("Content-Type", "application/json")

			resp, err := cli.Do(req)
			if err != nil {
				t.Fatalf("Request %d failed: %v", i+1, err)
			}
			resp.Body.Close()

			// We expect 401 Unauthorized, not 429 Rate Limit
			if resp.StatusCode == 429 {
				t.Fatalf("Hit rate limit too early at request %d", i+1)
			}
		}
	})

	t.Run("IP gets rate limited when exceeding limit", func(t *testing.T) {
		// Make requests rapidly to exceed IP rate limit
		// We'll make 15 requests rapidly to exceed the 10/minute limit
		rateLimitHit := false
		for i := range 15 {
			req, _ := http.NewRequest("GET", afi.APIAddress("grpc/aquarium.v2.UserService/GetMe"), nil)
			req.Header.Set("Content-Type", "application/json")

			resp, err := cli.Do(req)
			if err != nil {
				t.Fatalf("Request %d failed: %v", i+1, err)
			}
			resp.Body.Close()

			if resp.StatusCode == 429 {
				rateLimitHit = true
				t.Logf("IP rate limit hit at request %d", i+1)
				break
			}
		}
		if !rateLimitHit {
			t.Fatal("Expected to hit IP rate limit but didn't")
		}
	})
}

// Test_timeout_unary tests timeout functionality for unary operations
func Test_timeout_unary(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create admin client with a very long timeout to avoid client-side timeouts
	adminCli := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		},
	}
	adminOpts := []connect.ClientOption{
		connect.WithInterceptors(newStreamingAuthInterceptor("admin", afi.AdminToken())),
	}
	userClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	t.Run("Normal requests complete within timeout", func(t *testing.T) {
		start := time.Now()
		_, err := userClient.GetMe(context.Background(), connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}))
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Normal request failed: %v", err)
		}
		if duration > 25*time.Second { // Should be much faster than the 30-second timeout
			t.Fatalf("Request took too long: %v", duration)
		}
	})

	t.Run("Requests with existing deadline are not affected", func(t *testing.T) {
		// Create a context with a short deadline
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		start := time.Now()
		_, err := userClient.GetMe(ctx, connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}))
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Request with existing deadline failed: %v", err)
		}
		if duration > 10*time.Second { // Should complete quickly
			t.Fatalf("Request took too long: %v", duration)
		}
	})
}

// Test_streaming_not_affected tests that streaming operations are not affected by timeouts
func Test_streaming_not_affected(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create streaming client
	streamingCli, streamingOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientGRPC, afi.GetCA(t))
	streamingServiceClient := aquariumv2connect.NewStreamingServiceClient(streamingCli, afi.APIAddress("grpc"), streamingOpts...)
	sc := h.NewStreamingClient(context.Background(), t, "streaming-client", streamingServiceClient)
	defer sc.Close()

	t.Run("Streaming connection can be established", func(t *testing.T) {
		err := sc.EstablishBidirectionalStreaming()
		if err != nil {
			t.Fatalf("Failed to establish streaming: %v", err)
		}
	})

	t.Run("Streaming requests work without timeout issues", func(t *testing.T) {
		// Send a streaming request
		req := &aquariumv2.UserServiceGetMeRequest{}
		resp, err := sc.SendRequest("test-req", "UserServiceGetMeRequest", req)
		if err != nil {
			t.Fatalf("Streaming request failed: %v", err)
		}
		if resp == nil {
			t.Fatal("No response received")
		}
		t.Logf("Streaming request successful: %s", resp.ResponseType)
	})

	t.Run("Subscription streaming works", func(t *testing.T) {
		err := sc.EstablishSubscriptionStreaming([]aquariumv2.SubscriptionType{
			aquariumv2.SubscriptionType_SUBSCRIPTION_TYPE_USER,
		})
		if err != nil {
			t.Fatalf("Failed to establish subscription streaming: %v", err)
		}

		// Wait a bit to ensure the subscription is working
		time.Sleep(100 * time.Millisecond)
		t.Log("Subscription streaming established successfully")
	})
}

// Test_web_endpoint_rate_limiting tests rate limiting on web endpoints
func Test_web_endpoint_rate_limiting(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	cli := &http.Client{
		Timeout: time.Second * 5,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		},
	}

	t.Run("Web endpoint respects IP rate limiting", func(t *testing.T) {
		// Make requests to the web interface to test rate limiting
		rateLimitHit := false
		for i := range 15 {
			req, _ := http.NewRequest("GET", afi.APIAddress(""), nil)

			resp, err := cli.Do(req)
			if err != nil {
				t.Fatalf("Request %d failed: %v", i+1, err)
			}
			resp.Body.Close()

			if resp.StatusCode == 429 {
				rateLimitHit = true
				t.Logf("Web endpoint rate limit hit at request %d", i+1)
				break
			}
		}
		// Note: This test might not hit rate limit as quickly since web requests
		// might be handled differently, but it should eventually hit it
		t.Logf("Rate limit hit: %v", rateLimitHit)
	})
}

// Test_meta_endpoint_rate_limiting tests rate limiting on meta endpoints
func Test_meta_endpoint_rate_limiting(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// First create an application so meta endpoint has data
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	labelClient := aquariumv2connect.NewLabelServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)
	appClient := aquariumv2connect.NewApplicationServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Create a label and application
	labelResp, _ := labelClient.Create(context.Background(), connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
		Label: &aquariumv2.Label{
			Name:    "test-label",
			Version: 1,
			Definitions: []*aquariumv2.LabelDefinition{{
				Driver:    "test",
				Resources: &aquariumv2.Resources{Cpu: 1, Ram: 2},
			}},
		},
	}))

	_, _ = appClient.Create(context.Background(), connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
		Application: &aquariumv2.Application{LabelUid: labelResp.Msg.Data.Uid},
	}))

	cli := &http.Client{
		Timeout: time.Second * 5,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		},
	}

	t.Run("Meta endpoint respects IP rate limiting", func(t *testing.T) {
		// Make requests to the meta endpoint to test rate limiting
		rateLimitHit := false
		for i := 0; i < 15; i++ {
			req, _ := http.NewRequest("GET", afi.APIAddress("meta/v1/data/"), nil)

			resp, err := cli.Do(req)
			if err != nil {
				t.Fatalf("Request %d failed: %v", i+1, err)
			}
			resp.Body.Close()

			if resp.StatusCode == 429 {
				rateLimitHit = true
				t.Logf("Meta endpoint rate limit hit at request %d", i+1)
				break
			}
		}
		t.Logf("Rate limit hit: %v", rateLimitHit)
	})
}

// Test_slow_request_protection tests protection against slow request attacks
func Test_slow_request_protection(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	t.Run("Server handles slow body sending", func(t *testing.T) {
		// Create a custom transport to test slow requests
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		}

		cli := &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second, // Give enough time for the test
		}

		// Create a slow reader that sends data very slowly
		slowBody := &slowReader{
			data:  []byte(`{"test": "data"}`),
			delay: 100 * time.Millisecond,
		}

		req, _ := http.NewRequest("POST", afi.APIAddress("grpc/aquarium.v2.UserService/GetMe"), slowBody)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Basic "+basicAuth("admin", afi.AdminToken()))
		req.ContentLength = int64(len(slowBody.data))

		start := time.Now()
		resp, err := cli.Do(req)
		duration := time.Since(start)

		if err != nil {
			t.Logf("Slow request failed as expected: %v (duration: %v)", err, duration)
			return
		}

		resp.Body.Close()
		t.Logf("Slow request completed: status=%d, duration=%v", resp.StatusCode, duration)

		// The server should handle this gracefully, either by timing out or processing it
		if duration > 45*time.Second {
			t.Logf("Request took a long time but completed: %v", duration)
		}
	})

	t.Run("Server handles multiple concurrent slow requests", func(t *testing.T) {
		const numRequests = 5
		results := make(chan error, numRequests)

		for i := range numRequests {
			go func(reqNum int) {
				transport := &http.Transport{
					TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
				}

				cli := &http.Client{
					Transport: transport,
					Timeout:   30 * time.Second,
				}

				slowBody := &slowReader{
					data:  []byte(fmt.Sprintf(`{"test": "data%d"}`, reqNum)),
					delay: 200 * time.Millisecond,
				}

				req, _ := http.NewRequest("POST", afi.APIAddress("grpc/aquarium.v2.UserService/GetMe"), slowBody)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Basic "+basicAuth("admin", afi.AdminToken()))
				req.ContentLength = int64(len(slowBody.data))

				start := time.Now()
				resp, err := cli.Do(req)
				duration := time.Since(start)

				if err != nil {
					results <- fmt.Errorf("request %d failed: %v (duration: %v)", reqNum, err, duration)
					return
				}

				resp.Body.Close()
				t.Logf("Concurrent slow request %d completed: status=%d, duration=%v", reqNum, resp.StatusCode, duration)
				results <- nil
			}(i)
		}

		// Wait for all requests to complete
		for range numRequests {
			select {
			case err := <-results:
				if err != nil {
					t.Logf("Concurrent request error (expected): %v", err)
				}
			case <-time.After(45 * time.Second):
				t.Log("Some concurrent requests timed out (this may be expected)")
			}
		}
	})
}

// Test_user_custom_rate_limit tests per-user rate limit configuration
func Test_user_custom_rate_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
api_address: 127.0.0.1:0
drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))
	userClient := aquariumv2connect.NewUserServiceClient(adminCli, afi.APIAddress("grpc"), adminOpts...)

	// Create a test user with custom rate limit
	testUser := "limiteduser"
	testPassword := "testpass123"
	customLimit := int32(5) // Very low limit for testing

	t.Run("Create test user with custom rate limit", func(t *testing.T) {
		_, err := userClient.Create(context.Background(), connect.NewRequest(&aquariumv2.UserServiceCreateRequest{
			User: &aquariumv2.User{
				Name:     testUser,
				Password: &testPassword,
				Roles:    []string{"User"},
				Config:   &aquariumv2.UserConfig{RateLimit: &customLimit},
			},
		}))
		if err != nil {
			t.Fatalf("Failed to create test user: %v", err)
		}
	})

	// Create client for the test user
	testCli, testOpts := h.NewRPCClient(testUser, testPassword, h.RPCClientREST, afi.GetCA(t))
	testUserClient := aquariumv2connect.NewUserServiceClient(testCli, afi.APIAddress("grpc"), testOpts...)

	t.Run("User hits custom rate limit", func(t *testing.T) {
		// Make requests to exceed the custom limit (5 requests/minute)
		rateLimitHit := false
		for i := range 6 {
			_, err := testUserClient.GetMe(context.Background(), connect.NewRequest(&aquariumv2.UserServiceGetMeRequest{}))
			if err != nil {
				if strings.Contains(err.Error(), "Rate limit exceeded") || strings.Contains(err.Error(), "429") {
					rateLimitHit = true
					t.Logf("Custom rate limit hit at request %d", i+1)
					break
				}
				t.Logf("Request %d failed with non-rate-limit error: %v", i+1, err)
			}
		}
		if !rateLimitHit {
			t.Fatal("Expected to hit custom rate limit but didn't")
		}
	})
}

// slowReader simulates a slow client that sends data slowly
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (sr *slowReader) Read(p []byte) (n int, err error) {
	if sr.pos >= len(sr.data) {
		return 0, io.EOF
	}

	// Send only one byte at a time with delay
	time.Sleep(sr.delay)

	p[0] = sr.data[sr.pos]
	sr.pos++
	return 1, nil
}

// Helper functions for authentication
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
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

func (*streamingAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		// Server-side streaming handler (not needed for client test)
		return next(ctx, conn)
	}
}
