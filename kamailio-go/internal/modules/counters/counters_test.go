// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the counters module.
 */

package counters

import (
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Register / Lookup
// ---------------------------------------------------------------------------

func TestRegister_NewCounter(t *testing.T) {
	m := NewCountersModule()
	h, err := m.Register("core", "received_requests", 0, nil, "total requests", 0)
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if !h.IsValid() {
		t.Error("expected valid handle")
	}
	if m.CounterCount() != 1 {
		t.Errorf("CounterCount = %d, want 1", m.CounterCount())
	}
	if m.GroupCount() != 1 {
		t.Errorf("GroupCount = %d, want 1", m.GroupCount())
	}
}

func TestRegister_DuplicateWithoutFlag(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "reqs", 0, nil, "", 0)
	_, err := m.Register("core", "reqs", 0, nil, "", 0)
	if err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegister_DuplicateWithFlag(t *testing.T) {
	m := NewCountersModule()
	h1, _ := m.Register("core", "reqs", 0, nil, "", 0)
	h2, err := m.Register("core", "reqs", 0, nil, "", 1)
	if err != nil {
		t.Fatalf("Register with flag 1 error: %v", err)
	}
	if h1.ID != h2.ID {
		t.Errorf("expected same handle, got %d vs %d", h1.ID, h2.ID)
	}
	if m.CounterCount() != 1 {
		t.Errorf("CounterCount = %d, want 1 (idempotent)", m.CounterCount())
	}
}

func TestRegister_EmptyName(t *testing.T) {
	m := NewCountersModule()
	_, err := m.Register("core", "", 0, nil, "", 0)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestRegister_EmptyGroup_UsesScriptGroup(t *testing.T) {
	m := NewCountersModule()
	m.SetScriptGroup("myapp")
	h, err := m.Register("", "reqs", 0, nil, "", 0)
	if err != nil {
		t.Fatalf("Register error: %v", err)
	}
	if m.GetGroup(h) != "myapp" {
		t.Errorf("group = %q, want myapp", m.GetGroup(h))
	}
}

func TestLookup_ByGroupName(t *testing.T) {
	m := NewCountersModule()
	h1, _ := m.Register("core", "reqs", 0, nil, "", 0)
	h2, err := m.Lookup("core", "reqs")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if h1.ID != h2.ID {
		t.Errorf("handle mismatch: %d vs %d", h1.ID, h2.ID)
	}
}

func TestLookup_EmptyGroup_FirstMatch(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "reqs", 0, nil, "", 0)
	m.Register("other", "reqs", 0, nil, "", 0)
	// With an empty group, the lookup returns the first counter with a
	// matching name. In Go, map iteration order is randomised, so we
	// only verify that A counter with the right name is returned.
	h, err := m.Lookup("", "reqs")
	if err != nil {
		t.Fatalf("Lookup error: %v", err)
	}
	if !h.IsValid() {
		t.Fatal("expected valid handle")
	}
	if name := m.GetName(h); name != "reqs" {
		t.Errorf("name = %q, want reqs", name)
	}
}

func TestLookup_NotFound(t *testing.T) {
	m := NewCountersModule()
	_, err := m.Lookup("core", "nonexistent")
	if err == nil {
		t.Error("expected error for not found")
	}
}

func TestRegisterArray(t *testing.T) {
	m := NewCountersModule()
	defs := []CounterDef{
		{Name: "reqs", Doc: "total requests"},
		{Name: "errors", Doc: "total errors", Flags: FlagNoReset},
		{Name: "bytes", Doc: "total bytes"},
	}
	if err := m.RegisterArray("core", defs); err != nil {
		t.Fatalf("RegisterArray error: %v", err)
	}
	if m.CounterCount() != 3 {
		t.Errorf("CounterCount = %d, want 3", m.CounterCount())
	}
}

// ---------------------------------------------------------------------------
// Inc / Add / Reset
// ---------------------------------------------------------------------------

func TestInc(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "reqs", 0, nil, "", 0)
	m.Inc(h)
	m.Inc(h)
	m.Inc(h)
	if v := m.GetVal(h); v != 3 {
		t.Errorf("GetVal = %d, want 3", v)
	}
}

