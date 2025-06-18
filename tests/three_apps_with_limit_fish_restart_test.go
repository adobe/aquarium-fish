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

drivers:
  gates: {}
  providers:
    test:
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

	var label aquariumv2.Label
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

		if label.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label.Uid)
		}
	})

	var apps []*aquariumv2.Application
	for i := range 3 {
		var app *aquariumv2.Application
		t.Run(fmt.Sprintf("Create Application %d", i), func(t *testing.T) {
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
				t.Fatalf("Application %d UID is incorrect: %v", i, app.Uid)
			}
			apps = append(apps, app)
		})
	}

	var appState *aquariumv2.ApplicationState
	var appStates []*aquariumv2.ApplicationState
	var notAllocated *aquariumv2.Application
	t.Run("2 of 3 Applications should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			appStates = []*aquariumv2.ApplicationState{}
			for i := range apps {
				apitest.New().
					EnableNetworking(cli).
					Get(afi.APIAddress("api/v1/application/"+apps[i].Uid+"/state")).
					BasicAuth("admin", afi.AdminToken()).
					Expect(r).
					Status(http.StatusOK).
					End().
					JSON(&appState)

				if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
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
			Get(afi.APIAddress("api/v1/application/"+notAllocated.Uid+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appState)

		if appState.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
		}
	})

	// Restart the fish app node
	afi.Restart(t)

	t.Run("2 of 3 Applications should be ALLOCATED right after restart", func(t *testing.T) {
		for i := range appStates {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+appStates[i].ApplicationUid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
				t.Fatalf("Allocated Application Status is incorrect: %v", appState.Status)
			}
		}
	})

	t.Run("3rd Application still should have state NEW", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+notAllocated.Uid+"/state")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&appState)

		if appState.Status != aquariumv2.ApplicationState_NEW {
			t.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
		}
	})

	t.Run("Deallocate the Applications", func(t *testing.T) {
		for i := range appStates {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+appStates[i].ApplicationUid+"/deallocate")).
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
				Get(afi.APIAddress("api/v1/application/"+notAllocated.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Deallocate the 3rd Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+notAllocated.Uid+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("3rd Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+notAllocated.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_DEALLOCATED {
				r.Fatalf("3rd Application Status is incorrect: %v", appState.Status)
			}
		})
	})
}
