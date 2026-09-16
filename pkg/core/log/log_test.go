// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package log

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func newBufferLogger(buf *bytes.Buffer) *Logger {
	return &Logger{level: LevelDebug, output: buf}
}

func restoreDefaultLogger(t *testing.T) {
	t.Helper()
	type loggerState struct {
		level      Level
		output     interface{ Write([]byte) (int, error) }
		prefix     string
		showCaller bool
		showTime   bool
		useColor   bool
	}

	defaultLogger.mu.Lock()
	snapshot := loggerState{
		level:      defaultLogger.level,
		output:     defaultLogger.output,
		prefix:     defaultLogger.prefix,
		showCaller: defaultLogger.showCaller,
		showTime:   defaultLogger.showTime,
		useColor:   defaultLogger.useColor,
	}
	defaultLogger.mu.Unlock()

	t.Cleanup(func() {
		defaultLogger.mu.Lock()
		defaultLogger.level = snapshot.level
		defaultLogger.output = snapshot.output
		defaultLogger.prefix = snapshot.prefix
		defaultLogger.showCaller = snapshot.showCaller
		defaultLogger.showTime = snapshot.showTime
		defaultLogger.useColor = snapshot.useColor
		defaultLogger.mu.Unlock()
	})
}

func TestLevelStringRepresentations(t *testing.T) {
	tests := []struct {
		level Level
		long  string
		short string
	}{
		{LevelDebug, "DEBUG", "DBG"},
		{LevelInfo, "INFO", "INF"},
		{LevelNotice, "NOTICE", "NOT"},
		{LevelWarn, "WARN", "WRN"},
		{LevelError, "ERROR", "ERR"},
		{LevelFatal, "FATAL", "FAT"},
		{Level(99), "LEVEL(99)", "???"},
		// Level is signed, so an out-of-range value can be negative; an
		// upper-bound-only guard indexes the name slice and panics.
		{Level(-1), "LEVEL(-1)", "???"},
	}

	for _, tt := range tests {
		t.Run(tt.long, func(t *testing.T) {
			if got := tt.level.String(); got != tt.long {
				t.Fatalf("String() = %q, want %q", got, tt.long)
			}
			if got := tt.level.ShortString(); got != tt.short {
				t.Fatalf("ShortString() = %q, want %q", got, tt.short)
			}
		})
	}
}

func TestParseLevelAcceptsAliasesAndRejectsUnknown(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"debug", LevelDebug},
		{"DBG", LevelDebug},
		{"info", LevelInfo},
		{"inf", LevelInfo},
		{"notice", LevelNotice},
		{"not", LevelNotice},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"wrn", LevelWarn},
		{"error", LevelError},
		{"err", LevelError},
		{"fatal", LevelFatal},
		{"fat", LevelFatal},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			if err != nil {
				t.Fatalf("ParseLevel returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

	got, err := ParseLevel("verbose")
	if err == nil {
		t.Fatal("ParseLevel accepted an unknown level")
	}
	if got != LevelInfo {
		t.Fatalf("unknown level fallback = %v, want %v", got, LevelInfo)
	}
	if !strings.Contains(err.Error(), "verbose") {
		t.Fatalf("unknown level error = %q, want input included", err.Error())
	}
}

func TestLogFiltersMessagesBelowConfiguredLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)
	logger.SetLevel(LevelWarn)

	logger.Debug("debug should stay quiet")
	logger.Info("info should stay quiet")
	logger.Notice("notice should stay quiet")
	if buf.Len() != 0 {
		t.Fatalf("messages below warn were emitted: %q", buf.String())
	}

	logger.Warn("disk %s", "hot")
	logger.Error("disk %s", "burning")
	got := buf.String()
	if !strings.Contains(got, "WRN disk hot\n") || !strings.Contains(got, "ERR disk burning\n") {
		t.Fatalf("warn/error output = %q", got)
	}
}

func TestLogFormattingIncludesTimePrefixCallerAndColorWhenEnabled(t *testing.T) {
	var buf bytes.Buffer
	logger := newBufferLogger(&buf)
	logger.SetPrefix("diam")
	logger.EnableCaller(true)
	logger.EnableColor(true)
	logger.showTime = true

	logger.Info("peer %d connected", 7)
	got := buf.String()
	pattern := `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} \x1b\[32mINF\x1b\[0m \[diam\] log_test\.go:\d+ peer 7 connected\n$`
	if !regexp.MustCompile(pattern).MatchString(got) {
		t.Fatalf("formatted output %q did not match %s", got, pattern)
	}
}

func TestLoggerOutputCanBeRedirected(t *testing.T) {
	var first bytes.Buffer
	var second bytes.Buffer
	logger := newBufferLogger(&first)
	logger.Info("first")
	logger.SetOutput(&second)
	logger.Info("second")

	if strings.Contains(first.String(), "second") {
		t.Fatalf("old writer received redirected message: %q", first.String())
	}
	if !strings.Contains(first.String(), "INF first\n") {
		t.Fatalf("first writer = %q", first.String())
	}
	if !strings.Contains(second.String(), "INF second\n") {
		t.Fatalf("second writer = %q", second.String())
	}
}

func TestPackageLevelLoggerUsesMutableDefaultState(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(LevelNotice)
	SetPrefix("default")
	EnableColor(false)
	EnableCaller(false)
	defaultLogger.showTime = false

	Debug("debug hidden")
	Info("info hidden")
	Notice("visible")
	Warn("careful")
	Error("bad")

	got := buf.String()
	if strings.Contains(got, "hidden") {
		t.Fatalf("default logger emitted filtered message: %q", got)
	}
	for _, want := range []string{"NOT [default] visible\n", "WRN [default] careful\n", "ERR [default] bad\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("default logger output %q missing %q", got, want)
		}
	}
}

func TestWithPrefixCopiesDefaultConfigurationAndUsesProvidedPrefix(t *testing.T) {
	restoreDefaultLogger(t)

	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(LevelError)
	EnableColor(false)
	EnableCaller(false)
	defaultLogger.showTime = false

	logger := WithPrefix("peer")
	logger.Warn("hidden")
	logger.Error("closed")

	if got := buf.String(); got != "ERR [peer] closed\n" {
		t.Fatalf("WithPrefix output = %q", got)
	}
}

func TestColorForLevelFallsBackForUnknownLevels(t *testing.T) {
	if got := colorForLevel(Level(42)); got != "" {
		t.Fatalf("unknown color = %q, want empty", got)
	}
}

// WithPrefix copies the default logger's fields, all of which are guarded by
// its mutex. Extensions build prefixed loggers while startup is still applying
// configuration, so the copy has to take the lock. Only -race fails this.
func TestWithPrefixIsSafeAgainstConcurrentReconfiguration(t *testing.T) {
	defaultLogger.mu.Lock()
	restore := defaultLogger.level
	defaultLogger.mu.Unlock()
	defer SetLevel(restore)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			SetLevel(LevelDebug)
			SetLevel(LevelError)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if l := WithPrefix("probe"); l == nil {
				t.Error("WithPrefix returned nil")
				return
			}
		}
	}()

	wg.Wait()
}
