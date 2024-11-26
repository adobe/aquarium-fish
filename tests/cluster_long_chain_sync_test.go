/**
 * Copyright 2024 Adobe. All rights reserved.
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

// Testing cluster of 5 machines connected in chain how they are sync with each other
// * Create 5 nodes in different locations to control the amount of connections
// * Allocate Application on node-1 to execute only on node-5
// * Get Application information from node-3
// * Destroy Application from node-4
func Test_cluster_long_chain_sync_destroy(t *testing.T) {
	// Small cluster node
	afi1 := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi2 := afi1.NewClusterNode(t, "node-2", `---
node_location: test_loc-2

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi3 := afi2.NewClusterNode(t, "node-3", `---
node_location: test_loc-3

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi4 := afi3.NewClusterNode(t, "node-4", `---
node_location: test_loc-4

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Big cluster node
	afi5 := afi4.NewClusterNode(t, "node-5", `---
node_location: test_loc-5

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 4
      ram_limit: 8`)

	t.Cleanup(func() {
		afi1.Cleanup(t)
		afi2.Cleanup(t)
		afi3.Cleanup(t)
		afi4.Cleanup(t)
		afi5.Cleanup(t)
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
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi1.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":4,"ram":8}}]}`).
			BasicAuth("admin", afi1.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app1 types.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi1.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi1.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app1)

		if app1.UID == uuid.Nil {
			t.Fatalf("Application UID is incorrect: %v", app1.UID)
		}
	})

	var app5 types.Application
	t.Run("Application should sync in cluster chain in 2 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 2 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi5.APIAddress("api/v1/application/"+app1.UID.String())).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app5)

			if app5.UID != app1.UID {
				r.Fatalf("Application UID is incorrect: %v != %v", app5.UID, app1.UID)
			}
		})
	})

	var app_state5 types.ApplicationState
	t.Run("Application should get ALLOCATED in 15 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi5.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state5)

			if app_state5.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state5.Status)
			}
		})
	})

	var app_state1 types.ApplicationState
	t.Run("Application should sync ALLOCATED status in cluster chain in 2 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 2 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi1.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state1)

			if app_state1.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state1.Status)
			}
		})
	})

	var res types.Resource
	t.Run("Resource should be created and synced in cluster already", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi1.APIAddress("api/v1/application/"+app1.UID.String()+"/resource")).
			BasicAuth("admin", afi1.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)

		if res.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", res.Identifier)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi1.APIAddress("api/v1/application/"+app1.UID.String()+"/deallocate")).
			BasicAuth("admin", afi1.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi5.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state5)

			if app_state5.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state5.Status)
			}
		})
	})

	t.Run("Application should sync DEALLOCATED status in cluster chain in 2 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 2 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi1.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state1)

			if app_state1.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state1.Status)
			}
		})
	})
}
