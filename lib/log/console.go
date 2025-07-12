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

package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// ANSI color codes
const (
	ColorReset  = "\033[0m"
	ColorGray   = "\033[90m"
	ColorRed    = "\033[91m"
	ColorYellow = "\033[93m"
	ColorBlue   = "\033[94m"
	ColorCyan   = "\033[96m"
	ColorWhite  = "\033[97m"
	ColorDim    = "\033[2m"
)

// ConsoleHandler is a custom slog.Handler that formats logs for console output
type ConsoleHandler struct {
	opts   *slog.HandlerOptions
	writer io.Writer
	mu     sync.Mutex

	// Color settings
	useColor     bool
	autoColor    bool
	isDebugLevel bool

	// Attribute processing
	attrs  []slog.Attr
	groups []string
}

// NewConsoleHandler creates a new ConsoleHandler
func NewConsoleHandler(w io.Writer, opts *slog.HandlerOptions) *ConsoleHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}

	// Auto-detect color support
	autoColor := isTerminal(w)

	isDebug := opts.Level != nil && opts.Level.Level() <= slog.LevelDebug

	return &ConsoleHandler{
		opts:         opts,
		writer:       w,
		useColor:     autoColor,
		autoColor:    autoColor,
		isDebugLevel: isDebug,
	}
}

// isTerminal checks if the writer is a terminal (PTY)
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// SetUseColor enables or disables color output
func (h *ConsoleHandler) SetUseColor(useColor bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.useColor = useColor
}

// Enabled reports whether the handler handles records at the given level
func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

// Handle handles the Record
func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var buf strings.Builder

	// Format timestamp
	timestamp := r.Time.Format("060102/150405-07")
	if h.isDebugLevel {
		timestamp = r.Time.Format("060102/150405.000-07")
	}

	// Format level (3 chars)
	level := formatLevel(r.Level)

	// Extract pack and func from attributes
	pack, fun := h.extractPackFunc(r)

	// Apply colors if enabled
	if h.useColor {
		buf.WriteString(h.colorize(ColorGray, fmt.Sprintf("[%s]", timestamp)))
		buf.WriteString(" ")
		buf.WriteString(h.colorizeLevel(r.Level, level))
		buf.WriteString(" ")
		buf.WriteString(h.colorizeLevel(r.Level, r.Message))
		if pack != "" && fun != "" {
			buf.WriteString(" ")
			buf.WriteString(h.colorize(ColorDim, fmt.Sprintf("%s.%s", pack, fun)))
		}
	} else {
		buf.WriteString(fmt.Sprintf("[%s] %s %s", timestamp, level, r.Message))
		if pack != "" && fun != "" {
			buf.WriteString(fmt.Sprintf(" %s.%s", pack, fun))
		}
	}

	// Add attributes
	h.appendAttrs(&buf, r)

	buf.WriteString("\n")

	_, err := h.writer.Write([]byte(buf.String()))
	return err
}

// extractPackFunc extracts pack and func from record attributes
func (h *ConsoleHandler) extractPackFunc(r slog.Record) (string, string) {
	var pack, fun string

	// Check handler attributes first
	for _, attr := range h.attrs {
		switch attr.Key {
		case "pack":
			pack = attr.Value.String()
		case "func":
			fun = attr.Value.String()
		}
	}

	// Then check record attributes
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "pack":
			pack = a.Value.String()
		case "func":
			fun = a.Value.String()
		}
		return true
	})

	return pack, fun
}

// appendAttrs adds attributes to the buffer
func (h *ConsoleHandler) appendAttrs(buf *strings.Builder, r slog.Record) {
	// Add handler attributes (excluding pack and func)
	for _, attr := range h.attrs {
		if attr.Key != "pack" && attr.Key != "func" {
			h.appendAttr(buf, attr)
		}
	}

	// Add record attributes (excluding pack and func)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key != "pack" && a.Key != "func" {
			h.appendAttr(buf, a)
		}
		return true
	})
}

// appendAttr adds a single attribute to the buffer
func (h *ConsoleHandler) appendAttr(buf *strings.Builder, attr slog.Attr) {
	// Apply ReplaceAttr if configured
	if h.opts.ReplaceAttr != nil {
		attr = h.opts.ReplaceAttr(h.groups, attr)
		if attr.Key == "" {
			return
		}
	}

	buf.WriteString(" ")
	buf.WriteString(attr.Key)
	buf.WriteString("=")

	// Format value
	switch attr.Value.Kind() {
	case slog.KindString:
		buf.WriteString(attr.Value.String())
	case slog.KindInt64:
		buf.WriteString(fmt.Sprintf("%d", attr.Value.Int64()))
	case slog.KindUint64:
		buf.WriteString(fmt.Sprintf("%d", attr.Value.Uint64()))
	case slog.KindFloat64:
		buf.WriteString(fmt.Sprintf("%g", attr.Value.Float64()))
	case slog.KindBool:
		buf.WriteString(fmt.Sprintf("%t", attr.Value.Bool()))
	case slog.KindTime:
		buf.WriteString(attr.Value.Time().Format(time.RFC3339))
	case slog.KindDuration:
		buf.WriteString(attr.Value.Duration().String())
	case slog.KindAny, slog.KindGroup, slog.KindLogValuer:
		buf.WriteString(fmt.Sprintf("%v", attr.Value.Any()))
	default:
		buf.WriteString(fmt.Sprintf("%v", attr.Value.Any()))
	}
}

// formatLevel formats the log level as a 3-character string
func formatLevel(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "DBG"
	case slog.LevelInfo:
		return "INF"
	case slog.LevelWarn:
		return "WRN"
	case slog.LevelError:
		return "ERR"
	default:
		return "???"
	}
}

// colorize applies color to text if colors are enabled
func (h *ConsoleHandler) colorize(color, text string) string {
	if !h.useColor {
		return text
	}
	return color + text + ColorReset
}

// colorizeLevel applies level-specific colors
func (h *ConsoleHandler) colorizeLevel(level slog.Level, text string) string {
	if !h.useColor {
		return text
	}

	var color string
	switch level {
	case slog.LevelDebug:
		color = ColorCyan
	case slog.LevelInfo:
		color = ColorBlue
	case slog.LevelWarn:
		color = ColorYellow
	case slog.LevelError:
		color = ColorRed
	default:
		color = ColorWhite
	}

	return color + text + ColorReset
}

// WithAttrs returns a new ConsoleHandler with the given attributes
func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)

	return &ConsoleHandler{
		opts:         h.opts,
		writer:       h.writer,
		useColor:     h.useColor,
		autoColor:    h.autoColor,
		isDebugLevel: h.isDebugLevel,
		attrs:        newAttrs,
		groups:       h.groups,
	}
}

// WithGroup returns a new ConsoleHandler with the given group
func (h *ConsoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name

	return &ConsoleHandler{
		opts:         h.opts,
		writer:       h.writer,
		useColor:     h.useColor,
		autoColor:    h.autoColor,
		isDebugLevel: h.isDebugLevel,
		attrs:        h.attrs,
		groups:       newGroups,
	}
}