func TestAdd(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "bytes", 0, nil, "", 0)
	m.Add(h, 100)
	m.Add(h, -30)
	if v := m.GetVal(h); v != 70 {
		t.Errorf("GetVal = %d, want 70", v)
	}
}

func TestReset(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "reqs", 0, nil, "", 0)
	m.Add(h, 42)
	m.Reset(h)
	if v := m.GetVal(h); v != 0 {
		t.Errorf("GetVal = %d, want 0 after reset", v)
	}
}

func TestReset_FlagNoReset(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "uptime", FlagNoReset, nil, "", 0)
	m.Add(h, 99)
	m.Reset(h)
	if v := m.GetVal(h); v != 99 {
		t.Errorf("GetVal = %d, want 99 (FlagNoReset)", v)
	}
}

func TestInc_InvalidHandle(t *testing.T) {
	m := NewCountersModule()
	m.Inc(InvalidHandle) // must not panic
	m.Add(InvalidHandle, 5)
	m.Reset(InvalidHandle)
	if v := m.GetVal(InvalidHandle); v != 0 {
		t.Errorf("GetVal(invalid) = %d, want 0", v)
	}
}

// ---------------------------------------------------------------------------
// GetVal / GetRawVal with callback
// ---------------------------------------------------------------------------

func TestGetVal_Callback(t *testing.T) {
	m := NewCountersModule()
	cb := func(h CounterHandle) CounterVal { return 42 }
	h, _ := m.Register("core", "computed", 0, cb, "", 0)
	m.Add(h, 100) // raw value, but callback should be used
	if v := m.GetVal(h); v != 42 {
		t.Errorf("GetVal = %d, want 42 (callback)", v)
	}
	if v := m.GetRawVal(h); v != 100 {
		t.Errorf("GetRawVal = %d, want 100 (raw)", v)
	}
}

func TestGetVal_NoCallback(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "reqs", 0, nil, "", 0)
	m.Add(h, 55)
	if v := m.GetVal(h); v != 55 {
		t.Errorf("GetVal = %d, want 55", v)
	}
	if v := m.GetRawVal(h); v != 55 {
		t.Errorf("GetRawVal = %d, want 55", v)
	}
}

// ---------------------------------------------------------------------------
// GetName / GetGroup / GetDoc
// ---------------------------------------------------------------------------

func TestGetMetadata(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "reqs", FlagNoReset, nil, "total requests", 0)
	if m.GetName(h) != "reqs" {
		t.Errorf("name = %q", m.GetName(h))
	}
	if m.GetGroup(h) != "core" {
		t.Errorf("group = %q", m.GetGroup(h))
	}
	if m.GetDoc(h) != "total requests" {
		t.Errorf("doc = %q", m.GetDoc(h))
	}
	// Invalid handle.
	if m.GetName(InvalidHandle) != "" {
		t.Error("expected empty name for invalid handle")
	}
}

// ---------------------------------------------------------------------------
// Name-based operations (script functions)
// ---------------------------------------------------------------------------

func TestIncByName_GroupDotName(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "reqs", 0, nil, "", 0)
	if err := m.IncByName("core.reqs"); err != nil {
		t.Fatalf("IncByName error: %v", err)
	}
	if err := m.IncByName("core.reqs"); err != nil {
		t.Fatalf("IncByName #2 error: %v", err)
	}
	h, _ := m.Lookup("core", "reqs")
	if v := m.GetVal(h); v != 2 {
		t.Errorf("GetVal = %d, want 2", v)
	}
}

func TestIncByName_DefaultGroup(t *testing.T) {
	m := NewCountersModule()
	m.Register("script", "reqs", 0, nil, "", 0)
	if err := m.IncByName("reqs"); err != nil {
		t.Fatalf("IncByName error: %v", err)
	}
	h, _ := m.Lookup("script", "reqs")
	if v := m.GetVal(h); v != 1 {
		t.Errorf("GetVal = %d, want 1", v)
	}
}

