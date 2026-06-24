// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Debugger module tests - script tracing and breakpoints.
 */
package debugger

import (
	"sync"
	"testing"
)

func TestEnableDisable(t *testing.T) {
	m := NewDebuggerModule()
	if m.IsEnabled() {
		t.Error("expected disabled by default")
	}
	m.Enable()
	if !m.IsEnabled() {
		t.Error("expected enabled after Enable")
	}
	m.Disable()
	if m.IsEnabled() {
		t.Error("expected disabled after Disable")
	}
}

func TestLogAction(t *testing.T) {
	m := NewDebuggerModule()
	// When disabled, LogAction is a no-op.
	m.LogAction("route", "MAIN", 1, "msg1")
	if m.Count() != 0 {
		t.Errorf("Count when disabled = %d, want 0", m.Count())
	}

	m.Enable()
	m.LogAction("route", "MAIN", 1, "msg1")
	m.LogAction("if", "MAIN", 2, "msg2")
	entries := m.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Action != "route" || entries[0].Route != "MAIN" || entries[0].Line != 1 || entries[0].Msg != "msg1" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Line != 2 {
		t.Errorf("entries[1].Line = %d, want 2", entries[1].Line)
	}
	if entries[0].Time.IsZero() {
		t.Error("expected non-zero Time")
	}
}

func TestClearLog(t *testing.T) {
	m := NewDebuggerModule()
	m.Enable()
	m.LogAction("a", "r", 1, "m")
	m.LogAction("b", "r", 2, "m")
	if m.Count() != 2 {
		t.Fatalf("Count = %d, want 2", m.Count())
	}
	m.ClearLog()
	if m.Count() != 0 {
		t.Errorf("Count after ClearLog = %d, want 0", m.Count())
	}
	if got := m.GetEntries(); len(got) != 0 {
		t.Errorf("len(GetEntries) after ClearLog = %d, want 0", len(got))
	}
}

func TestBreakpoints(t *testing.T) {
	m := NewDebuggerModule()
	m.SetBreakpoint("MAIN", 10)
	m.SetBreakpoint("FAILURE", 5)
	m.SetBreakpoint("MAIN", 10) // idempotent

	if !m.HasBreakpoint("MAIN", 10) {
		t.Error("expected breakpoint at MAIN:10")
	}
	if m.HasBreakpoint("MAIN", 11) {
		t.Error("did not expect breakpoint at MAIN:11")
	}
	if m.BreakpointCount() != 2 {
		t.Errorf("BreakpointCount = %d, want 2", m.BreakpointCount())
	}

	bps := m.ListBreakpoints()
	if len(bps) != 2 {
		t.Errorf("len(ListBreakpoints) = %d, want 2", len(bps))
	}

	if !m.RemoveBreakpoint("MAIN", 10) {
		t.Error("RemoveBreakpoint returned false for existing breakpoint")
	}
	if m.RemoveBreakpoint("MAIN", 10) {
		t.Error("RemoveBreakpoint returned true for already-removed breakpoint")
	}
	if m.HasBreakpoint("MAIN", 10) {
		t.Error("expected breakpoint removed")
	}
	if m.BreakpointCount() != 1 {
		t.Errorf("BreakpointCount after remove = %d, want 1", m.BreakpointCount())
	}
}

func TestSetConfig(t *testing.T) {
	m := NewDebuggerModule()
	cfg := DebugConfig{Enabled: true, LogLevel: 3, LogMask: 0xFF, StepMode: true}
	m.SetConfig(cfg)
	if !m.IsEnabled() {
		t.Error("expected enabled after SetConfig")
	}
	got := m.GetConfig()
	if got.LogLevel != 3 || got.LogMask != 0xFF || !got.StepMode {
		t.Errorf("GetConfig = %+v", got)
	}
}

func TestCount(t *testing.T) {
	m := NewDebuggerModule()
	m.Enable()
	for i := 0; i < 5; i++ {
		m.LogAction("a", "r", i, "m")
	}
	if m.Count() != 5 {
		t.Errorf("Count = %d, want 5", m.Count())
	}
}

func TestConcurrentLogAction(t *testing.T) {
	m := NewDebuggerModule()
	m.Enable()
	const goroutines = 50
	const perG = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.LogAction("a", "r", j, "m")
			}
		}()
	}
	wg.Wait()
	want := goroutines * perG
	if m.Count() != want {
		t.Errorf("Count = %d, want %d", m.Count(), want)
	}
}

func TestConcurrentBreakpoints(t *testing.T) {
	m := NewDebuggerModule()
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			m.SetBreakpoint("R", n)
		}(i)
		go func(n int) {
			defer wg.Done()
			m.HasBreakpoint("R", n)
		}(i)
	}
	wg.Wait()
	if m.BreakpointCount() != goroutines {
		t.Errorf("BreakpointCount = %d, want %d", m.BreakpointCount(), goroutines)
	}
}

func TestDefaultDebuggerAndInit(t *testing.T) {
	Init()
	d1 := DefaultDebugger()
	d2 := DefaultDebugger()
	if d1 != d2 {
		t.Error("DefaultDebugger returned different instances")
	}
	d1.Enable()
	d1.LogAction("a", "r", 1, "m")
	if d2.Count() != 1 {
		t.Errorf("Count after log via default = %d, want 1", d2.Count())
	}
	Init()
	if DefaultDebugger().Count() != 0 {
		t.Error("expected reset after Init()")
	}
	if DefaultDebugger().IsEnabled() {
		t.Error("expected disabled after Init()")
	}
}
