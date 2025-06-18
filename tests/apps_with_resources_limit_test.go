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

// Will check how the Apps are allocated with limited amount of resources it should looks like:
// * 2 random apps allocated simultaneously and third one waits
// * Destroying first 2 apps and third should become allocated
// * Destroy the third app
func Test_three_apps_with_limit(t *testing.T) {
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

// Will check how the Apps are allocated with limited amount of global slots
func Test_apps_with_slot_limit(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
node_slots_limit: 1

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 999
      ram_limit: 999
    test/another:
      cpu_limit: 999
      ram_limit: 999`)

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

	var label1 aquariumv2.Label
	t.Run("Create Label 1", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label1", "version":1, "definitions": [{"driver":"test", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label1)

		if label1.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label1.Uid)
		}
	})
	var label2 aquariumv2.Label
	t.Run("Create Label 2", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label2", "version":1, "definitions": [{"driver":"test/another", "resources":{"cpu":1,"ram":2}}]}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label2)

		if label2.Uid == uuid.Nil.String() {
			t.Fatalf("Label UID is incorrect: %v", label2.Uid)
		}
	})

	var apps []*aquariumv2.Application
	for _, labelUID := range []string{label1.Uid, label2.Uid} {
		for i := range 2 {
			var app *aquariumv2.Application
			t.Run(fmt.Sprintf("Create Application %d", i), func(t *testing.T) {
				apitest.New().
					EnableNetworking(cli).
					Post(afi.APIAddress("api/v1/application/")).
					JSON(`{"label_UID":"`+labelUID+`"}`).
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
	}

	var appState *aquariumv2.ApplicationState
	var appStates []*aquariumv2.ApplicationState
	var notAllocated []*aquariumv2.Application
	t.Run("1 of 4 Applications should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			notAllocated = []*aquariumv2.Application{}
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
					notAllocated = append(notAllocated, apps[i])
				} else {
					appStates = append(appStates, appState)
				}
			}

			if len(appStates) < 1 {
				r.Fatalf("Allocated less then 1 Application: %v", len(appStates))
			}
		})
	})

	t.Run("Not allocated Applications should have state NEW", func(t *testing.T) {
		for _, app := range notAllocated {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_NEW {
				t.Fatalf("Not allocated Application Status is incorrect: %v", appState.Status)
			}
		}
	})

	t.Run("Deallocate the allocated Application", func(t *testing.T) {
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

	t.Run("Another Application should get ALLOCATED in 30 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			notAllocated = []*aquariumv2.Application{}
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

				if appState.Status == aquariumv2.ApplicationState_DEALLOCATED {
					// Skipping this one
				} else if appState.Status != aquariumv2.ApplicationState_ALLOCATED {
					notAllocated = append(notAllocated, apps[i])
				} else {
					appStates = append(appStates, appState)
				}
			}

			if len(appStates) < 1 {
				r.Fatalf("Allocated less then 1 Application: %v", len(appStates))
			}
		})
	})

	t.Run("Not allocated Applications should have state NEW", func(t *testing.T) {
		for _, app := range notAllocated {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.Uid+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != aquariumv2.ApplicationState_NEW {
				t.Fatalf("Not allocated Application Status is incorrect: %v", appState.Status)
			}
		}
	})
}
