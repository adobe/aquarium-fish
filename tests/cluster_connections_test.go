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
	"testing"
	"time"

	"github.com/steinfletcher/apitest"

	"github.com/adobe/aquarium-fish/lib/openapi/types"
	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Testing connections for the node when auto management is disabled
// * Allocate 8 nodes cluster in one location
// * Check each node have 1 active connection (auto manage client disabled)
func Test_cluster_connections_noauto(t *testing.T) {
	// Small cluster node
	afi1 := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi2 := afi1.NewClusterNode(t, "node-2", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi3 := afi2.NewClusterNode(t, "node-3", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi4 := afi3.NewClusterNode(t, "node-4", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi5 := afi4.NewClusterNode(t, "node-5", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi6 := afi5.NewClusterNode(t, "node-6", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi7 := afi6.NewClusterNode(t, "node-7", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi8 := afi6.NewClusterNode(t, "node-8", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

cluster_auto: false

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	t.Cleanup(func() {
		afi1.Cleanup(t)
		afi2.Cleanup(t)
		afi3.Cleanup(t)
		afi4.Cleanup(t)
		afi5.Cleanup(t)
		afi6.Cleanup(t)
		afi7.Cleanup(t)
		afi8.Cleanup(t)
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

	var conns []types.Connection
	t.Run("Connections of node-1 should stay 1 in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi1.APIAddress("api/v1/node/this/connections")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&conns)

			if len(conns) != 1 {
				r.Fatalf("Amount of node-1 connections is not 1: %d (%q)", len(conns), conns)
			}
		})
	})
}

// TODO: Testing max connections for the node is working but it's still possible to connect new nodes
// * Allocate 8 nodes cluster in one location
// * Check each node have 8 active connection
// * Add 9th node to the cluster
// * Add remote location node
// * Add second remote location node
/*func Test_cluster_max_connections_noauto(t *testing.T) {
	// Small cluster node
	afi1 := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi2 := afi1.NewClusterNode(t, "node-2", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi3 := afi2.NewClusterNode(t, "node-3", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi4 := afi3.NewClusterNode(t, "node-4", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi5 := afi4.NewClusterNode(t, "node-5", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi6 := afi5.NewClusterNode(t, "node-6", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi7 := afi6.NewClusterNode(t, "node-7", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Small cluster node
	afi8 := afi6.NewClusterNode(t, "node-8", `---
node_location: test_loc-1

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	t.Cleanup(func() {
		afi1.Cleanup(t)
		afi2.Cleanup(t)
		afi3.Cleanup(t)
		afi4.Cleanup(t)
		afi5.Cleanup(t)
		afi6.Cleanup(t)
		afi7.Cleanup(t)
		afi8.Cleanup(t)
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

	var conns []types.Connection
	t.Run("Connections of node-1 should become 8 in 10 sec", func(t *testing.T) {
		h.Retry(&h.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}, t, func(r *h.R) {
			apitest.New().
				EnableNetworking(cli).
				Get(afi1.APIAddress("api/v1/node/this/connections")).
				BasicAuth("admin", afi1.AdminToken()).
				Expect(r).
				Status(http.StatusOK).
				End().
				JSON(&conns)

			if len(conns) != 8 {
				r.Fatalf("Amount of node-1 connections is not 8: %d (%q)", len(conns), conns)
			}
		})
	})
}*/
