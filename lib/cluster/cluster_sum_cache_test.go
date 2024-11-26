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

package cluster

import (
	"testing"
	"time"
)

func Test_cluster_sum_cache_match(t *testing.T) {
	sc := newSumCache(time.Second*10, time.Second*30)

	t.Run("Putting new ids should be ok", func(t *testing.T) {
		if ok := sc.Put(1234); !ok {
			t.Fatalf(`Put(1234) = %v, want: %v`, ok, true)
		}
		if ok := sc.Put(1235); !ok {
			t.Fatalf(`Put(1235) = %v, want: %v`, ok, true)
		}
		if ok := sc.Put(1236); !ok {
			t.Fatalf(`Put(1236) = %v, want: %v`, ok, true)
		}
	})

	t.Run("Putting existing ids should be not ok", func(t *testing.T) {
		if ok := sc.Put(1234); ok {
			t.Fatalf(`Put(1234) = %v, want: %v`, ok, false)
		}
		if ok := sc.Put(1235); ok {
			t.Fatalf(`Put(1235) = %v, want: %v`, ok, false)
		}
		if ok := sc.Put(1236); ok {
			t.Fatalf(`Put(1236) = %v, want: %v`, ok, false)
		}
	})

	t.Run("Putting new id should be ok", func(t *testing.T) {
		if ok := sc.Put(4321); !ok {
			t.Fatalf(`Put(4321) = %v, want: %v`, ok, true)
		}
	})
}

func Test_cluster_sum_cache_delete(t *testing.T) {
	sc := newSumCache(time.Second*10, time.Second*30)

	t.Run("Putting new id should be ok", func(t *testing.T) {
		if ok := sc.Put(1234); !ok {
			t.Fatalf(`Put(1234) = %v, want: %v`, ok, true)
		}
	})

	t.Run("Deliting existing id should be ok", func(t *testing.T) {
		sc.Delete(1234)
	})

	t.Run("Adding the same id after delete should be ok", func(t *testing.T) {
		if ok := sc.Put(1234); !ok {
			t.Fatalf(`Put(1234) = %v, want: %v`, ok, true)
		}
	})

	t.Run("Delete unknown id should not panic", func(t *testing.T) {
		sc.Delete(1235)
	})
}

func Test_cluster_sum_cache_cleanup(t *testing.T) {
	sc := newSumCache(200*time.Millisecond, 100*time.Millisecond)

	t.Run("Putting new id should be ok", func(t *testing.T) {
		if ok := sc.Put(1234); !ok {
			t.Fatalf(`Put(1234) = %v, want: %v`, ok, true)
		}
	})

	time.Sleep(2 * time.Second)

	t.Run("In 3 seconds the id should be cleaned up", func(t *testing.T) {
		if ok := sc.Put(1234); !ok {
			t.Fatalf(`Put(1234) = %v, want: %v`, ok, true)
		}
	})
}
