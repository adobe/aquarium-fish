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

// The test ensures the Node is actually participate in generating it's data UID's
// * Checks Label
// * Checks Application
// * Checks ApplicationState
// * Checks Resource
// * TODO: Other data UIDs
func Test_generated_uids_prefix_is_node_prefix(t *testing.T) {
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

	var nodes []*aquariumv2.Node
	var node *aquariumv2.Node
	t.Run("Get node data", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&nodes)

		if len(nodes) != 1 {
			t.Fatalf("Nodes list count is not 1: %d", len(nodes))
		}
		node = nodes[0]
		if node.Uid == uuid.Nil.String() {
			t.Fatalf("Node UID is incorrect: %v", node.Uid)
		}
	})

	var label aquariumv2.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label.Uid)
		}

		if label.Uid[:6] != node.Uid[:6] {
			t.Fatalf("Label UID prefix != Node UID prefix: %v, %v", label.Uid, node.Uid)
		}
	})

	var app aquariumv2.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.Uid+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.Uid == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", app.Uid)
		}

		if app.Uid[:6] != node.Uid[:6] {
			t.Fatalf("Application UID prefix != Node UID prefix: %v, %v", app.Uid, node.Uid)
		}
	})

	var appState aquariumv2.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Uid == uuid.Nil.String() {
				t.Fatalf("ApplicationState UID is incorrect: %v", appState.Uid)
			}

			if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}

			if appState.Uid[:6] != node.Uid[:6] {
				t.Fatalf("ApplicationState UID prefix != Node UID prefix: %v, %v", appState.Uid, node.Uid)
			}
		})
	})

	var res aquariumv2.ApplicationResource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/resource")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)

		if res.Uid == uuid.Nil.String() {
			t.Fatalf("Resource UID is incorrect: %v", res.Uid)
		}

		if res.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", res.Identifier)
		}

		if res.Uid[:6] != node.Uid[:6] {
			t.Fatalf("Resource UID prefix != Node UID prefix: %v, %v", res.Uid, node.Uid)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}
