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

package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Make sure one node filter will match to allocate
// * Setup a bunch of Fish identifiers
// * Allocate Application with one filter
// * Make sure it's allocated
func Test_node_filter_one_match_good(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
node_identifiers:
  - "example:test"
  - "example2:test2"

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							NodeFilter: []string{"example:test"},
							Cpu:        1,
							Ram:        2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == "" || labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == "" || appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}

// Make sure two the node filters needed to match to allocate
// * Setup a bunch of Fish identifiers
// * Allocate Application
// * Make sure it's allocated
func Test_node_filter_two_match_good(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
node_identifiers:
  - "example:test"
  - "example2:test2"
  - "example3:test3"

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							NodeFilter: []string{"example:test", "example2:test2"},
							Cpu:        1,
							Ram:        2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == "" || labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == "" || appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", resp.Msg.Data.Status)
			}
		})
	})
}

// Make sure one node filter cannot allocate because it's wrong
// * Setup a bunch of Fish identifiers
// * Allocate Application with one filter that is not available
// * Make sure it's NOT allocated - wait
func Test_node_filter_label_wrong_filter(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
node_identifiers:
  - "example:test"
  - "example2:test2"

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							NodeFilter: []string{"example:test-no-way-this-will-match"},
							Cpu:        1,
							Ram:        2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == "" || labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == "" || appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should NOT get ALLOCATED due to wrong filter for 5 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 5 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status == aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status should NOT be ALLOCATED: %v", resp.Msg.Data.Status)
			}
		})
	})
}

// Make sure node filter cannot allocate because node has less filters than needed
// * Setup ONE Fish identifiers
// * Allocate Application with TWO filters
// * Make sure it's NOT allocated - wait
func Test_node_filter_node_has_less_than_needed(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
node_identifiers:
  - "example:test"

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA(t))

	// Create service clients
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)
	appClient := aquariumv2connect.NewApplicationServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							NodeFilter: []string{"example:test", "example2:test2"},
							Cpu:        1,
							Ram:        2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}
		labelUID = resp.Msg.Data.Uid

		if labelUID == "" || labelUID == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", labelUID)
		}
	})

	var appUID string
	t.Run("Create Application", func(t *testing.T) {
		resp, err := appClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.ApplicationServiceCreateRequest{
				Application: &aquariumv2.Application{
					LabelUid: labelUID,
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create application:", err)
		}
		appUID = resp.Msg.Data.Uid

		if appUID == "" || appUID == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", appUID)
		}
	})

	t.Run("Application should NOT get ALLOCATED - not enough filters for 5 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 5 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			resp, err := appClient.GetState(
				context.Background(),
				connect.NewRequest(&aquariumv2.ApplicationServiceGetStateRequest{
					ApplicationUid: appUID,
				}),
			)
			if err != nil {
				r.Fatal("Failed to get application state:", err)
			}

			if resp.Msg.Data.Status == aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status should NOT be ALLOCATED: %v", resp.Msg.Data.Status)
			}
		})
	})
}
