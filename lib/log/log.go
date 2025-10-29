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

// Package log provides structured logging with OpenTelemetry integration for Fish executable
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/bridges/otelslog"
)

type Level = slog.Level

const (
	LevelDebug Level = slog.LevelDebug
	LevelInfo  Level = slog.LevelInfo
	LevelWarn  Level = slog.LevelWarn
	LevelError Level = slog.LevelError
)

var levels = []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}

// Global logger instance
var (
	loggerMu sync.RWMutex
	logger   *slog.Logger

	// OpenTelemetry integration
	otelHandler *otelslog.Handler
)

// Initialize with default configuration on package load
func init() {
	// Initialize with default config
	_ = Initialize(DefaultConfig())
}

// Configuration
type Config struct {
	Level        string `json:"level"`         // Log level (debug, info, warn, error)
	Format       string `json:"format"`        // Output format (console, json)
	UseTimestamp bool   `json:"use_timestamp"` // Include timestamp in logs
	UseColor     bool   `json:"use_color"`     // Use colors in console output
	UseModule    bool   `json:"use_module"`    // Include module information
	UseCaller    bool   `json:"use_caller"`    // Include caller information
	OtelEnabled  bool   `json:"otel_enabled"`  // Enable OpenTelemetry integration
}

// DefaultConfig returns default logging configuration
func DefaultConfig() *Config {
	return &Config{
		Level:        "info",
		Format:       "console",
		UseTimestamp: true,
		UseColor:     true,
		UseModule:    true,
		UseCaller:    false,
		OtelEnabled:  false,
	}
}

// parseLevel converts string level to slog.Level
func parseLevel(levelStr string) (Level, error) {
	switch strings.ToLower(levelStr) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level %q", levelStr)
	}
}

// Initialize sets up the global logger with the given configuration
func Initialize(config *Config) error {
	// Parse log level
	level, err := parseLevel(config.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", config.Level, err)
	}

	// Configure output
	var output io.Writer = os.Stdout

	// Create handler options
	opts := &slog.HandlerOptions{
		Level: level,
	}

	// Add caller information if requested
	if config.UseCaller {
		opts.AddSource = true
		// Customize caller format
		_, projectDir, _, ok := runtime.Caller(0)
		projectDir = filepath.Dir(filepath.Dir(filepath.Dir(projectDir)))
		if !ok {
			return fmt.Errorf("unable to determine project root directory")
		}
		opts.ReplaceAttr = func(_ /*groups*/ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				if source, ok := a.Value.Any().(*slog.Source); ok {
					relPath, _ := filepath.Rel(projectDir, source.File)
					return slog.String(slog.SourceKey, relPath+":"+strconv.Itoa(source.Line))
				}
			}
			return a
		}
	}

	// Create handler
	var handler slog.Handler
	if config.Format == "console" {
		consoleHandler := NewConsoleHandler(output, opts)
		// Color for now is set automatically depends on the stdout is PTY or not
		// consoleHandler.SetUseColor(config.UseColor)
		handler = consoleHandler
	} else {
		handler = slog.NewJSONHandler(output, opts)
	}

	// Create logger
	log := slog.New(handler)

	// Set global logger
	loggerMu.Lock()
	logger = log
	loggerMu.Unlock()

	// Set up OpenTelemetry integration if enabled
	if config.OtelEnabled {
		if err := SetupOtelIntegration(); err != nil {
			return fmt.Errorf("unable to setup otel for logging: %w", err)
		}
	}

	return nil
}

// SetupOtelIntegration sets up OpenTelemetry integration for logging
// This is called from the monitoring package when OpenTelemetry is initialized
func SetupOtelIntegration() error {
	loggerMu.Lock()
	defer loggerMu.Unlock()

	if otelHandler == nil {
		// Create OpenTelemetry handler
		otelHandler = otelslog.NewHandler("aquarium-fish")

		// Create a multi-handler that combines console/JSON output with OpenTelemetry
		multiHandler := &multiHandler{
			handlers: []slog.Handler{logger.Handler(), otelHandler},
		}

		// Update the global logger
		logger = slog.New(multiHandler)
	}
	return nil
}

// multiHandler combines multiple slog.Handler implementations
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var lastErr error
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, r); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		newHandlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// GetLevel returns current logging level as string
func GetLevel() Level {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	for _, lvl := range levels {
		level := logger.Handler().Enabled(context.Background(), lvl)
		if level {
			return lvl
		}
	}
	return LevelError
}

// WithFunc provides a way to identify package and function executed
// Empty values in the params are not allowed
func WithFunc(pack, fun string) *slog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	if pack == "" || fun == "" {
		return nil
		// It's possible to figure out the package/name automatically - but too much overhead
		/*pc, _, _, ok := runtime.Caller(1)
		if ok {
			f := runtime.FuncForPC(pc)
			if f != nil {
				funcPath := f.Name()
				lastDot := strings.LastIndexByte(funcPath, '.')
				if lastDot != -1 {
					if pack == "" {
						pack = funcPath[:lastDot]
						if lastSlash := strings.LastIndexByte(pack, '/'); lastSlash != -1 {
							pack = pack[lastSlash+1:]
						}
						if firstDot := strings.IndexByte(pack, '.'); firstDot != -1 {
							pack = pack[:firstDot]
						}
					}
					if fun == "" {
						fun = funcPath[lastDot+1:]
					}
				}
			}
		}*/
	}
	return logger.With("pack", pack, "func", fun).WithGroup(pack)
}
