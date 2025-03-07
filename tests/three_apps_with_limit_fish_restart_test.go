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

// Will allocate 2 Applications and restart the fish node to check if they will be picked up after
// * 2 apps allocated simultaneously and third one waits
// * Fish node restarts
// * Checks that 2 Apps are still ALLOCATED and third one is NEW
// * Destroying first 2 apps and third should become allocated
// * Destroy the third app
func Test_three_apps_with_limit_fish_restart(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

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
			Post(afi.APIAddress("api/v1/label/")).
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

	var apps []types.Application
	for i := range 3 {
		var app types.Application
		t.Run(fmt.Sprintf("Create Application %d", i), func(t *testing.T) {
			apitest.New().
				EnableNetworking(cli).
				Post(afi.APIAddress("api/v1/application/")).
				JSON(`{"label_UID":"`+label.UID.String()+`"}`).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&app)

			if app.UID == uuid.Nil {
				t.Fatalf("Application %d UID is incorrect: %v", i, app.UID)
			}
			apps = append(apps, app)
		})
	}

	var appState types.ApplicationState
	var appStates []types.ApplicationState
	var notAllocated types.Application
	t.Run("2 of 3 Applications should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			appStates = []types.ApplicationState{}
			for i := range apps {
				apitest.New().
					EnableNetworking(cli).
					Get(afi.APIAddress("api/v1/application/"+apps[i].UID.String()+"/state")).
					BasicAuth("admin", afi.AdminToken()).
					Expect(r).
					Status(http.StatusOK).
					End().
					JSON(&appState)

				if appState.Status != types.ApplicationStatusALLOCATED {
					notAllocated = apps[i]
				} else {
					appStates = append(appStates, appState)
				}
			}

			if len(appStates) < 2 {
				r.Fatalf("Allocated less then 2 Applications: %v", len(appStates))
			}
		})
	})

	t.Run("3rd Application should have state NEW", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+notAllocated.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appState)

		if appState.Status != types.ApplicationStatusNEW {
			t.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
		}
	})

	// Restart the fish app node
	t.Run("Restart the fish node", func(t *testing.T) {
		afi.Restart(t)
	})

	t.Run("2 of 3 Applications should be ALLOCATED right after restart", func(t *testing.T) {
		for i := range appStates {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+appStates[i].ApplicationUID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				t.Fatalf("Allocated Application Status is incorrect: %v", appState.Status)
			}
		}
	})

	t.Run("3rd Application still should have state NEW", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+notAllocated.UID.String()+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appState)

		if appState.Status != types.ApplicationStatusNEW {
			t.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
		}
	})

	t.Run("Deallocate the Applications", func(t *testing.T) {
		for i := range appStates {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+appStates[i].ApplicationUID.String()+"/deallocate")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End()
		}
	})

	t.Run("3rd Application should get ALLOCATED in 30 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 5 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+notAllocated.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Deallocate the 3rd Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+notAllocated.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("3rd Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+notAllocated.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}
