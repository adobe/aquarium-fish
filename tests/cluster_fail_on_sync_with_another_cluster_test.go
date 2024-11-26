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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	h "github.com/adobe/aquarium-fish/tests/helper"
)

// Testing the failure if someone want to connect one cluster with another cluster
// * Creating 2 independent nodes which initiates their own clusters
// * Stopping node-2, cleaning it's ca & node keys & certs, copying ca from node-1
// * Starting node-2 with argument to join node-1 and expect it to fail
func Test_cluster_fail_on_sync_with_another_cluster(t *testing.T) {
	// First cluster node
	afi1 := h.NewAquariumFish(t, "node-1", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	// Another cluster node
	afi2 := h.NewAquariumFish(t, "node-2", `---
node_location: test_loc

api_address: 127.0.0.1:0

drivers:
  - name: test
    cfg:
      cpu_limit: 2
      ram_limit: 4`)

	t.Cleanup(func() {
		afi1.Cleanup(t)
		afi2.Cleanup(t)
	})

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	t.Run("Stopping node-2 to refresh the node key", func(t *testing.T) {
		afi2.Stop(t)
		if err := os.Remove(filepath.Join(afi2.Workspace(), "fish_data", "node-2.key")); err != nil {
			t.Fatalf("Unable to remove the node-2.key: %v", err)
		}
		if err := os.Remove(filepath.Join(afi2.Workspace(), "fish_data", "node-2.crt")); err != nil {
			t.Fatalf("Unable to remove the node-2.crt: %v", err)
		}
		if err := os.Remove(filepath.Join(afi2.Workspace(), "fish_data", "ca.key")); err != nil {
			t.Fatalf("Unable to remove the ca.key: %v", err)
		}
		if err := os.Remove(filepath.Join(afi2.Workspace(), "fish_data", "ca.crt")); err != nil {
			t.Fatalf("Unable to remove the ca.crt: %v", err)
		}
		// Copy seed node CA to generate valid cluster node cert
		if err := h.CopyFile(filepath.Join(afi1.Workspace(), "fish_data", "ca.key"), filepath.Join(afi2.Workspace(), "fish_data", "ca.key")); err != nil {
			t.Fatalf("Unable to copy CA key: %v", err)
		}
		if err := h.CopyFile(filepath.Join(afi1.Workspace(), "fish_data", "ca.crt"), filepath.Join(afi2.Workspace(), "fish_data", "ca.crt")); err != nil {
			t.Fatalf("Unable to copy CA crt: %v", err)
		}
	})

	t.Run("Expecting failure of starting node-2 after trying to connect conflicting cluster", func(t *testing.T) {
		h.ExpectFailure(t, func(tt testing.TB) {
			afi2.Start(tt, "--join", afi1.Endpoint())
		})
	})
}
