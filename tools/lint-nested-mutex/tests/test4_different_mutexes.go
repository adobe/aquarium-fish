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

// Author: Sergei Parshev (@sparshev)

/**
 * Test case 4: Different mutexes - should NOT cause errors
 * Functions use different mutexes, so no conflicts expected
 * Expected: 0 errors
 */

package tests

import "sync"

type MultiMutex struct {
	mu1  sync.RWMutex
	mu2  sync.RWMutex
	data string
}

// UsesFirstMutex holds lock on mu1 and calls UsesSecondMutex
func (m *MultiMutex) UsesFirstMutex() {
	m.mu1.Lock()
	defer m.mu1.Unlock()

	// This should NOT cause conflict since different mutex is used
	m.UsesSecondMutex()
}

// UsesSecondMutex uses a different mutex (mu2)
func (m *MultiMutex) UsesSecondMutex() {
	m.mu2.RLock()
	defer m.mu2.RUnlock()

	// Access data while holding different mutex
	_ = m.data
}
