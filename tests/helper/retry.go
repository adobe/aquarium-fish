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

package helper

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// Failer is an interface compatible with testing.T.
type Failer interface {
	Helper()

	// Log is called for the final test output
	Log(args ...any)

	// FailNow is called when the retrying is abandoned.
	FailNow()
}

// R provides context for the retryer.
type R struct {
	fail   bool
	done   bool
	output []string
}

// Helper shows this struct as helper
func (*R) Helper() {}

var runFailed = struct{}{}

// FailNow will fail the retry
func (r *R) FailNow() {
	r.fail = true
	panic(runFailed)
}

// Fatal fail and log
func (r *R) Fatal(args ...any) {
	r.log(fmt.Sprint(args...))
	r.FailNow()
}

// Fatalf fail and log
func (r *R) Fatalf(format string, args ...any) {
	r.log(fmt.Sprintf(format, args...))
	r.FailNow()
}

// Error log error
func (r *R) Error(args ...any) {
	r.log(fmt.Sprint(args...))
	r.fail = true
}

// Errorf log error
func (r *R) Errorf(format string, args ...any) {
	r.log(fmt.Sprintf(format, args...))
	r.fail = true
}

// Check check if everything is ok
func (r *R) Check(err error) {
	if err != nil {
		r.log(err.Error())
		r.FailNow()
	}
}

func (r *R) log(s string) {
	r.output = append(r.output, decorate(s))
}

// Stop retrying, and fail the test with the specified error.
func (r *R) Stop(err error) {
	r.log(err.Error())
	r.done = true
}

func decorate(s string) string {
	_, file, line, ok := runtime.Caller(3)
	if ok {
		n := strings.LastIndex(file, "/")
		if n >= 0 {
			file = file[n+1:]
		}
	} else {
		file = "???"
		line = 1
	}
	return fmt.Sprintf("%s:%d: %s", file, line, s)
}

// Retry again
func Retry(r Retryer, t Failer, f func(r *R)) {
	t.Helper()
	run(r, t, f)
}

func dedup(a []string) string {
	if len(a) == 0 {
		return ""
	}
	seen := map[string]struct{}{}
	var b bytes.Buffer
	for _, s := range a {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		b.WriteString(s)
		b.WriteRune('\n')
	}
	return b.String()
}

func run(r Retryer, t Failer, f func(r *R)) {
	t.Helper()
	rr := &R{}

	fail := func() {
		t.Helper()
		out := dedup(rr.output)
		if out != "" {
			t.Log(out)
		}
		t.FailNow()
	}

	for r.Continue() {
		func() {
			defer func() {
				if p := recover(); p != nil && p != runFailed {
					panic(p)
				}
			}()
			f(rr)
		}()

		switch {
		case rr.done:
			fail()
			return
		case !rr.fail:
			return
		}
		rr.fail = false
	}
	fail()
}

// Retryer provides an interface for repeating operations
// until they succeed or an exit condition is met.
type Retryer interface {
	// Continue returns true if the operation should be repeated, otherwise it
	// returns false to indicate retrying should stop.
	Continue() bool
}

// Counter repeats an operation a given number of
// times and waits between subsequent operations.
type Counter struct {
	Count int
	Wait  time.Duration

	intCount int
}

// Continue counter
func (r *Counter) Continue() bool {
	if r.intCount == r.Count {
		return false
	}
	if r.intCount > 0 {
		time.Sleep(r.Wait)
	}
	r.intCount++
	return true
}

// Timer repeats an operation for a given amount
// of time and waits between subsequent operations.
type Timer struct {
	Timeout time.Duration
	Wait    time.Duration

	// stop is the timeout deadline.
	// Set on the first invocation of Next().
	stop time.Time
}

// Continue the timer
func (r *Timer) Continue() bool {
	if r.stop.IsZero() {
		r.stop = time.Now().Add(r.Timeout)
		return true
	}
	if time.Now().After(r.stop) {
		return false
	}
	time.Sleep(r.Wait)
	return true
}
