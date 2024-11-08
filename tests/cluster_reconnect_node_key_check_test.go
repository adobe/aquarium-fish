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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steinfletcher/apitest"

	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Testing that the once connected node will store the node and then cluster verifies it's cert pubkey is ok
// * Create 3 nodes in a cluster
// * Disconnect 3rd node
// * Clear it's key and cert to force to recreate on start
// * Run the third node and see how it fails to start due to join issue
// * Null the node-3 Node record pubkey on node-2
// * Run the third node again with join aimed to second node and it should be fine now.
func Test_cluster_reconnect_node_key_check(t *testing.T) {
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
node_location: test_loc-2

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Big cluster node
	afi3 := afi2.NewClusterNode(t, "node-3", `---
node_location: test_loc-3

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 4
      ram_limit: 8`)

	t.Cleanup(func() {
		afi1.Cleanup(t)
		afi2.Cleanup(t)
		afi3.Cleanup(t)
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

	t.Run("Stopping node-3 to refresh the node key", func(t *testing.T) {
		afi3.Stop(t)
		if err := os.Remove(filepath.Join(afi3.Workspace(), "fish_data", "node-3.key")); err != nil {
			t.Fatalf("Unable to remove the node-3.key: %v", err)
		}
		if err := os.Remove(filepath.Join(afi3.Workspace(), "fish_data", "node-3.crt")); err != nil {
			t.Fatalf("Unable to remove the node-3.crt: %v", err)
		}
	})

	t.Run("Expecting failure of starting node-3 after removing the key", func(t *testing.T) {
		h.ExpectFailure(t, func(tt testing.TB) {
			afi3.Start(tt)
		})
	})

	t.Run("Removing node-2 pubkey from the cluster", func(t *testing.T) {
		apitest.New().
			EnableNetworking(cli).
			Put(afi2.APIAddress("api/v1/node/node-3/pubkey")).
			BasicAuth("admin", afi1.AdminToken()).
			Expect(t).
			Status(http.StatusOK).
			End()
	})

	t.Run("Now node-3 should startup just fine", func(t *testing.T) {
		afi3.Start(t)
	})
}
