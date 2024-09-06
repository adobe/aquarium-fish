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

// Ensure application task interface snapshot for user is working
func Test_application_task_snapshot_by_user(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

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

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	t.Run("Create User", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/user/")).
			JSON(`{"name":"test-user", "password":"test-user-password"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.UID == uuid.Nil {
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	var res types.Resource
	t.Run("Resource should be created", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/resource")).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&res)

		if res.Identifier == "" {
			t.Fatalf("Resource identifier is incorrect: %v", res.Identifier)
		}
	})

	var appTask1 types.ApplicationTask
	t.Run("Create ApplicationTask 1 Snapshot on ALLOCATE", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/task/")).
			JSON(map[string]any{"task": "snapshot", "when": types.ApplicationStatusALLOCATED}).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appTask1)

		if appTask1.UID == uuid.Nil {
			t.Fatalf("ApplicationTask 1 UID is incorrect: %v", appTask1.UID)
		}
	})

	var appTask2 types.ApplicationTask
	t.Run("Create ApplicationTask 2 Snapshot on DEALLOCATE", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/task/")).
			JSON(map[string]any{"task": "snapshot", "when": types.ApplicationStatusDEALLOCATE}).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appTask2)

		if appTask2.UID == uuid.Nil {
			t.Fatalf("ApplicationTask 2 UID is incorrect: %v", appTask2.UID)
		}
	})

	var appTasks []types.ApplicationTask
	t.Run("ApplicationTask 1 should be executed in 10 sec and 2 should not be executed", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/task/")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appTasks)

			if len(appTasks) != 2 {
				r.Fatalf("Application Tasks list does not contain 2 tasks")
			}
			if appTasks[0].UID != appTask1.UID {
				r.Fatalf("ApplicationTask 1 UID is incorrect: %v != %v", appTasks[0].UID, appTask1.UID)
			}
			if appTasks[1].UID != appTask2.UID {
				r.Fatalf("ApplicationTask 2 UID is incorrect: %v != %v", appTasks[1].UID, appTask2.UID)
			}
			if string(appTasks[0].Result) != `{"snapshots":["test-snapshot"],"when":"ALLOCATED"}` {
				r.Fatalf("ApplicationTask 1 result is incorrect: %v", appTasks[0].Result)
			}
			if string(appTasks[1].Result) != `{}` {
				r.Fatalf("ApplicationTask 2 result is incorrect: %v", appTasks[1].Result)
			}
		})
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("ApplicationTask 2 should be executed in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/task/")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appTasks)

			if len(appTasks) != 2 {
				r.Fatalf("Application Tasks list does not contain 2 tasks")
			}
			if appTasks[1].UID != appTask2.UID {
				r.Fatalf("ApplicationTask 2 UID is incorrect: %v != %v", appTasks[1].UID, appTask2.UID)
			}
			if string(appTasks[1].Result) != `{"snapshots":["test-snapshot"],"when":"DEALLOCATE"}` {
				r.Fatalf("ApplicationTask 2 result is incorrect: %v", appTasks[1].Result)
			}
		})
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}
