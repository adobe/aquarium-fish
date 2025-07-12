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

package log

import (
	"testing"
)

func Test_console_handler_color(t *testing.T) {
	// Test with default config (console format)
	config := DefaultConfig()
	config.UseColor = true
	config.Level = "debug"

	err := Initialize(config)
	if err != nil {
		panic(err)
	}

	logger := WithFunc("main", "main")

	// Test different log levels
	logger.Debug("This is a debug message", "key", "value", "number", 42)
	logger.Info("This is an info message", "user", "test 2", "active", true)
	logger.Warn("This is a warning message", "error_count", 3)
	logger.Error("This is an error message", "stack_trace", "simulated")
}

func Test_console_handler_nocolor(t *testing.T) {
	// Test without pack/func
	config2 := DefaultConfig()
	config2.UseColor = false
	config2.Level = "info"

	err := Initialize(config2)
	if err != nil {
		panic(err)
	}

	logger2 := WithFunc("test", "function")
	logger2.Info("Testing without colors", "format", "plain")
}
