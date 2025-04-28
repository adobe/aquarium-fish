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
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Benchmark to check how many nodes could wait for Application
func Test_jenkins_agents_awaiting(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  gates: {}
  providers:
    test:
      cpu_limit: 100000
      ram_limit: 200000`)

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

	var label types.Label
	apitest.New().
		EnableNetworking(cli).
		Post(afi.APIAddress("api/v1/label/")).
		JSON(`{"name":"test-label", "version":1, "definitions": [
			{"driver":"test", "resources":{"cpu":1,"ram":2}}
		]}`).
		BasicAuth("admin", afi.AdminToken()).
		Expect(t).
		Status(http.StatusOK).
		End().
		JSON(&label)

	if label.UID == uuid.Nil {
		t.Fatalf("Label UID is incorrect: %v", label.UID)
	}

	// Running periodic requests to test what's the delay will be
	wg := &sync.WaitGroup{}
	reachedLimit := false
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, afi *h.AFInstance, cli *http.Client) {
		defer wg.Done()

		var app types.Application
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
			t.Errorf("Application UID is incorrect: %v", app.UID)
		}

		var appState types.ApplicationState
		for !reachedLimit {
			start := time.Now()
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End().
				JSON(&appState)

			elapsed := time.Since(start).Milliseconds()
			t.Logf("Request delay: %dms", elapsed)
			if elapsed > 5000 {
				reachedLimit = true
			}
			time.Sleep(5 * time.Second)
		}
		t.Logf("Client thread completed")
	}
	counter := 0
	for !reachedLimit {
		// Running 40 parallel threads at a time to simulate a big pipeline startup
		for range 40 {
			wg.Add(1)
			go workerFunc(t, wg, afi, cli)
			counter += 1
		}
		t.Logf("Client threads: %d", counter)
		time.Sleep(10 * time.Second)
	}
	t.Logf("Completed, waiting for stop: %d", counter)
	wg.Wait()
}
