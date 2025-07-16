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
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	"github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2/aquariumv2connect"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// This is a test which makes sure we can create and filter Labels
// * Create Label (multiple versions and names)
// * Find Label by various filters
func Test_label_create_and_filter(t *testing.T) {
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

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	// Create admin client
	adminCli, adminOpts := h.NewRPCClient("admin", afi.AdminToken(), h.RPCClientREST, afi.GetCA())

	// Create service client
	labelClient := aquariumv2connect.NewLabelServiceClient(
		adminCli,
		afi.APIAddress("grpc"),
		adminOpts...,
	)

	var labelUID string
	t.Run("Create test-label Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
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

	t.Run("Create second version of test-label Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label",
					Version: 2,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 2,
							Ram: 4,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}

		if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", resp.Msg.Data.Uid)
		}
	})

	t.Run("Create another test-label2 Label", func(t *testing.T) {
		resp, err := labelClient.Create(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceCreateRequest{
				Label: &aquariumv2.Label{
					Name:    "test-label2",
					Version: 1,
					Definitions: []*aquariumv2.LabelDefinition{{
						Driver: "test",
						Resources: &aquariumv2.Resources{
							Cpu: 1,
							Ram: 2,
						},
					}},
				},
			}),
		)
		if err != nil {
			t.Fatal("Failed to create label:", err)
		}

		if resp.Msg.Data.Uid == "" || resp.Msg.Data.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", resp.Msg.Data.Uid)
		}
	})

	// Perform label list tests
	t.Run("Listing all the labels & versions", func(t *testing.T) {
		resp, err := labelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}

		if len(resp.Msg.Data) != 3 {
			t.Fatalf("Labels count is incorrect: %v != 3", len(resp.Msg.Data))
		}
	})

	t.Run("Listing labels with name test-label", func(t *testing.T) {
		resp, err := labelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{
				Name: &[]string{"test-label"}[0],
			}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}

		if len(resp.Msg.Data) != 2 {
			t.Fatalf("Labels count is incorrect: %v != 2", len(resp.Msg.Data))
		}
	})

	t.Run("Listing labels with name test-label and version 2", func(t *testing.T) {
		resp, err := labelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{
				Name:    &[]string{"test-label"}[0],
				Version: &[]string{"2"}[0],
			}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}

		if len(resp.Msg.Data) != 1 {
			t.Fatalf("Labels count is incorrect: %v != 1", len(resp.Msg.Data))
		}
	})

	t.Run("Listing labels with version 1", func(t *testing.T) {
		resp, err := labelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{
				Version: &[]string{"1"}[0],
			}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}

		if len(resp.Msg.Data) != 2 {
			t.Fatalf("Labels count is incorrect: %v != 2", len(resp.Msg.Data))
		}
	})

	t.Run("Listing labels with version last", func(t *testing.T) {
		resp, err := labelClient.List(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceListRequest{
				Version: &[]string{"last"}[0], // "last" indicates last version
			}),
		)
		if err != nil {
			t.Fatal("Failed to list labels:", err)
		}

		if len(resp.Msg.Data) != 2 {
			t.Fatalf("Labels count is incorrect: %v != 2", len(resp.Msg.Data))
		}
	})

	t.Run("Getting specific label by UID", func(t *testing.T) {
		resp, err := labelClient.Get(
			context.Background(),
			connect.NewRequest(&aquariumv2.LabelServiceGetRequest{
				LabelUid: labelUID,
			}),
		)
		if err != nil {
			t.Fatalf("Failed to get label %s: %v", labelUID, err)
		}

		if resp.Msg.Data.Uid != labelUID {
			t.Fatalf("Label UID mismatch: %v != %v", resp.Msg.Data.Uid, labelUID)
		}
		if resp.Msg.Data.Name != "test-label" {
			t.Fatalf("Label name mismatch: %v != test-label", resp.Msg.Data.Name)
		}
		if resp.Msg.Data.Version != 1 {
			t.Fatalf("Label version mismatch: %v != 1", resp.Msg.Data.Version)
		}
	})
}
