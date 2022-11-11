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
)

// Ensure application task interface snapshot for user is working
func Test_application_task_snapshot_by_user(t *testing.T) {
	t.Parallel()
	afi := RunAquariumFish(t, `---
node_name: node-1
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  - name: test`)

	t.Cleanup(func() {
		afi.Cleanup()
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
			JSON(`{"name":"test-label", "version":1, "driver":"test", "definition": {"resources":{"cpu":1,"ram":2}}}`).
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

	var app_state types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStateStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state.Status)
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

		if res.HwAddr == "" {
			t.Fatalf("Resource hwaddr is incorrect: %v", res.HwAddr)
		}
	})

	var app_task types.ApplicationTask
	t.Run("Create ApplicationTask Snapshot", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/task/")).
			JSON(`{"task":"snapshot"}`).
			BasicAuth("test-user", "test-user-password").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_task)

		if app_task.UID == uuid.Nil {
			t.Fatalf("ApplicationTask UID is incorrect: %v", app_task.UID)
		}
	})

	var app_tasks []types.ApplicationTask
	t.Run("ApplicationTask should be executed in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/task/")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_tasks)

			if len(app_tasks) != 1 {
				r.Fatalf("Application Tasks list is empty")
			}
			if app_tasks[0].UID != app_task.UID {
				r.Fatalf("ApplicationTask UID is incorrect: %v != %v", app_tasks[0].UID, app_task.UID)
			}
			if string(app_tasks[0].Result) != `{"snapshots":["test-snapshot"]}` {
				r.Fatalf("ApplicationTask result is incorrect: %v", app_tasks[0].Result)
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

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("test-user", "test-user-password").
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStateStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", app_state.Status)
			}
		})
	})
}
