// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

// Package log provides logging for the Diameter stack.
// This corresponds to fd_log from the original freeDiameter.
package log

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents the log level.
type Level int

const (
	// LevelDebug is for detailed debug information
	LevelDebug Level = iota
	// LevelInfo is for general information
	LevelInfo
	// LevelNotice is for significant events
	LevelNotice
	// LevelWarn is for warning conditions
	LevelWarn
	// LevelError is for error conditions
	LevelError
	// LevelFatal is for fatal errors
	LevelFatal
)

// String returns the string representation of a log level.
func (l Level) String() string {
	names := []string{"DEBUG", "INFO", "NOTICE", "WARN", "ERROR", "FATAL"}
	if int(l) < len(names) {
		return names[l]
	}
	return fmt.Sprintf("LEVEL(%d)", l)
}

// ShortString returns a short representation for log output.
func (l Level) ShortString() string {
	names := []string{"DBG", "INF", "NOT", "WRN", "ERR", "FAT"}
	if int(l) < len(names) {
		return names[l]
	}
	return "???"
}

// Logger provides logging functionality.
type Logger struct {
	mu     sync.Mutex
	level  Level
	output io.Writer
	prefix string

	// Optional formatting
	showCaller bool
	showTime   bool
	useColor   bool
}

// Flags for log formatting
const (
	FlagTime   = 1 << iota // Include timestamp
	FlagCaller             // Include caller info
	FlagColor              // Use ANSI colors
)

// Default logger
var defaultLogger = &Logger{
	level:    LevelInfo,
	output:   os.Stderr,
	showTime: true,
	useColor: true,
}

// SetLevel sets the log level for the default logger.
func SetLevel(level Level) {
	defaultLogger.SetLevel(level)
}

// SetOutput sets the output for the default logger.
func SetOutput(w io.Writer) {
	defaultLogger.SetOutput(w)
}

// SetPrefix sets the prefix for the default logger.
func SetPrefix(prefix string) {
	defaultLogger.SetPrefix(prefix)
}

// EnableColor enables or disables color output.
func EnableColor(enable bool) {
	defaultLogger.EnableColor(enable)
}

// EnableCaller enables or disables caller info in logs.
func EnableCaller(enable bool) {
	defaultLogger.EnableCaller(enable)
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput sets the log output destination.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

// SetPrefix sets the log prefix.
func (l *Logger) SetPrefix(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prefix = prefix
}

// EnableColor enables or disables color output.
func (l *Logger) EnableColor(enable bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.useColor = enable
}

// EnableCaller enables or disables caller info.
func (l *Logger) EnableCaller(enable bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.showCaller = enable
}

// Log logs a message at the given level.
func (l *Logger) Log(level Level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	var sb strings.Builder

	// Timestamp
	if l.showTime {
		sb.WriteString(time.Now().Format("2006-01-02 15:04:05.000"))
		sb.WriteString(" ")
	}

	// Level with optional color
	levelStr := level.ShortString()
	if l.useColor {
		sb.WriteString(colorForLevel(level))
		sb.WriteString(levelStr)
		sb.WriteString("\033[0m")
	} else {
		sb.WriteString(levelStr)
	}
	sb.WriteString(" ")

	// Prefix
	if l.prefix != "" {
		sb.WriteString("[")
		sb.WriteString(l.prefix)
		sb.WriteString("] ")
	}

	// Caller info
	if l.showCaller {
		_, file, line, ok := runtime.Caller(2)
		if ok {
			// Get just the filename
			if idx := strings.LastIndex(file, "/"); idx >= 0 {
				file = file[idx+1:]
			}
			fmt.Fprintf(&sb, "%s:%d ", file, line)
		}
	}

	// Message
	if len(args) > 0 {
		fmt.Fprintf(&sb, format, args...)
	} else {
		sb.WriteString(format)
	}
	sb.WriteString("\n")

	_, _ = l.output.Write([]byte(sb.String()))
}

func colorForLevel(level Level) string {
	switch level {
	case LevelDebug:
		return "\033[36m" // Cyan
	case LevelInfo:
		return "\033[32m" // Green
	case LevelNotice:
		return "\033[34m" // Blue
	case LevelWarn:
		return "\033[33m" // Yellow
	case LevelError:
		return "\033[31m" // Red
	case LevelFatal:
		return "\033[35m" // Magenta
	default:
		return ""
	}
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.Log(LevelDebug, format, args...)
}

// Info logs an info message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.Log(LevelInfo, format, args...)
}

// Notice logs a notice message.
func (l *Logger) Notice(format string, args ...interface{}) {
	l.Log(LevelNotice, format, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.Log(LevelWarn, format, args...)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.Log(LevelError, format, args...)
}

// Fatal logs a fatal message and exits.
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.Log(LevelFatal, format, args...)
	os.Exit(1)
}

// Package level functions using the default logger

// Debug logs a debug message.
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

// Info logs an info message.
func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

// Notice logs a notice message.
func Notice(format string, args ...interface{}) {
	defaultLogger.Notice(format, args...)
}

// Warn logs a warning message.
func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

// Error logs an error message.
func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

// Fatal logs a fatal message and exits.
func Fatal(format string, args ...interface{}) {
	defaultLogger.Fatal(format, args...)
}

// WithPrefix returns a new logger with the given prefix.
func WithPrefix(prefix string) *Logger {
	return &Logger{
		level:      defaultLogger.level,
		output:     defaultLogger.output,
		prefix:     prefix,
		showTime:   defaultLogger.showTime,
		showCaller: defaultLogger.showCaller,
		useColor:   defaultLogger.useColor,
	}
}

// ParseLevel parses a log level string.
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(s) {
	case "DEBUG", "DBG":
		return LevelDebug, nil
	case "INFO", "INF":
		return LevelInfo, nil
	case "NOTICE", "NOT":
		return LevelNotice, nil
	case "WARN", "WARNING", "WRN":
		return LevelWarn, nil
	case "ERROR", "ERR":
		return LevelError, nil
	case "FATAL", "FAT":
		return LevelFatal, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level: %s", s)
	}
}
