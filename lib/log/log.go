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

package log

import (
	"fmt"
	"log"
	"os"
)

var (
	UseTimestamp bool = true
	Verbosity    int8 = 2

	DebugLogger *log.Logger
	InfoLogger  *log.Logger
	WarnLogger  *log.Logger
	ErrorLogger *log.Logger
)

func SetVerbosity(level string) error {
	switch level {
	case "debug":
		Verbosity = 1
	case "info":
		Verbosity = 2
	case "warn":
		Verbosity = 3
	case "error":
		Verbosity = 4
	default:
		return fmt.Errorf("Unable to parse verbosity level: %s", level)
	}

	return nil
}

func InitLoggers() error {
	flags := log.Lmsgprefix

	// Showing short file for debug verbosity
	if Verbosity < 2 {
		flags |= log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile
	} else if UseTimestamp {
		flags |= log.Ldate | log.Ltime
	}

	DebugLogger = log.New(os.Stdout, "DEBUG:\t", flags)
	InfoLogger = log.New(os.Stdout, "INFO:\t", flags)
	WarnLogger = log.New(os.Stdout, "WARN:\t", flags)
	ErrorLogger = log.New(os.Stdout, "ERROR:\t", flags)

	return nil
}

func GetInfoLogger() *log.Logger {
	return InfoLogger
}

func Debug(v ...any) {
	if Verbosity <= 1 {
		DebugLogger.Output(2, fmt.Sprintln(v...))
	}
}

func Debugf(format string, v ...any) {
	if Verbosity <= 1 {
		DebugLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}

func Info(v ...any) {
	if Verbosity <= 2 {
		InfoLogger.Output(2, fmt.Sprintln(v...))
	}
}

func Infof(format string, v ...any) {
	if Verbosity <= 2 {
		InfoLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}

func Warn(v ...any) {
	if Verbosity <= 3 {
		WarnLogger.Output(2, fmt.Sprintln(v...))
	}
}

func Warnf(format string, v ...any) {
	if Verbosity <= 3 {
		WarnLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
}

func Error(v ...any) error {
	msg := fmt.Sprintln(v...)
	if Verbosity <= 4 {
		ErrorLogger.Output(2, msg)
	}
	return fmt.Errorf("%s", msg)
}

func Errorf(format string, v ...any) error {
	if Verbosity <= 4 {
		ErrorLogger.Output(2, fmt.Sprintf(format+"\n", v...))
	}
	return fmt.Errorf(format, v...)
}
