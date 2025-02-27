/**
 * Copyright 2024 Adobe. All rights reserved.
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

// Checks if node can handle multiple application requests at a time
// Fish node should be able to handle ~20 requests / second when limited to 2 CPU core and 500MB of memory
func Test_allocate_apps_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
cpu_limit: 2
mem_target: "512MB"

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 1000
      ram_limit: 2000`)

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
	})

	// Spin up 50 of threads to create application and look what will happen
	wg := &sync.WaitGroup{}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(t *testing.T, wg *sync.WaitGroup, id int, afi *h.AFInstance, label string) {
			defer wg.Done()

			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			cli := &http.Client{
				Timeout:   time.Second * 5,
				Transport: tr,
			}

			var app types.Application
			t.Run(fmt.Sprintf("%04d Create Application", id), func(t *testing.T) {
				apitest.New().
					EnableNetworking(cli).
					Post(afi.APIAddress("api/v1/application/")).
					JSON(`{"label_UID":"`+label+`"}`).
					BasicAuth("admin", afi.AdminToken()).
					Expect(t).
					Status(http.StatusOK).
					End().
					JSON(&app)

				if app.UID == uuid.Nil {
					t.Errorf("Application UID is incorrect: %v", app.UID)
				}
			})
		}(t, wg, i, afi, label.UID.String())
	}
	wg.Wait()
}

// Checks if node can handle multiple application requests at a time with no auth
// Without auth it should be relatively simple for the fish node to ingest 200 requests in less then a second
func Test_allocate_apps_noauth_stress(t *testing.T) {
	//t.Parallel()  - nope just one at a time
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
cpu_limit: 8
mem_target: "1024MB"

api_address: 127.0.0.1:0
proxy_ssh_address: 127.0.0.1:0

disable_auth: true

drivers:
  - name: test
    cfg:
      cpu_limit: 1000
      ram_limit: 2000`)

	t.Cleanup(func() {
		afi.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	tr := http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	cli := http.Client{
		Timeout:   time.Second * 5,
		Transport: &tr,
	}

	var label types.Label
	t.Run("Create Label", func(t *testing.T) {
		apitest.New().
			EnableNetworking(&cli).
			Post(afi.APIAddress("api/v1/label/")).
			JSON(`{"name":"test-label", "version":1, "definitions": [
				{"driver":"test", "resources":{"cpu":1,"ram":2}}
			]}`).
			BasicAuth("admin", "notoken").
			Expect(t).
			Status(http.StatusOK).
			End().
			JSON(&label)

		if label.UID == uuid.Nil {
			t.Fatalf("Label UID is incorrect: %v", label.UID)
		}
	})

	// Flooding the node with 100 batches of 200 parallel Applications requests
	for b := 0; b < 100; b++ {
		// Spin up 200 of threads to create application and look what will happen
		wg := &sync.WaitGroup{}
		afi.PrintMemUsage(t)
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func(t *testing.T, wg *sync.WaitGroup, batch, id int, afi *h.AFInstance, label string) {
				defer wg.Done()

				var app types.Application
				t.Run(fmt.Sprintf("%03d-%04d Create Application", batch, id), func(t *testing.T) {
					apitest.New().
						EnableNetworking(&cli).
						Post(afi.APIAddress("api/v1/application/")).
						JSON(`{"label_UID":"`+label+`"}`).
						BasicAuth("admin", "notoken").
						Expect(t).
						Status(http.StatusOK).
						End().
						JSON(&app)

					if app.UID == uuid.Nil {
						t.Errorf("Application UID is incorrect: %v", app.UID)
					}
				})
			}(t, wg, b, i, afi, label.UID.String())
		}
		wg.Wait()
		time.Sleep(100 * time.Millisecond)
	}
	afi.PrintMemUsage(t)
}
