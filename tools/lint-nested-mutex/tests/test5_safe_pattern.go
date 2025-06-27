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
 * Test case 5: Safe pattern - no mutex conflicts
 * This should NOT trigger any errors (function calls happen outside lock scope)
 * Expected: 0 errors
 */

package tests

import "sync"

type SafeCounter struct {
	mu    sync.RWMutex
	value int
}

// SafeIncrement calls helper function BEFORE acquiring lock
func (s *SafeCounter) SafeIncrement() {
	// Call helper function before locking - this is safe
	newValue := s.CalculateNewValue()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = newValue
}

// CalculateNewValue reads value safely and returns new value
func (s *SafeCounter) CalculateNewValue() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.value + 1
}
