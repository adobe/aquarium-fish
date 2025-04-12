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

// Just regular maintenance + shutdown test:
// * Allocate Application
// * Sending maintenance request with shutdown
// * Fish should be running
// * Destroy Application
// * Fish should shutdown in 10 sec
func Test_shutdown_after_maintenace(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
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

	var label types.Label
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

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
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
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Send maintenance + shutdown request", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("shutdown", "true").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Check Fish node is still running after 10s", func(t *testing.T) {
		time.Sleep(10 * time.Second)
		if !afi.IsRunning() {
			t.Fatalf("Fish is not running anymore, but should due to still running applications")
		}
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Fish should stop in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			if afi.IsRunning() {
				r.Fatalf("Fish is still running, but should be stopped already")
			}
		})
	})
}

// Immediate shutdown test without maintenance:
// * Allocate Application
// * Sending immediate shutdown request
// * Fish should shutdown in 10 sec
func Test_immediate_shutdown(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
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

	var label types.Label
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

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
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
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Send immediate shutdown request", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("enable", "false").
			Query("shutdown", "true").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Fish should stop in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			if afi.IsRunning() {
				r.Fatalf("Fish is still running, but should be stopped already")
			}
		})
	})
}

// Shutdown after delay:
// * Allocate Application
// * Sending shutdown request with delay of 11s
// * Fish should not shutdown in 10 sec
// * Fish should shutdown in 10 sec
func Test_shutdown_after_delay(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
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

	var label types.Label
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

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
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
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Send immediate shutdown request with delay", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("enable", "false").
			Query("shutdown", "true").
			Query("shutdown_delay", "11s").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Check Fish node is still running after 10s", func(t *testing.T) {
		time.Sleep(10 * time.Second)
		if !afi.IsRunning() {
			t.Fatalf("Fish is not running anymore, but should due to delay")
		}
	})

	t.Run("Fish should stop in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			if afi.IsRunning() {
				r.Fatalf("Fish is still running, but should be stopped already")
			}
		})
	})
}

// Cancel shutdown mode during maintenance
// * Allocate Application
// * Sending maintenance request with shutdown
// * Fish should not shutdown in 10 sec
// * Sending shutdown cancel request
// * Fish should not shutdown in 10 sec
func Test_shutdown_cancel_during_maintenance(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
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

	var label types.Label
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

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	var app types.Application
	t.Run("Create Application", func(t *testing.T) {
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
			t.Fatalf("Application UID is incorrect: %v", app.UID)
		}
	})

	var appState types.ApplicationState
	t.Run("Application should get ALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Send maintenance + shutdown request", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("shutdown", "true").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Check Fish node is still running after 10s", func(t *testing.T) {
		time.Sleep(10 * time.Second)
		if !afi.IsRunning() {
			t.Fatalf("Fish is not running anymore, but should due to delay")
		}
	})

	t.Run("Send shutdown cancel request", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("enable", "false").
			Query("shutdown", "false").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Deallocate the Application", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Application should get DEALLOCATED in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			if appState.Status != types.ApplicationStatusDEALLOCATED {
				r.Fatalf("Application Status is incorrect: %v", appState.Status)
			}
		})
	})

	t.Run("Check Fish node is still running after 10s", func(t *testing.T) {
		time.Sleep(10 * time.Second)
		if !afi.IsRunning() {
			t.Fatalf("Fish is not running anymore, but should due to cancelled shutdown")
		}
	})
}

// Cancel shutdown mode during delay
// * Allocate Application
// * Sending shutdown request with delay of 15s
// * Fish should not shutdown in 10 sec
// * Sending shutdown cancel request
// * Fish should not shutdown in 10 sec
func Test_shutdown_cancel_during_delay(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates:
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

	t.Run("Send shutdown request with delay", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("enable", "false").
			Query("shutdown", "true").
			Query("shutdown_delay", "15s").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Check Fish node is still running after 10s", func(t *testing.T) {
		time.Sleep(10 * time.Second)
		if !afi.IsRunning() {
			t.Fatalf("Fish is not running anymore, but should due to delay")
		}
	})

	t.Run("Send shutdown cancel request", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Get(afi.APIAddress("api/v1/node/this/maintenance")).
			Query("enable", "false").
			Query("shutdown", "false").
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Check Fish node is still running after 10s", func(t *testing.T) {
		time.Sleep(10 * time.Second)
		if !afi.IsRunning() {
			t.Fatalf("Fish is not running anymore, but should due to cancelled shutdown")
		}
	})
}
