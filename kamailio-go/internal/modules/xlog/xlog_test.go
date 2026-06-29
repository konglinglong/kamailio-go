// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the xlog module.
 */

package xlog

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLevelParsingAndNames(t *testing.T) {
	cases := []struct {
		name  string
		level LogLevel
	}{
		{"alert", LevelAlert},
		{"crit", LevelCrit},
		{"critical", LevelCrit},
		{"err", LevelError},
		{"error", LevelError},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"notice", LevelNotice},
		{"info", LevelInfo},
		{"dbg", LevelDebug},
		{"debug", LevelDebug},
	}
	for _, tc := range cases {
		if got := parseLevel(tc.name); got != tc.level {
			t.Errorf("parseLevel(%q) = %d, want %d", tc.name, got, tc.level)
		}
	}
	// Unknown level defaults to info.
	if got := parseLevel("bogus"); got != LevelInfo {
		t.Errorf("parseLevel(bogus) = %d, want %d (info)", got, LevelInfo)
	}
	// Case-insensitive.
	if got := parseLevel("INFO"); got != LevelInfo {
		t.Errorf("parseLevel(INFO) = %d, want %d", got, LevelInfo)
	}

	// levelName round-trips for known levels.
	for _, l := range []LogLevel{LevelAlert, LevelCrit, LevelError, LevelWarn, LevelNotice, LevelInfo, LevelDebug} {
		if name := levelName(l); name == "" {
			t.Errorf("levelName(%d) = empty", l)
		}
	}
}

func TestSetAndGetLogLevel(t *testing.T) {
	m := NewXLogModule()

	// Default level is info.
	if got := m.GetLogLevel(); got != "info" {
		t.Errorf("GetLogLevel() = %q, want info", got)
	}
	if got := m.GetLogLevelValue(); got != LevelInfo {
		t.Errorf("GetLogLevelValue() = %d, want %d", got, LevelInfo)
	}

	m.SetLogLevel("debug")
	if got := m.GetLogLevel(); got != "dbg" {
		t.Errorf("GetLogLevel() after debug = %q, want dbg", got)
	}

	m.SetLogLevelValue(LevelWarn)
	if got := m.GetLogLevelValue(); got != LevelWarn {
		t.Errorf("GetLogLevelValue() after warn = %d, want %d", got, LevelWarn)
	}
	if got := m.GetLogLevel(); got != "warn" {
		t.Errorf("GetLogLevel() after warn = %q, want warn", got)
	}
}

func TestLevelFiltering(t *testing.T) {
	m := NewXLogModule()
	// Discard output to keep the test quiet.
	var buf bytes.Buffer
	m.SetOutput(&buf)

	// Threshold = info: debug suppressed, info+ emitted.
	m.SetLogLevelValue(LevelInfo)
	if m.XDBG("debug-msg") {
		t.Error("XDBG returned true below threshold, want false")
	}
	if !m.XINFO("info-msg") {
		t.Error("XINFO returned false at threshold, want true")
	}
	if !m.XERR("err-msg") {
		t.Error("XERR returned false above threshold, want true")
	}

	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2 (info+err)", len(entries))
	}
	if entries[0].Message != "info-msg" {
		t.Errorf("entries[0].Message = %q, want info-msg", entries[0].Message)
	}
	if entries[1].LevelName != "err" {
		t.Errorf("entries[1].LevelName = %q, want err", entries[1].LevelName)
	}

	// Threshold = error: info suppressed, error emitted.
	m.Reset()
	m.SetLogLevelValue(LevelError)
	if m.XINFO("info-msg2") {
		t.Error("XINFO returned true below error threshold, want false")
	}
	if !m.XERR("err-msg2") {
		t.Error("XERR returned false at error threshold, want true")
	}
	if len(m.Entries()) != 1 {
		t.Errorf("len(Entries) after error threshold = %d, want 1", len(m.Entries()))
	}

	// Threshold = debug: everything emitted.
	m.Reset()
	m.SetLogLevelValue(LevelDebug)
	if !m.XDBG("d") || !m.XINFO("i") || !m.XERR("e") {
		t.Error("at debug threshold, all levels should be emitted")
	}
	if len(m.Entries()) != 3 {
		t.Errorf("len(Entries) at debug = %d, want 3", len(m.Entries()))
	}
}

