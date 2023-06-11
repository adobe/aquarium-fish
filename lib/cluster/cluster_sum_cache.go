/**
 * Copyright 2023 Adobe. All rights reserved.
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
	"sync"
	"time"
)

// Sum cache is used to identify the duplicated messages in the cluster & client on receive
type sumCache struct {
	// When the cache item will expire to be automatically cleaned
	timeout int64

	// Map with uint32 id's and timeouts
	mu    sync.Mutex
	cache map[uint32]int64

	// Needed to stop the cleanup when needed
	wg   sync.WaitGroup
	stop chan struct{}
}

// Creates the new cache with required timeout for the new items and cleanup interval
func newSumCache(item_timeout, cleanup_interval time.Duration) *sumCache {
	sc := &sumCache{
		timeout: int64(item_timeout.Seconds()),
		cache:   make(map[uint32]int64),
		stop:    make(chan struct{}),
	}

	sc.wg.Add(1)
	go func(ci time.Duration) {
		defer sc.wg.Done()
		sc.cleanupLoop(ci)
	}(cleanup_interval)

	return sc
}

func (sc *sumCache) stopCleanup() {
	close(sc.stop)
	sc.wg.Wait()
}

// Will try to put the new item if it doesn't exist and return true, otherwise false
func (sc *sumCache) Put(id uint32) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Check if the id already here
	if _, ok := sc.cache[id]; ok {
		return false
	}

	// Put the id into the map
	sc.cache[id] = time.Now().Unix() + sc.timeout
	return true
}

// In case some operation failed - it could be useful to remove the previously added item
func (sc *sumCache) Delete(id uint32) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	delete(sc.cache, id)
}

func (sc *sumCache) cleanupLoop(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-sc.stop:
			return
		case <-t.C:
			sc.mu.Lock()
			for id, timeout := range sc.cache {
				if timeout <= time.Now().Unix() {
					delete(sc.cache, id)
				}
			}
			sc.mu.Unlock()
		}
	}
}
