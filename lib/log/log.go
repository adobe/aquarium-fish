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
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/trace"
)

// Global logger instance
var (
	loggerMu   sync.RWMutex
	zeroLogger zerolog.Logger

	// OpenTelemetry integration
	otelLogger otellog.Logger
)

// Initialize with default configuration on package load
func init() {
	// Initialize with default config
	_ = Initialize(DefaultConfig())
}

// otelHook implements zerolog.Hook to forward logs to OpenTelemetry
type otelHook struct {
	mu       sync.RWMutex
	logger   otellog.Logger
	enabled  bool
	minLevel zerolog.Level
}

// newOtelHook creates a new OpenTelemetry hook for zerolog
func newOtelHook(logger otellog.Logger, minLevel zerolog.Level) *otelHook {
	return &otelHook{
		logger:   logger,
		enabled:  true,
		minLevel: minLevel,
	}
}

// Run implements zerolog.Hook
func (h *otelHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	if !h.isEnabled() || level < h.minLevel {
		return
	}

	// Create OpenTelemetry log record
	var record otellog.Record
	record.SetTimestamp(time.Now())
	record.SetBody(otellog.StringValue(msg))
	record.SetSeverity(mapZerologLevelToOtel(level))
	record.SetSeverityText(level.String())

	// Extract context from the event if available
	ctx := context.Background()

	// Check if we have trace context from the current goroutine
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		record.AddAttributes(
			otellog.String("trace_id", span.SpanContext().TraceID().String()),
			otellog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}

	// Add structured fields from zerolog event
	// This is a simplified approach - in a full implementation you'd want to
	// extract all the fields from the zerolog event
	record.AddAttributes(
		otellog.String("level", level.String()),
		otellog.String("logger", "aquarium-fish"),
	)

	// Emit the log record
	h.mu.RLock()
	logger := h.logger
	h.mu.RUnlock()

	if logger != nil {
		logger.Emit(ctx, record)
	}
}

// Enable/disable the hook
func (h *otelHook) isEnabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

func (h *otelHook) SetEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
}

// mapZerologLevelToOtel maps zerolog levels to OpenTelemetry severity levels
func mapZerologLevelToOtel(level zerolog.Level) otellog.Severity {
	switch level {
	case zerolog.TraceLevel:
		return otellog.SeverityTrace
	case zerolog.DebugLevel:
		return otellog.SeverityDebug
	case zerolog.InfoLevel:
		return otellog.SeverityInfo
	case zerolog.WarnLevel:
		return otellog.SeverityWarn
	case zerolog.ErrorLevel:
		return otellog.SeverityError
	case zerolog.FatalLevel:
		return otellog.SeverityFatal
	case zerolog.PanicLevel:
		return otellog.SeverityFatal
	default:
		return otellog.SeverityInfo
	}
}

// Configuration
type Config struct {
	Level        string `json:"level"`          // Log level (trace, debug, info, warn, error, fatal, panic)
	Format       string `json:"format"`         // Output format (console, json)
	UseTimestamp bool   `json:"use_timestamp"`  // Include timestamp in logs
	UseColor     bool   `json:"use_color"`      // Use colors in console output
	UseCaller    bool   `json:"use_caller"`     // Include caller information
	OtelEnabled  bool   `json:"otel_enabled"`   // Enable OpenTelemetry integration
	OtelMinLevel string `json:"otel_min_level"` // Minimum level for OpenTelemetry logs
}

// DefaultConfig returns default logging configuration
func DefaultConfig() *Config {
	return &Config{
		Level:        "info",
		Format:       "console",
		UseTimestamp: true,
		UseColor:     true,
		UseCaller:    false,
		OtelEnabled:  false,
		OtelMinLevel: "info",
	}
}

// Initialize sets up the global logger with the given configuration
func Initialize(config *Config) error {
	// Parse log level
	level, err := zerolog.ParseLevel(config.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", config.Level, err)
	}

	// Set global log level
	zerolog.SetGlobalLevel(level)

	// Configure output
	var output io.Writer = os.Stdout

	if config.Format == "console" {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
			NoColor:    !config.UseColor,
		}
		if !config.UseTimestamp {
			consoleWriter.TimeFormat = ""
		}
		output = consoleWriter
	}

	// Create logger
	logger := zerolog.New(output)

	// Add timestamp if requested
	if config.UseTimestamp {
		logger = logger.With().Timestamp().Logger()
	}

	// Add caller if requested
	if config.UseCaller {
		logger = logger.With().Caller().Logger()
	}

	// Set global logger
	loggerMu.Lock()
	zeroLogger = logger
	loggerMu.Unlock()

	// Set up OpenTelemetry integration if enabled
	if config.OtelEnabled {
		if err := SetupOtelIntegration(config.Level); err != nil {
			return fmt.Errorf("unable to setup otel for logging: %w", err)
		}
	}

	return nil
}

// SetupOtelIntegration sets up OpenTelemetry integration for logging
// This is called from the monitoring package when OpenTelemetry is initialized
func SetupOtelIntegration(minLevel string) error {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	if otelLogger == nil {
		otelMinLevel, err := zerolog.ParseLevel(minLevel)
		if err != nil {
			return fmt.Errorf("invalid otel min level %q: %w", minLevel, err)
		}

		otelLogger = global.GetLoggerProvider().Logger("aquarium-fish")

		// Add OpenTelemetry hook
		hook := newOtelHook(otelLogger, otelMinLevel)
		zeroLogger = zeroLogger.Hook(hook)
	}
	return nil
}

// Context-aware logging functions
func WithContext(ctx context.Context) *zerolog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger := zeroLogger
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		logger = logger.With().
			Str("trace_id", span.SpanContext().TraceID().String()).
			Str("span_id", span.SpanContext().SpanID().String()).
			Logger()
	}
	return &logger
}

func WithFields(fields map[string]any) *zerolog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger := zeroLogger.With()
	for k, v := range fields {
		logger = logger.Interface(k, v)
	}
	l := logger.Logger()
	return &l
}

func WithField(key string, value any) *zerolog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger := zeroLogger.With().Interface(key, value).Logger()
	return &logger
}

func WithError(err error) *zerolog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger := zeroLogger.With().Err(err).Logger()
	return &logger
}

// Convenience functions for common log levels
func Trace() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Trace()
}

func Debug() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Debug()
}

func Info() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Info()
}

func Warn() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Warn()
}

func Error() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Error()
}

func Fatal() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Fatal()
}

func Panic() *zerolog.Event {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return zeroLogger.Panic()
}

// Structured logging with context
func TraceCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Trace()
}

func DebugCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Debug()
}

func InfoCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Info()
}

func WarnCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Warn()
}

func ErrorCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Error()
}

func FatalCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Fatal()
}

func PanicCtx(ctx context.Context) *zerolog.Event {
	return WithContext(ctx).Panic()
}

// GetLevel returns current logging level as string
func GetLevel() string {
	return zeroLogger.GetLevel().String()
}