func TestLevelHelpers(t *testing.T) {
	m := NewXLogModule()
	m.SetLogLevelValue(LevelDebug) // emit everything
	var buf bytes.Buffer
	m.SetOutput(&buf)

	m.XDBG("d")
	m.XINFO("i")
	m.XNOTICE("n")
	m.XWARN("w")
	m.XERR("e")
	m.XCRIT("c")
	m.XALERT("a")
	m.XLog("x") // default info

	entries := m.Entries()
	if len(entries) != 8 {
		t.Fatalf("len(Entries) = %d, want 8", len(entries))
	}
	want := []struct {
		name string
		msg  string
	}{
		{"dbg", "d"}, {"info", "i"}, {"notice", "n"}, {"warn", "w"},
		{"err", "e"}, {"crit", "c"}, {"alert", "a"}, {"info", "x"},
	}
	for i, w := range want {
		if entries[i].LevelName != w.name {
			t.Errorf("entries[%d].LevelName = %q, want %q", i, entries[i].LevelName, w.name)
		}
		if entries[i].Message != w.msg {
			t.Errorf("entries[%d].Message = %q, want %q", i, entries[i].Message, w.msg)
		}
	}

	// The Log(level, msg) helper parses the level name.
	m.Reset()
	if !m.Log("warn", "via-log") {
		t.Error("Log(warn, ...) returned false, want true")
	}
	last := m.LastEntry()
	if last == nil || last.LevelName != "warn" || last.Message != "via-log" {
		t.Errorf("LastEntry = %+v, want warn/via-log", last)
	}
}

func TestOutputAndPrefix(t *testing.T) {
	m := NewXLogModule()
	m.SetLogLevelValue(LevelInfo)
	var buf bytes.Buffer
	m.SetOutput(&buf)
	m.SetPrefix("PFX> ")

	m.XINFO("hello")

	out := buf.String()
	if !strings.HasPrefix(out, "PFX> ") {
		t.Errorf("output missing prefix: %q", out)
	}
	if !strings.Contains(out, "info: hello") {
		t.Errorf("output missing level/message: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output not newline-terminated: %q", out)
	}

	// nil output disables mirroring but entries are still captured.
	m.SetOutput(nil)
	m.Reset()
	m.XINFO("captured")
	if len(m.Entries()) != 1 {
		t.Errorf("entries not captured with nil output: %d", len(m.Entries()))
	}
}

func TestLastEntryAndReset(t *testing.T) {
	m := NewXLogModule()
	m.SetLogLevelValue(LevelDebug)
	m.SetOutput(nil)

	if m.LastEntry() != nil {
		t.Error("LastEntry() on fresh module = non-nil, want nil")
	}

	m.XINFO("first")
	m.XERR("second")
	last := m.LastEntry()
	if last == nil || last.Message != "second" {
		t.Errorf("LastEntry = %+v, want second", last)
	}

	m.Reset()
	if m.LastEntry() != nil {
		t.Error("LastEntry() after Reset = non-nil, want nil")
	}
	if len(m.Entries()) != 0 {
		t.Errorf("len(Entries) after Reset = %d, want 0", len(m.Entries()))
	}
}

func TestDefaultAndInit(t *testing.T) {
	d := DefaultXLog()
	if d == nil {
		t.Fatal("DefaultXLog() = nil")
	}
	// Default level is info before we change it.
	if got := d.GetLogLevel(); got != "info" {
		t.Errorf("GetLogLevel() = %q, want info", got)
	}
	// Quiet the default instance during the test.
	d.SetOutput(nil)
	d.SetLogLevelValue(LevelDebug)
	d.Reset()

	if !d.XINFO("default") {
		t.Error("DefaultXLog XINFO returned false, want true")
	}
	if got := d.GetLogLevel(); got != "dbg" {
		t.Errorf("GetLogLevel() after debug = %q, want dbg", got)
	}

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	d2 := DefaultXLog()
	d2.SetOutput(nil)
	if d2.LastEntry() != nil {
		t.Error("Init() did not reset the default instance")
	}
	if got := d2.GetLogLevel(); got != "info" {
		t.Errorf("GetLogLevel() after Init = %q, want info", got)
	}
}

func TestRingBufferCap(t *testing.T) {
	m := NewXLogModule()
	m.SetLogLevelValue(LevelDebug)
	m.SetOutput(nil)

	// Emit more than maxEntries; the ring should cap at maxEntries.
	for i := 0; i < maxEntries+50; i++ {
		m.XINFO("x")
	}
	if got := len(m.Entries()); got != maxEntries {
		t.Errorf("len(Entries) = %d, want %d (ring cap)", got, maxEntries)
	}
}

func TestConcurrentLogging(t *testing.T) {
	m := NewXLogModule()
	m.SetLogLevelValue(LevelDebug)
	var buf bytes.Buffer
	m.SetOutput(&buf)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.XINFO("concurrent")
			m.XDBG("dbg")
			_ = m.IsEnabled(LevelInfo)
			_ = m.Entries()
			_ = m.GetLogLevel()
		}(i)
	}
	wg.Wait()
}
