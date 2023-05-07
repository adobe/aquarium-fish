/**
 * Copyright 2023 Adobe. All rights reserved.
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

// Testing the way to connect to cluster in maintenance mode, which allows to connect to cluster but not to accept any workload
// * Create one node with not enough resources
// * Send allocation request
// * Connect second node with enough resources in maintenance mode
// * Check second node knows about the requested allocation
// * Make sure that allocation is not happening on the second node
func Test_cluster_reconnect_in_maintenance_mode(t *testing.T) {
	// Small cluster node
	afi1 := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	t.Cleanup(func() { afi1.Cleanup(t) })

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

	// Big cluster node
	afi2 := afi1.NewClusterNode(t, "node-2", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 4
      ram_limit: 8`, "--maintenance")

	t.Cleanup(func() { afi2.Cleanup(t) })

	var app_state types.ApplicationState
	t.Run("Application should not get ALLOCATED in 15 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi1.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status == types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state.Status)
			}
		})
	})

	afi2.Restart(t)

	app_state.Status = ""
	t.Run("Application should get ALLOCATED in 15 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi1.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state.Status)
			}
		})
	})

	var res types.Resource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi2.APIAddress("api/v1/application/"+app1.UID.String()+"/resource")).
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
				Get(afi2.APIAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state.Status)
			}
		})
	})
}
