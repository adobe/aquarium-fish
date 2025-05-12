/**
 * Copyright 2025 Adobe. All rights reserved.
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
	"bufio"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

const STEP = 50
const ATTEMPTS = 10

// Benchmark to run multiple persistent vote processes and measure the allocation time for
// It also checks that the amount of pickups is equal the amount of application requests
func Test_jenkins_agents_check_pickups_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      is_remote: true
      cpu_limit: 100000
      ram_limit: 200000`, "--timestamp=true")

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
		Timeout:   time.Second * 30,
		Transport: tr,
	}

	// Creating 2 labels - one for the app that can't be allocated and another one for a good app
	var labelNoWay types.Label
	apitest.New().
		EnableNetworking(cli).
		Post(afi.APIAddress("api/v1/label/")).
		JSON(`{"name":"label-noway", "version":1, "definitions": [
			{"driver":"test", "options":{"delay_available_capacity": 0.1}, "resources":{"cpu":999999,"ram":9999999}}
		]}`).
		BasicAuth("admin", afi.AdminToken()).
		Expect(t).
		Status(http.StatusOK).
		End().
		JSON(&labelNoWay)

	if labelNoWay.UID == uuid.Nil {
		t.Fatalf("LabelNoWay UID is incorrect: %v", labelNoWay.UID)
	}

	var labelTheWay types.Label
	apitest.New().
		EnableNetworking(cli).
		Post(afi.APIAddress("api/v1/label/")).
		JSON(`{"name":"label-theway", "version":1, "definitions": [
			{"driver":"test", "options":{"delay_available_capacity": 0.1}, "resources":{"cpu":1,"ram":2}}
		]}`).
		BasicAuth("admin", afi.AdminToken()).
		Expect(t).
		Status(http.StatusOK).
		End().
		JSON(&labelTheWay)

	if labelTheWay.UID == uuid.Nil {
		t.Fatalf("LabelTheWay UID is incorrect: %v", labelTheWay.UID)
	}

	// Running goroutines amount fetcher
	exitTest := false
	go func() {
		var amount int

		for !exitTest {
			amount = 0
			res := apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/node/this/profiling/goroutine")).
				Query("debug", "2").
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End()

			scanner := bufio.NewScanner(res.Response.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "goroutine ") {
					amount += 1
				}
			}
			res.Response.Body.Close()

			t.Log("Goroutines amount:", amount)

			time.Sleep(5 * time.Second)
		}
	}()

	// Running periodic requests to test what's the delay will be
	workerFunc := func(t *testing.T, afi *h.AFInstance, cli *http.Client) {
		var app types.Application
		apitest.New().
			EnableNetworking(cli).
			Post(afi.APIAddress("api/v1/application/")).
			JSON(`{"label_UID":"`+labelNoWay.UID.String()+`"}`).
			BasicAuth("admin", afi.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&app)

		if app.UID == uuid.Nil {
			exitTest = true
			t.Errorf("Application UID is incorrect: %v", app.UID)
		}
	}

	// Creating the apps that will not be executed and then measuring how much time it takes to
	// allocate and deallocate the actual good application
	wg := &sync.WaitGroup{}
	counter := 0

	// Monitoring pickups of Applications
	afi.WaitForLog("Fish: NEW Application with no Vote:", func(substring, line string) bool {
		// If the application processing is not expected - wg.Done() will panic
		defer func() {
			// Notifying the test of a failure in Application processing
			if r := recover(); r != nil {
				//exitTest = true
				t.Errorf("Detected not expected Application processing: %s: %v", line, r)
			}
		}()
		wg.Done()

		// Returning false here to continue to catch the Applications processing forever
		return false
	})

	// Repeated test until failure
	for range ATTEMPTS {
		// Running test on how long it takes to pickup & allocate an application
		// It should be no longer then 5 seconds (delay between pickups)
		t.Logf("Running test: (bg elections: %d)", counter)
		t.Run(fmt.Sprintf("Application should be ALLOCATED in 20 sec (bg elections: %d)", counter), func(t *testing.T) {
			var app types.Application
			// Keep track of applications in wg to make sure there is no more apps picked up by the Fish
			wg.Add(1)
			apitest.New().
				EnableNetworking(cli).
				Post(afi.APIAddress("api/v1/application/")).
				JSON(`{"label_UID":"`+labelTheWay.UID.String()+`"}`).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&app)

			if app.UID == uuid.Nil {
				exitTest = true
				t.Errorf("Desired Application UID is incorrect: %v", app.UID)
			}

			// Wait for Allocate of the Application
			var appState types.ApplicationState
			h.Retry(&h.Timer{Timeout: 15 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
				apitest.New().
					EnableNetworking(cli).
					Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
					BasicAuth("admin", afi.AdminToken()).
					Expect(r).
					Status(http.StatusOK).
					End().
					JSON(&appState)

				if appState.Status != types.ApplicationStatusALLOCATED {
					exitTest = true
					r.Fatalf("Desired Application %s Status is incorrect: %v", appState.ApplicationUID, appState.Status)
				} else {
					exitTest = false
				}
			})
		})

		// Stop test execution if error happened
		if exitTest {
			break
		}

		// Running STEP amount of parallel applications at a time to simulate a big pipeline startup
		wg.Add(STEP)
		for range STEP {
			go workerFunc(t, afi, cli)
			counter += 1
		}

		// Wait for pickup of those Applications
		wg.Wait()
	}
	t.Logf("Completed: %d", counter)
}
