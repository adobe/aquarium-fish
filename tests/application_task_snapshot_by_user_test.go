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

// Ensure application task interface snapshot for user is working
func Test_application_task_snapshot_by_user(t *testing.T) {
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

	var label aquariumv2.Label
	t.Run("Admin: Create Label", func(t *testing.T) {
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
	})

	t.Run("Admin: Create User", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/")).
			JSON(`{"name":"test-user", "password":"test-user-password"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	// User-side of requests
	t.Run("User: List Label with name test-label should not be allowed by Auth", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("name", "test-label").
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusForbidden).
			End()
	})

	t.Run("User: Create Application should not be allowed by Auth", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.Uid+`"}`).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusForbidden).
			End()
	})

	t.Run("Admin: Put User role in place", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/test-user/roles")).
			JSON(`["User"]`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	var labels []aquariumv2.Label
	t.Run("User: List Label with name test-label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/label/")).
			Query("name", "test-label").
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&labels)

		if len(labels) != 1 || labels[0].Uid == uuid.Nil.String() {
			t.Fatalf("Label is incorrect")
		}
	})

	var app aquariumv2.Application
	t.Run("User: Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.Uid+`"}`).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.Uid == uuid.Nil.String() {
			t.Fatalf("Application UID is incorrect: %v", app.Uid)
		}
	})

	var appState aquariumv2.ApplicationState
	t.Run("User: Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res aquariumv2.ApplicationResource
	t.Run("User: Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/resource")).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)

		if res.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", res.Identifier)
		}
	})

	t.Run("User: Create ApplicationTask Snapshot should not be allowed by Auth", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/"+app.Uid+"/task/")).
			JSON(map[string]any{"task": "snapshot", "when": aquariumv2.ApplicationState_ALLOCATED}).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusForbidden).
			End()
	})

	t.Run("Admin: Put Power & User role in place", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/user/test-user/roles")).
			JSON(`["Power", "User"]`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	var appTask1 aquariumv2.ApplicationTask
	t.Run("User: Create ApplicationTask 1 Snapshot on ALLOCATE", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/"+app.Uid+"/task/")).
			JSON(map[string]any{"task": "snapshot", "when": aquariumv2.ApplicationState_ALLOCATED}).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appTask1)

		if appTask1.Uid == uuid.Nil.String() {
			t.Fatalf("ApplicationTask 1 UID is incorrect: %v", appTask1.Uid)
		}
	})

	var appTask2 aquariumv2.ApplicationTask
	t.Run("User: Create ApplicationTask 2 Snapshot on DEALLOCATE", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/"+app.Uid+"/task/")).
			JSON(map[string]any{"task": "snapshot", "when": aquariumv2.ApplicationState_DEALLOCATE}).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appTask2)

		if appTask2.Uid == uuid.Nil.String() {
			t.Fatalf("ApplicationTask 2 UID is incorrect: %v", appTask2.Uid)
		}
	})

	var appTasks []*aquariumv2.ApplicationTask
	t.Run("User: ApplicationTask 1 should be executed in 10 sec and 2 should not be executed", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/task/")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appTasks)

			if len(appTasks) != 2 {
				r.Fatalf("Application Tasks list does not contain 2 tasks")
			}

			// Tasks could return in any order, so reversing if first one is actually a second
			if appTasks[0].Uid != appTask1.Uid {
				appTasks[0], appTasks[1] = appTasks[1], appTasks[0]
			}

			if appTasks[0].Uid != appTask1.Uid {
				r.Fatalf("ApplicationTask 1 UID is incorrect: %v != %v", appTasks[0].Uid, appTask1.Uid)
			}
			if appTasks[1].Uid != appTask2.Uid {
				r.Fatalf("ApplicationTask 2 UID is incorrect: %v != %v", appTasks[1].Uid, appTask2.Uid)
			}
			if appTasks[0].Result.String() != `{"snapshots":["test-snapshot"],"when":"ALLOCATED"}` {
				r.Fatalf("ApplicationTask 1 result is incorrect: %v", appTasks[0].Result)
			}
			if appTasks[1].Result.String() != `{}` {
				r.Fatalf("ApplicationTask 2 result is incorrect: %v", appTasks[1].Result)
			}
		})
	})

	t.Run("User: Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.Uid+"/deallocate")).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("User: ApplicationTask 2 should be executed in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/task/")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appTasks)

			if len(appTasks) != 2 {
				r.Fatalf("Application Tasks list does not contain 2 tasks")
			}

			// Tasks could return in any order, so reversing if first one is actually a second
			if appTasks[1].Uid != appTask2.Uid {
				appTasks[0], appTasks[1] = appTasks[1], appTasks[0]
			}

			if appTasks[1].Uid != appTask2.Uid {
				r.Fatalf("ApplicationTask 2 UID is incorrect: %v != %v", appTasks[1].Uid, appTask2.Uid)
			}
			if appTasks[1].Result.String() != `{"snapshots":["test-snapshot"],"when":"DEALLOCATE"}` {
				r.Fatalf("ApplicationTask 2 result is incorrect: %v", appTasks[1].Result)
			}
		})
	})

	t.Run("User: Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("test-user", "test-user-password").
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
