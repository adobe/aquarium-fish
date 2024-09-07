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

// Package log provides logging for Fish executable
package log

import (
	"fmt"
	"log"
	"os"
)

var (
	// UseTimestamp needed if you don't want to output timestamp in the logging message
	// for example that's helpful in case your service journal already contains timestamps
	UseTimestamp = true

	verbosity int8 = 2

	debugLogger *log.Logger
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
)

func init() {
	// Init default loggers
	InitLoggers()
}

// SetVerbosity defines verbosity of the logger
func SetVerbosity(level string) error {
	switch level {
	case "debug":
		verbosity = 1
	case "info":
		verbosity = 2
	case "warn":
		verbosity = 3
	case "error":
		verbosity = 4
	default:
		return fmt.Errorf("Unable to parse verbosity level: %s", level)
	}

	return nil
}

// GetVerbosity returns current verbosity level
func GetVerbosity() int8 {
	return verbosity
}

// InitLoggers initializes the loggers
func InitLoggers() error {
	flags := log.Lmsgprefix

	// Skip timestamp if not needed
	if UseTimestamp {
		flags |= log.Ldate | log.Ltime
		if verbosity < 2 {
			flags |= log.Lmicroseconds
		}
	}
	// Show short file for debug verbosity
	if verbosity < 2 {
		flags |= log.Lshortfile
	}

	debugLogger = log.New(os.Stdout, "DEBUG:\t", flags)
	infoLogger = log.New(os.Stdout, "INFO:\t", flags)
	warnLogger = log.New(os.Stdout, "WARN:\t", flags)
	errorLogger = log.New(os.Stdout, "ERROR:\t", flags)

	return nil
}

// GetInfoLogger returns Info logger
func GetInfoLogger() *log.Logger {
	return infoLogger
}

// GetErrorLogger returns Error logger
func GetErrorLogger() *log.Logger {
	return errorLogger
}

// Debug logs debug message
func Debug(v ...any) {
	if verbosity <= 1 {
		debugLogger.Output(2, fmt.Sprintln(v...))
	}
}

// Debugf logs debug message with formatting
func Debugf(format string, v ...any) {
	if verbosity <= 1 {
		debugLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}

// Info logs info message
func Info(v ...any) {
	if verbosity <= 2 {
		infoLogger.Output(2, fmt.Sprintln(v...))
	}
}

// Infof logs info message with formatting
func Infof(format string, v ...any) {
	if verbosity <= 2 {
		infoLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}

// Warn logs warning message
func Warn(v ...any) {
	if verbosity <= 3 {
		warnLogger.Output(2, fmt.Sprintln(v...))
	}
}

// Warnf logs warning message with formatting
func Warnf(format string, v ...any) {
	if verbosity <= 3 {
		warnLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}

// Error logs error message
func Error(v ...any) error {
	msg := fmt.Sprintln(v...)
	if verbosity <= 4 {
		errorLogger.Output(2, msg)
	}
	return fmt.Errorf("%s", msg)
}

// Errorf logs error message with formatting
func Errorf(format string, v ...any) error {
	if verbosity <= 4 {
		errorLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
	return fmt.Errorf(format, v...)
}
