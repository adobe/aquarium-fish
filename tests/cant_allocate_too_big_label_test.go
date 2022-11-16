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

// Checks that the node will not try to execute the bigger Label Application:
// * Create Application
// * It still NEW after 40 secs
// * Destroy Application
// * Should get RECALLED state
func Test_cant_allocate_too_big_label(t *testing.T) {
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
			JSON(`{"name":"test-label", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":5,"ram":9}}]}`).
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
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.UID == uuid.Nil {
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	time.Sleep(10 * time.Second)

	var app_state types.ApplicationState
	t.Run("Application should have state NEW in 10 sec", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusNEW {
			t.Fatalf("Application Status is incorrect: %v", app_state.Status)
		}
	})

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 20 sec", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusNEW {
			t.Fatalf("Application Status is incorrect: %v", app_state.Status)
		}
	})

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 30 sec", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusNEW {
			t.Fatalf("Application Status is incorrect: %v", app_state.Status)
		}
	})

	time.Sleep(10 * time.Second)

	t.Run("Application should have state NEW in 40 sec", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app_state)

		if app_state.Status != types.ApplicationStatusNEW {
			t.Fatalf("Application Status is incorrect: %v", app_state.Status)
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get RECALLED in 10 sec", func(t *testing.T) {
		Retry(&Timer{Timeout: 5 * time.Second, Wait: 1 * time.Second}, t, func(r *R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.ApiAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&app_state)

			if app_state.Status != types.ApplicationStatusRECALLED {
				r.Fatalf("Application Status is incorrect: %v", app_state.Status)
			}
		})
	})
}
