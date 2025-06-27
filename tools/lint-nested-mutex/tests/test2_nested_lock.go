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
 * Test case 2: Nested Lock across functions
 * This should trigger: "Nested Lock detected across functions"
 * Expected: 1 error
 */

package tests

import "sync"

type DataManager struct {
	mutex sync.RWMutex
	count int
}

// WriteData holds Lock and calls InternalWrite
func (d *DataManager) WriteData(value int) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	// This call should trigger nested Lock detection
	d.InternalWrite(value)
}

// InternalWrite also uses Lock on the same mutex
func (d *DataManager) InternalWrite(value int) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.count = value
}
