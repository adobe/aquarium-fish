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
 * Test case 1: Nested RLock across functions
 * This should trigger: "Nested RLock detected across functions"
 * Expected: 1 error
 */

package tests

import "sync"

type TestStruct struct {
	mu   sync.RWMutex
	data string
}

// ParentFunction holds RLock and calls ChildFunction
func (t *TestStruct) ParentFunction() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// This call should trigger nested RLock detection
	return t.ChildFunction()
}

// ChildFunction also uses RLock on the same mutex
func (t *TestStruct) ChildFunction() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.data
}
