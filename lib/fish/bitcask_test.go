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

// Keeping this test to make sure patching is working correctly until will be fixed upstream in #270

package fish

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.mills.io/bitcask/v2"
)

// Run multiple parallel writes & reads to ensure database is stable
func Test_bitcask_write_and_read(t *testing.T) {
	workspace := t.TempDir()
	defer os.RemoveAll(workspace)

	// Setting WithMaxDatafileSize to increase the probability of failure
	db, err := bitcask.Open(filepath.Join(workspace, "bitcask.db"), bitcask.WithMaxDatafileSize(1024))
	if err != nil {
		t.Fatalf("Unable to start the bitcask database")
		return
	}
	defer db.Close()

	for i := 0; i < 10; i++ {
		go func(t *testing.T, id int) {
			defer t.Log(fmt.Sprintf("Reader %03d stopped", id))
			t.Log(fmt.Sprintf("Reader %03d started", id))

			var as []string
			for {
				err = db.Collection("application").List(&as)

				//t.Log(fmt.Sprintf("Reader %03d Apps amount:", id), len(as))
			}
		}(t, i)
	}

	// Flooding the db with 100 batches of 200 parallel object stores
	for b := 0; b < 10; b++ {
		// Spin up 200 of threads to create Application and look what will happen
		wg := &sync.WaitGroup{}
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func(t *testing.T, wg *sync.WaitGroup, batch, id int) {
				defer wg.Done()

				t.Run(fmt.Sprintf("%03d-%04d Create Application", batch, id), func(t *testing.T) {
					k := fmt.Sprintf("%03d-%04d", batch, id)
					v := fmt.Sprintf("TEST %03d-%04d Test", batch, id)
					err := db.Collection("application").Add(k, v)

					if err != nil {
						t.Errorf("Failed to set Application: %v", err)
					}
				})
			}(t, wg, b, i)
		}
		wg.Wait()
	}
}
