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

package tests

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// This is a test which makes sure we can send yaml input to create a Label
// * Create Label
// * Find Label by new filter
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

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cli := &http.Client{
		Timeout:   time.Second * 5,
		Transport: tr,
	}

	var label types.Label
	t.Run("Create test-label Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			Header("Content-Type", "application/yaml").
			Body(`---
name: test-label
version: 1
definitions:
  - driver: test
    resources:
      cpu: 1
      ram: 2`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})
	t.Run("Create second version of test-label Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			Header("Content-Type", "application/yaml").
			Body(`---
name: test-label
version: 2
definitions:
  - driver: test
    resources:
      cpu: 2
      ram: 4`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})
	t.Run("Create another test-label2 Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			Header("Content-Type", "application/yaml").
			Body(`---
name: test-label2
version: 1
definitions:
  - driver: test
    resources:
      cpu: 1
      ram: 2`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	// Perform label list tests
	var labels []types.Label
	t.Run("Listing all the labels & versions", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 3 {
			t.Fatalf("Labels count is incorrect: %v != 3", len(labels))
		}
	})
	t.Run("Listing labels with name test-label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("name", "test-label").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 2 {
			t.Fatalf("Labels count is incorrect: %v != 2", len(labels))
		}
	})
	t.Run("Listing labels with name test-label and version 2", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("name", "test-label").
			Query("version", "2").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 1 {
			t.Fatalf("Labels count is incorrect: %v != 1", len(labels))
		}
	})
	t.Run("Listing labels with version 1", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("version", "1").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 2 {
			t.Fatalf("Labels count is incorrect: %v != 2", len(labels))
		}
	})
	t.Run("Listing labels with version last", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("version", "last").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 2 {
			t.Fatalf("Labels count is incorrect: %v != 2", len(labels))
		}
	})
	t.Run("Listing labels with name test-label version last", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("name", "test-label").
			Query("version", "last").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 1 {
			t.Fatalf("Labels count is incorrect: %v != 1", len(labels))
		}
		if labels[0].Name != "test-label" {
			t.Fatalf("Label name is incorrect: %v != 'test-label'", labels[0].Name)
		}
		if labels[0].Version != 2 {
			t.Fatalf("Label version is incorrect: %v != 2", labels[0].Version)
		}
	})
}
