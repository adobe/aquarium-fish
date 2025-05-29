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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Check the database compaction works correctly in constant flow of applications
func Test_compactdb_check(t *testing.T) {
	t.Parallel()
	afi := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc
default_resource_lifetime: 20s

api_address: 127.0.0.1:0

db_cleanup_interval: 10s
db_compact_interval: 5s

drivers:
  gates: {}
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

	// No ERROR could happen during execution of this test
	afi.WaitForLog("ERROR:", func(substring, line string) bool {
		t.Errorf("Error located in the Fish log: %q", line)
		return true
	})

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

	completed := false
	workerFunc := func(t *testing.T, wg *sync.WaitGroup, id int, afi *h.AFInstance, cli *http.Client) {
		t.Logf("Worker %d: Started", id)
		defer t.Logf("Worker %d: Ended", id)
		defer wg.Done()

		for !completed {
			var app types.Application
			var appState types.ApplicationState

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
				t.Errorf("Worker %d: Application UID is incorrect: %v", id, app.UID)
				return
			}

			// Checking state until it's allocated
			for appState.Status != types.ApplicationStatusALLOCATED {
				apitest.New().
					EnableNetworking(cli).
					Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/state")).
					BasicAuth("admin", afi.AdminToken()).
					Expect(t).
					Status(http.StatusOK).
					End().
					JSON(&appState)

				if appState.UID == uuid.Nil {
					t.Errorf("Worker %d: ApplicationStatus UID is incorrect: %v", id, appState.UID)
					return
				}
				if appState.Status == types.ApplicationStatusERROR {
					t.Errorf("Worker %d: ApplicationStatus is incorrect: %v", id, appState.Status)
					return
				}

				time.Sleep(time.Second)
			}

			// Time to deallocate
			apitest.New().
				EnableNetworking(cli).
				Get(afi.APIAddress("api/v1/application/"+app.UID.String()+"/deallocate")).
				BasicAuth("admin", afi.AdminToken()).
				Expect(t).
				Status(http.StatusOK).
				End()
		}
	}

	// Run multiple application create/terminate routines to keep DB busy during the processes
	wg := &sync.WaitGroup{}
	for id := range 10 {
		wg.Add(1)
		go workerFunc(t, wg, id, afi, cli)
		time.Sleep(50 * time.Millisecond)
	}

	t.Run("Applications should be cleaned from DB and compacted", func(t *testing.T) {
		// Wait for the next 20 cleanupdb completed to have enough time to fill the DB
		cleaned := make(chan struct{})
		for range 20 {
			afi.WaitForLog("Fish: CleanupDB completed", func(substring, line string) bool {
				cleaned <- struct{}{}
				return true
			})
			<-cleaned
		}

		// Now stopping the workers to calm down a bit and wait for a few more cleanups
		completed = true
		for range 3 {
			afi.WaitForLog("Fish: CleanupDB completed", func(substring, line string) bool {
				cleaned <- struct{}{}
				return true
			})
			<-cleaned
		}

		compacted := make(chan error)
		afi.WaitForLog("DB: CompactDB: After compaction: ", func(substring, line string) bool {
			// Check the Keys get back to normal
			spl := strings.Split(line, ", ")
			for _, val := range spl {
				if !strings.Contains(val, "Keys: ") {
					continue
				}
				spl = strings.Split(val, ": ")
				// Database should have just 3 keys left: user/admin, label/UID and node/node-1
				if spl[1] != "3" {
					t.Errorf("Wrong amount of keys left in the database: %s != 3", spl[1])
					break
				}
			}
			if spl[0] != "Keys" {
				t.Errorf("Unable to locate database compaction result for Keys: %s", spl[0])
			}
			compacted <- nil
			return true
		})
		// Stopping the node to trigger CompactDB process the last time
		afi.Stop(t)

		<-compacted
	})
}
