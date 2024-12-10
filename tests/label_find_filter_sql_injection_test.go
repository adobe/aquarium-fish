/**
 * Copyright 2021 Adobe. All rights reserved.
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

// Filters are using the user-defined SQL in find, so need to make sure there is no SQL injection
// * Create a couple of labels
// * A number of potential SQL injections
// * A number of valid filter requests
func Test_label_find_filter_sql_injection(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

drivers:
  - name: test`)

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

	var label1 types.Label
	t.Run("Create Label 1", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label1", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label1)

		if label1.UID == uuid.Nil {
			t.Fatalf("Label 1 UID is incorrect: %v", label1.UID)
		}
	})

	var label2 types.Label
	t.Run("Create Label 2", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label2", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label2)

		if label2.UID == uuid.Nil {
			t.Fatalf("Label 2 UID is incorrect: %v", label2.UID)
		}
	})

	var labels []types.Label
	t.Run("Find Label 1 with simple SQL injection", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `name = '`+label1.Name+`'; DROP label`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 1 {
			t.Fatalf("Label 1 not found: %v", labels)
		}
		if labels[0].UID != label1.UID {
			t.Fatalf("Label 1 UID is incorrect: %v != %v", labels[0].UID, label1.UID)
		}
	})

	t.Run("Find no labels with subquery SQL injection", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `name IN (DROP label)`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 0 {
			t.Fatalf("Labels weirdly found, but should not be: %v", labels)
		}
	})

	t.Run("Find no labels with stupid SQL injection", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `DROP label`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 0 {
			t.Fatalf("Labels weirdly found, but should not be: %v", labels)
		}
	})

	t.Run("Find Label 1", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `name='`+label1.Name+`'`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 1 {
			t.Fatalf("Label 1 not found: %v", labels)
		}
		if labels[0].UID != label1.UID {
			t.Fatalf("Label 1 UID is incorrect: %v != %v", labels[0].UID, label1.UID)
		}
	})

	t.Run("Find Label 2", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `name = '`+label2.Name+`'`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 1 {
			t.Fatalf("Label 2 not found: %v", labels)
		}
		if labels[0].UID != label2.UID {
			t.Fatalf("Label 2 UID is incorrect: %v != %v", labels[0].UID, label2.UID)
		}
	})

	t.Run("Find all labels with LIKE", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `name LIKE 'test-label%'`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 2 {
			t.Fatalf("Labels not found: %v", labels)
		}
	})

	t.Run("Find all labels with IN", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("filter", `name IN ('`+label1.Name+`', '`+label2.Name+`')`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 2 {
			t.Fatalf("Labels not found: %v", labels)
		}
	})
}