func TestAddByName(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "bytes", 0, nil, "", 0)
	if err := m.AddByName("core.bytes", 500); err != nil {
		t.Fatalf("AddByName error: %v", err)
	}
	h, _ := m.Lookup("core", "bytes")
	if v := m.GetVal(h); v != 500 {
		t.Errorf("GetVal = %d, want 500", v)
	}
}

func TestResetByName(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "reqs", 0, nil, "", 0)
	m.IncByName("core.reqs")
	if err := m.ResetByName("core.reqs"); err != nil {
		t.Fatalf("ResetByName error: %v", err)
	}
	h, _ := m.Lookup("core", "reqs")
	if v := m.GetVal(h); v != 0 {
		t.Errorf("GetVal = %d, want 0", v)
	}
}

func TestGetValByName(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "reqs", 0, nil, "", 0)
	m.IncByName("core.reqs")
	v, err := m.GetValByName("core.reqs")
	if err != nil {
		t.Fatalf("GetValByName error: %v", err)
	}
	if v != 1 {
		t.Errorf("GetValByName = %d, want 1", v)
	}
}

func TestIncByName_NotFound(t *testing.T) {
	m := NewCountersModule()
	err := m.IncByName("nonexistent.counter")
	if err == nil {
		t.Error("expected error for unknown counter")
	}
}

// ---------------------------------------------------------------------------
// Iteration
// ---------------------------------------------------------------------------

func TestIterateGroupNames_Sorted(t *testing.T) {
	m := NewCountersModule()
	m.Register("zeta", "a", 0, nil, "", 0)
	m.Register("alpha", "b", 0, nil, "", 0)
	m.Register("mid", "c", 0, nil, "", 0)
	var groups []string
	m.IterateGroupNames(func(g string) { groups = append(groups, g) })
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0] != "alpha" || groups[1] != "mid" || groups[2] != "zeta" {
		t.Errorf("groups = %v, want [alpha mid zeta]", groups)
	}
}

func TestIterateGroupVarNames_Sorted(t *testing.T) {
	m := NewCountersModule()
	m.Register("core", "zebra", 0, nil, "", 0)
	m.Register("core", "alpha", 0, nil, "", 0)
	m.Register("core", "mid", 0, nil, "", 0)
	var names []string
	m.IterateGroupVarNames("core", func(n string) { names = append(names, n) })
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "alpha" || names[1] != "mid" || names[2] != "zebra" {
		t.Errorf("names = %v, want [alpha mid zebra]", names)
	}
}

func TestIterateGroupVars(t *testing.T) {
	m := NewCountersModule()
	h1, _ := m.Register("core", "reqs", 0, nil, "", 0)
	h2, _ := m.Register("core", "errors", 0, nil, "", 0)
	m.Add(h1, 10)
	m.Add(h2, 5)
	type entry struct{ group, name string; handle CounterHandle }
	var entries []entry
	m.IterateGroupVars("core", func(g, n string, h CounterHandle) {
		entries = append(entries, entry{g, n, h})
	})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].name != "errors" {
		t.Errorf("first entry name = %q, want errors (sorted)", entries[0].name)
	}
	for _, e := range entries {
		if e.group != "core" {
			t.Errorf("group = %q, want core", e.group)
		}
		if !e.handle.IsValid() {
			t.Error("expected valid handle")
		}
	}
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

func TestSetScriptGroup(t *testing.T) {
	m := NewCountersModule()
	if m.ScriptGroup() != "script" {
		t.Errorf("default script group = %q, want script", m.ScriptGroup())
	}
	m.SetScriptGroup("myapp")
	if m.ScriptGroup() != "myapp" {
		t.Errorf("script group = %q, want myapp", m.ScriptGroup())
	}
	m.SetScriptGroup("") // empty falls back to "script"
	if m.ScriptGroup() != "script" {
		t.Errorf("script group = %q, want script (fallback)", m.ScriptGroup())
	}
}

