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

// Will allocate 2 Applications and restart the fish node to check if they will be picked up after
// * 2 apps allocated simultaneous and third one waits
// * Fish node restarts
// * Checks that 2 Apps are still ALLOCATED and third one is NEW
// * Destroying first 2 apps and third should become allocated
// * Destroy the third app
func Test_three_apps_with_limit_fish_restart(t *testing.T) {
	t.Parallel()
	afi := RunAquariumFish(t, `---
node_name: node-1
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 4
      ram_limit: 8`)

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
			JSON(`{"name":"test-label", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":2,"ram":4}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app1 types.Application
	t.Run("Create Application 1", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app1)

		if app1.UID == uuid.Nil {
			t.Fatalf("Application 1 UID is incorrect: %v", app1.UID)
		}
	})

	var app2 types.Application
	t.Run("Create Application 2", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app2)

		if app2.UID == uuid.Nil {
			t.Fatalf("Application 2 UID is incorrect: %v", app2.UID)
		}
	})

	var app3 types.Application
	t.Run("Create Application 3", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.ApiAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+label.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app3)

		if app3.UID == uuid.Nil {
			t.Fatalf("Application 3 UID is incorrect: %v", app3.UID)
		}
	})

	var app_state types.ApplicationState
	t.Run("Application 1 should get ALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application 1 Status is incorrect: %v", app_state.Status)
			}
		})
	})

	t.Run("Application 2 should get ALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app2.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application 2 Status is incorrect: %v", app_state.Status)
			}
		})
	})

	t.Run("Application 3 should have state NEW", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app3.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusNEW {
			t.Fatalf("Application 3 Status is incorrect: %v", app_state.Status)
		}
	})

	// Restart the fish app node
	t.Run("Restart the fish node", func(t *testing.T) {
		afi.Restart(t)
	})

	t.Run("Application 1 should be ALLOCATED right after restart", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app1.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusALLOCATED {
			t.Fatalf("Application 1 Status is incorrect: %v", app_state.Status)
		}
	})

	t.Run("Application 2 should be ALLOCATED right after restart", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app2.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusALLOCATED {
			t.Fatalf("Application 2 Status is incorrect: %v", app_state.Status)
		}
	})

	t.Run("Application 3 still should have state NEW", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app3.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusNEW {
			t.Fatalf("Application 3 Status is incorrect: %v", app_state.Status)
		}
	})

	t.Run("Deallocate the Application 1", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app1.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Deallocate the Application 2", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app2.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application 1 should get DEALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app1.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application 1 Status is incorrect: %v", app_state.Status)
			}
		})
	})

	t.Run("Application 2 should get DEALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app2.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application 2 Status is incorrect: %v", app_state.Status)
			}
		})
	})

	t.Run("Application 3 should get ALLOCATED in 40 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 40 * time.Second, Wait: 5 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app3.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application 3 Status is incorrect: %v", app_state.Status)
			}
		})
	})

	t.Run("Deallocate the Application 3", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app3.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application 3 should get DEALLOCATED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app3.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application 3 Status is incorrect: %v", app_state.Status)
			}
		})
	})
}
