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
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestConsoleHandler_BasicFormat(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	// Test basic message
	logger.Info("test message")
	output := buf.String()

	// Check timestamp format
	if !strings.Contains(output, "[") || !strings.Contains(output, "]") {
		t.Errorf("Expected timestamp in brackets, got: %s", output)
	}

	// Check level format
	if !strings.Contains(output, "INF") {
		t.Errorf("Expected INF level, got: %s", output)
	}

	// Check message
	if !strings.Contains(output, "test message") {
		t.Errorf("Expected 'test message', got: %s", output)
	}
}

func TestConsoleHandler_DebugTimestamp(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	logger.Debug("debug message")
	output := buf.String()

	// Debug level should include milliseconds
	if !strings.Contains(output, ".") {
		t.Errorf("Debug timestamp should include milliseconds, got: %s", output)
	}
}

func TestConsoleHandler_InfoTimestamp(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	logger.Info("info message")
	output := buf.String()

	// Info level should not include milliseconds
	if strings.Contains(output, ".") && strings.Contains(output, "INF") {
		t.Errorf("Info timestamp should not include milliseconds, got: %s", output)
	}
}

func TestConsoleHandler_Attributes(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	logger.Info("test", "key1", "value1", "key2", 42)
	output := buf.String()

	// Check attributes are present
	if !strings.Contains(output, "key1=value1") {
		t.Errorf("Expected 'key1=value1', got: %s", output)
	}
	if !strings.Contains(output, "key2=42") {
		t.Errorf("Expected 'key2=42', got: %s", output)
	}
}

func TestConsoleHandler_PackFunc(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	logger.Info("test", "pack", "mypackage", "func", "myfunction")
	output := buf.String()

	// Check pack.func format
	if !strings.Contains(output, "mypackage.myfunction") {
		t.Errorf("Expected 'mypackage.myfunction', got: %s", output)
	}
}

func TestConsoleHandler_Groups(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	// Test with groups
	logger = logger.WithGroup("user").WithGroup("profile")
	logger.Info("test", "name", "john", "age", 30)
	output := buf.String()

	// Check group prefixes
	if !strings.Contains(output, "user.profile.name=john") {
		t.Errorf("Expected 'user.profile.name=john', got: %s", output)
	}
	if !strings.Contains(output, "user.profile.age=30") {
		t.Errorf("Expected 'user.profile.age=30', got: %s", output)
	}
}

func TestConsoleHandler_GroupAttributes(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	// Test group attributes
	logger.Info("test", slog.Group("user", "name", "john", "age", 30))
	output := buf.String()

	// Check group attributes are flattened with group name prefix
	if !strings.Contains(output, "user.name=john") {
		t.Errorf("Expected 'user.name=john', got: %s", output)
	}
	if !strings.Contains(output, "user.age=30") {
		t.Errorf("Expected 'user.age=30', got: %s", output)
	}
}

func TestConsoleHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	// Add attributes to logger
	logger = logger.With("service", "test", "version", "1.0")
	logger.Info("message", "key", "value")
	output := buf.String()

	// Check that With attributes are included
	if !strings.Contains(output, "service=test") {
		t.Errorf("Expected 'service=test', got: %s", output)
	}
	if !strings.Contains(output, "version=1.0") {
		t.Errorf("Expected 'version=1.0', got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected 'key=value', got: %s", output)
	}
}

func TestConsoleHandler_Levels(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)

	// Test all levels
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 4 {
		t.Errorf("Expected 4 lines, got %d", len(lines))
	}

	// Check level abbreviations
	if !strings.Contains(lines[0], "DBG") {
		t.Errorf("Expected DBG level, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "INF") {
		t.Errorf("Expected INF level, got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "WRN") {
		t.Errorf("Expected WRN level, got: %s", lines[2])
	}
	if !strings.Contains(lines[3], "ERR") {
		t.Errorf("Expected ERR level, got: %s", lines[3])
	}
}

func TestConsoleHandler_ColorDisabled(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler.SetUseColor(false)
	logger := slog.New(handler)

	logger.Info("test message")
	output := buf.String()

	// Check no ANSI color codes are present
	if strings.Contains(output, "\033[") {
		t.Errorf("Expected no color codes, got: %s", output)
	}
}

func TestConsoleHandler_ReplaceAttr(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "secret" {
				return slog.Attr{Key: "secret", Value: slog.StringValue("***")}
			}
			return a
		},
	})
	logger := slog.New(handler)

	logger.Info("test", "secret", "password123", "public", "data")
	output := buf.String()

	// Check that secret is replaced
	if !strings.Contains(output, "secret=***") {
		t.Errorf("Expected 'secret=***', got: %s", output)
	}
	if !strings.Contains(output, "public=data") {
		t.Errorf("Expected 'public=data', got: %s", output)
	}
}

func TestConsoleHandler_ComplexGroups(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	// Test nested groups
	logger = logger.WithGroup("request").WithGroup("user")
	logger.Info("test",
		slog.Group("profile", "name", "john", "age", 30),
		"status", "active",
	)
	output := buf.String()

	// Check nested group structure
	if !strings.Contains(output, "request.user.profile.name=john") {
		t.Errorf("Expected 'request.user.profile.name=john', got: %s", output)
	}
	if !strings.Contains(output, "request.user.profile.age=30") {
		t.Errorf("Expected 'request.user.profile.age=30', got: %s", output)
	}
	if !strings.Contains(output, "request.user.status=active") {
		t.Errorf("Expected 'request.user.status=active', got: %s", output)
	}
}

func TestConsoleHandler_Enabled(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})

	// Test that debug and info are disabled
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Debug should be disabled when level is Warn")
	}
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Info should be disabled when level is Warn")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("Warn should be enabled when level is Warn")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("Error should be enabled when level is Warn")
	}
}

func TestConsoleHandler_EmptyGroups(t *testing.T) {
	var buf bytes.Buffer
	handler := NewConsoleHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)

	// Test with empty group name
	logger = logger.WithGroup("")
	logger.Info("test", "key", "value")
	output := buf.String()

	// Should not add empty group prefix
	if strings.Contains(output, ".key=value") {
		t.Errorf("Should not have empty group prefix, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected 'key=value', got: %s", output)
	}
}