func TestAddScriptCounter_NameOnly(t *testing.T) {
	m := NewCountersModule()
	if err := m.AddScriptCounter("reqs"); err != nil {
		t.Fatalf("AddScriptCounter error: %v", err)
	}
	h, _ := m.Lookup("script", "reqs")
	if m.GetDoc(h) != "custom script counter." {
		t.Errorf("doc = %q", m.GetDoc(h))
	}
}

func TestAddScriptCounter_GroupDotName(t *testing.T) {
	m := NewCountersModule()
	if err := m.AddScriptCounter("core.reqs"); err != nil {
		t.Fatalf("AddScriptCounter error: %v", err)
	}
	h, _ := m.Lookup("core", "reqs")
	if !h.IsValid() {
		t.Error("expected valid handle for core.reqs")
	}
}

func TestAddScriptCounter_WithDescription(t *testing.T) {
	m := NewCountersModule()
	if err := m.AddScriptCounter("core.reqs total received requests"); err != nil {
		t.Fatalf("AddScriptCounter error: %v", err)
	}
	h, _ := m.Lookup("core", "reqs")
	if m.GetDoc(h) != "total received requests" {
		t.Errorf("doc = %q, want 'total received requests'", m.GetDoc(h))
	}
}

func TestAddScriptCounter_GroupDotName_Desc(t *testing.T) {
	m := NewCountersModule()
	if err := m.AddScriptCounter("core.reqs:total requests"); err != nil {
		t.Fatalf("AddScriptCounter error: %v", err)
	}
	h, _ := m.Lookup("core", "reqs")
	if m.GetDoc(h) != "total requests" {
		t.Errorf("doc = %q, want 'total requests'", m.GetDoc(h))
	}
}

func TestAddScriptCounter_EmptySpec(t *testing.T) {
	m := NewCountersModule()
	if err := m.AddScriptCounter(""); err == nil {
		t.Error("expected error for empty spec")
	}
}

// ---------------------------------------------------------------------------
// CounterHandle
// ---------------------------------------------------------------------------

func TestCounterHandle_IsValid(t *testing.T) {
	if InvalidHandle.IsValid() {
		t.Error("InvalidHandle should not be valid")
	}
	h := CounterHandle{ID: 1}
	if !h.IsValid() {
		t.Error("handle with ID=1 should be valid")
	}
}

// ---------------------------------------------------------------------------
// DefaultCounters singleton / Init
// ---------------------------------------------------------------------------

func TestDefaultCounters_Singleton(t *testing.T) {
	a := DefaultCounters()
	b := DefaultCounters()
	if a != b {
		t.Error("DefaultCounters must return same instance")
	}
}

func TestInit_Reset(t *testing.T) {
	a := DefaultCounters()
	a.Register("core", "temp", 0, nil, "", 0)
	if a.CounterCount() != 1 {
		t.Fatalf("CounterCount = %d, want 1", a.CounterCount())
	}
	Init()
	b := DefaultCounters()
	if b.CounterCount() != 0 {
		t.Errorf("CounterCount = %d, want 0 after Init", b.CounterCount())
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	m := NewCountersModule()
	h, _ := m.Register("core", "concurrent", 0, nil, "", 0)
	const n = 100
	var wg sync.WaitGroup
	// Concurrent writers.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Inc(h)
		}()
	}
	// Concurrent readers.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.GetVal(h)
			_ = m.GetRawVal(h)
		}()
	}
	// Concurrent registrators.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _ = m.Register("dyn", "counter", 0, nil, "", 1)
		}(i)
	}
	// Concurrent name-based ops.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.IncByName("core.concurrent")
		}()
	}
	wg.Wait()
	v := m.GetVal(h)
	if v != int64(2*n) {
		t.Errorf("GetVal = %d, want %d", v, 2*n)
	}
}
