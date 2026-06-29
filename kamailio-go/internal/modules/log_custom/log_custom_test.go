// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the LogCustom module.
 */

package log_custom

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// readLog returns the contents of the log file at path.
func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log %s: %v", path, err)
	}
	return string(b)
}

func TestLogWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	m := New()
	m.Init(path)
	defer m.Close()

	// Default level is info; debug messages are filtered out.
	m.Log("debug", "should be skipped")
	m.Log("info", "hello world")
	m.Log("err", "boom")
	m.Close()

	content := readLog(t, path)
	if strings.Contains(content, "should be skipped") {
		t.Errorf("debug line should not be written at info level:\n%s", content)
	}
	if !strings.Contains(content, "info: hello world") {
		t.Errorf("info line missing:\n%s", content)
	}
	if !strings.Contains(content, "err: boom") {
		t.Errorf("err line missing:\n%s", content)
	}
}

func TestLevelFiltering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	m := New()
	m.Init(path)

	// Threshold err: only err/crit/alert are emitted.
	m.SetLevel("err")
	if got := m.GetLevel(); got != "err" {
		t.Fatalf("GetLevel() = %q, want err", got)
	}
	m.Log("debug", "dbg-msg")
	m.Log("info", "info-msg")
	m.Log("notice", "notice-msg")
	m.Log("warn", "warn-msg")
	m.Log("err", "err-msg")
	m.Log("crit", "crit-msg")
	m.Close()

	content := readLog(t, path)
	for _, skipped := range []string{"dbg-msg", "info-msg", "notice-msg", "warn-msg"} {
		if strings.Contains(content, skipped) {
			t.Errorf("%q should be filtered at err level:\n%s", skipped, content)
		}
	}
	for _, emitted := range []string{"err: err-msg", "crit: crit-msg"} {
		if !strings.Contains(content, emitted) {
			t.Errorf("%q should be emitted at err level:\n%s", emitted, content)
		}
	}
}

func TestSetGetLevel(t *testing.T) {
	m := New()
	for _, name := range []string{"alert", "crit", "err", "warn", "notice", "info", "debug"} {
		m.SetLevel(name)
		if got := m.GetLevel(); got != name {
			t.Errorf("GetLevel() after SetLevel(%q) = %q, want %q", name, got, name)
		}
	}
	// Aliases.
	m.SetLevel("error")
	if got := m.GetLevel(); got != "err" {
		t.Errorf("GetLevel() after SetLevel(error) = %q, want err", got)
	}
	m.SetLevel("warning")
	if got := m.GetLevel(); got != "warn" {
		t.Errorf("GetLevel() after SetLevel(warning) = %q, want warn", got)
	}
	m.SetLevel("dbg")
	if got := m.GetLevel(); got != "debug" {
		t.Errorf("GetLevel() after SetLevel(dbg) = %q, want debug", got)
	}
	// Unknown -> info.
	m.SetLevel("nonsense")
	if got := m.GetLevel(); got != "info" {
		t.Errorf("GetLevel() after SetLevel(nonsense) = %q, want info", got)
	}
}

func TestConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	m := New()
	m.Init(path)
	defer m.Close()
	m.SetLevel("debug")

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			m.Log("info", "msg-"+itoa(i))
			m.SetLevel("info")
			_ = m.GetLevel()
		}(i)
	}
	wg.Wait()
	m.Close()

	content := readLog(t, path)
	for i := 0; i < goroutines; i++ {
		if !strings.Contains(content, "msg-"+itoa(i)) {
			t.Errorf("missing log line for msg-%d", i)
		}
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d := DefaultLogCustom()
	if d == nil {
		t.Fatal("DefaultLogCustom() = nil")
	}
	if d != DefaultLogCustom() {
		t.Fatal("DefaultLogCustom() returned different instances")
	}
	if got := d.GetLevel(); got != "info" {
		t.Errorf("GetLevel() = %q, want info", got)
	}
	// Default starts with no file: Log is a no-op.
	d.Log("info", "no-file")
	// Re-init resets.
	path := filepath.Join(t.TempDir(), "log.txt")
	d.Init(path)
	d.Log("info", "default-msg")
	d.Close()
	if !strings.Contains(readLog(t, path), "default-msg") {
		t.Errorf("default log line missing")
	}
	// Package Init() resets to unconfigured.
	Init()
	if DefaultLogCustom().GetLevel() != "info" {
		t.Errorf("GetLevel after re-Init should be info")
	}
}

// itoa is a tiny local int->string helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
