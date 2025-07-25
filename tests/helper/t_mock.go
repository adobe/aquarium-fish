/**
 * Copyright 2023-2025 Adobe. All rights reserved.
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

package helper

import (
	"runtime"
	"sync"
	"testing"
)

// MockT is useful to capture the failed test
type MockT struct {
	testing.T

	FailNowCalled bool

	t *testing.T
}

// FailNow when it's the right time
func (m *MockT) FailNow() {
	m.FailNowCalled = true
	runtime.Goexit()
}

// Log message
func (m *MockT) Log(args ...any) {
	m.t.Log(args...)
}

// Logf message
func (m *MockT) Logf(format string, args ...any) {
	m.t.Logf(format, args...)
}

// Fatal message
func (m *MockT) Fatal(args ...any) {
	m.t.Log(args...)
	m.FailNow()
}

// Fatalf message
func (m *MockT) Fatalf(format string, args ...any) {
	m.t.Logf(format, args...)
	m.FailNow()
}

// xpectFailure when failure expected
func ExpectFailure(t *testing.T, f func(tt testing.TB)) {
	t.Helper()
	var wg sync.WaitGroup
	mockT := &MockT{t: t}

	wg.Add(1)
	go func() {
		defer wg.Done()
		f(mockT)
	}()
	wg.Wait()

	if !mockT.FailNowCalled {
		t.Fatalf("ExpectFailure: the function did not fail as expected")
	}
}
