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
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// This is a test which makes sure we can send json input to create a Label
// * Create Label with json
// * Check Label was created
func Test_json_label_create(t *testing.T) {
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

	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: afi.GetCA(t)},
		},
	}

	var labelResp aquariumv2.LabelServiceCreateResponse
	t.Run("Create & check JSON Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("grpc/aquarium.v2.LabelService/Create")).
			Header("Content-Type", "application/json").
			Body(`{"label":{"name":"test-label","version":1,"definitions":[{"driver":"test","options":{"fail_options_apply":0},"resources":{"cpu":1,"ram":2}}]}}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labelResp)

		if labelResp.GetStatus() != true {
			t.Fatalf("Can't create label: %v", labelResp.Message)
		}

		label := labelResp.Data
		if label.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label.Uid)
		}
		if label.Name != "test-label" {
			t.Fatalf("Label Name is incorrect: %v", label.Name)
		}
		if label.Version != 1 {
			t.Fatalf("Label Version is incorrect: %v", label.Version)
		}
		if len(label.Definitions) != 1 {
			t.Fatalf("Label Definitions size is incorrect: %v", len(label.Definitions))
		}
		if label.Definitions[0].Driver != "test" {
			t.Fatalf("Label Definition driver is incorrect: %v", label.Definitions[0].Driver)
		}
		if label.Definitions[0].Resources.Cpu != 1 {
			t.Fatalf("Label Definition resources Cpu is incorrect: %v", label.Definitions[0].Resources.Cpu)
		}
		if label.Definitions[0].Resources.Ram != 2 {
			t.Fatalf("Label Definition resources Ram is incorrect: %v", label.Definitions[0].Resources.Ram)
		}
	})
}
