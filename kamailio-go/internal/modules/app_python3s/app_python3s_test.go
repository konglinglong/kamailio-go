// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_python3s module - Python3 SIP-specific bindings (mock).
 */
package app_python3s

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&Config{ScriptPath: "router.py"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if m.ScriptCount() != 1 {
		t.Errorf("ScriptCount = %d, want 1", m.ScriptCount())
	}
	if err := m.LoadScript("other.py"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if m.ScriptCount() != 2 {
		t.Errorf("ScriptCount = %d, want 2", m.ScriptCount())
	}
}

func TestLoadScriptEmpty(t *testing.T) {
	m := New()
	if err := m.LoadScript(""); err == nil {
		t.Error("LoadScript(empty) should error")
	}
}

func TestInitNilConfig(t *testing.T) {
	m := New()
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init(nil): %v", err)
	}
	if m.IsLoaded() {
		t.Error("nil config should not load a script")
	}
}

func TestRegisterAndGetMsg(t *testing.T) {
	m := New()
	msg := &parser.SIPMsg{ID: 42}
	handle := m.RegisterMsg(msg)
	if handle == 0 {
		t.Error("handle should be non-zero")
	}
	got := m.GetMsg(handle)
	if got == nil {
		t.Fatal("GetMsg returned nil")
	}
	if got.ID != 42 {
		t.Errorf("msg ID = %d, want 42", got.ID)
	}
	m.ReleaseMsg(handle)
	if m.GetMsg(handle) != nil {
		t.Error("GetMsg should return nil after ReleaseMsg")
	}
}

func TestGetMsgUnknownHandle(t *testing.T) {
	m := New()
	if m.GetMsg(999) != nil {
		t.Error("GetMsg unknown handle should return nil")
	}
}

func TestReleaseMsgUnknownHandle(t *testing.T) {
	m := New()
	// Should not panic.
	m.ReleaseMsg(999)
}

func TestCallFunction(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	mock := m.interp.(*mockPython3SInterp)
	mock.RegisterFunc("route", func(args ...interface{}) interface{} {
		// args[0] is the msg handle
		if len(args) == 0 {
			return -1
		}
		handle, ok := args[0].(uint64)
		if !ok {
			return -1
		}
		if handle == 0 {
			return -1
		}
		return 1
	})
	msg := &parser.SIPMsg{ID: 7}
	ret, err := m.CallFunction("route", msg)
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 1 {
		t.Errorf("ret = %d, want 1", ret)
	}
	// The message should have been released after the call.
	if m.MsgCount() != 0 {
		t.Errorf("MsgCount = %d, want 0 after call", m.MsgCount())
	}
}

func TestCallBeforeLoad(t *testing.T) {
	m := New()
	if _, err := m.CallFunction("x", &parser.SIPMsg{}); err == nil {
		t.Error("CallFunction before load should error")
	}
}

func TestCallUnknownFunction(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	if _, err := m.CallFunction("missing", &parser.SIPMsg{}); err == nil {
		t.Error("CallFunction unknown should error")
	}
}

func TestEvalExpr(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	got, err := m.EvalExpr("1+2", &parser.SIPMsg{ID: 1})
	if err != nil {
		t.Fatalf("EvalExpr: %v", err)
	}
	if got != "1+2" {
		t.Errorf("EvalExpr = %v, want %q", got, "1+2")
	}
	if m.MsgCount() != 0 {
		t.Errorf("MsgCount = %d, want 0 after eval", m.MsgCount())
	}
}

func TestEvalExprBeforeLoad(t *testing.T) {
	m := New()
	if _, err := m.EvalExpr("1", &parser.SIPMsg{}); err == nil {
		t.Error("EvalExpr before load should error")
	}
}

func TestClose(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	m.RegisterMsg(&parser.SIPMsg{})
	m.Close()
	if m.IsLoaded() {
		t.Error("expected not loaded after Close")
	}
	if m.MsgCount() != 0 {
		t.Error("expected empty registry after Close")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultAppPython3S()
	d2 := DefaultAppPython3S()
	if d1 != d2 {
		t.Error("DefaultAppPython3S should return same instance")
	}
	Init()
	d3 := DefaultAppPython3S()
	if d3 == d1 {
		t.Error("Init should reset default module to a new instance")
	}
}

func TestMockInterpreter(t *testing.T) {
	mock := newMockPython3SInterp()
	if err := mock.LoadScript("a.py"); err != nil {
		t.Fatalf("mock LoadScript: %v", err)
	}
	if !mock.HasScript("a.py") {
		t.Error("mock should have script a.py")
	}
	mock.RegisterFunc("answer", func(args ...interface{}) interface{} { return 42 })
	res, err := mock.CallFunction("answer")
	if err != nil {
		t.Fatalf("mock CallFunction: %v", err)
	}
	if res.(int) != 42 {
		t.Errorf("mock result = %v, want 42", res)
	}
	if _, err := mock.CallFunction("nope"); err == nil {
		t.Error("mock unknown function should error")
	}
	mock.Close()
	if mock.HasScript("a.py") {
		t.Error("mock should not have script after Close")
	}
}

func TestNewWithInterpreter(t *testing.T) {
	mock := newMockPython3SInterp()
	mock.RegisterFunc("ping", func(args ...interface{}) interface{} { return 1 })
	m := NewWithInterpreter(mock)
	_ = m.Init(&Config{ScriptPath: "s.py"})
	ret, err := m.CallFunction("ping", &parser.SIPMsg{})
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 1 {
		t.Errorf("ret = %d, want 1", ret)
	}
}

func TestNewWithNilInterpreter(t *testing.T) {
	m := NewWithInterpreter(nil)
	if m.interp == nil {
		t.Error("nil interpreter should fall back to mock")
	}
}

func TestMultipleMsgHandles(t *testing.T) {
	m := New()
	h1 := m.RegisterMsg(&parser.SIPMsg{ID: 1})
	h2 := m.RegisterMsg(&parser.SIPMsg{ID: 2})
	if h1 == h2 {
		t.Error("handles should be unique")
	}
	if m.GetMsg(h1).ID != 1 {
		t.Error("h1 should map to msg ID 1")
	}
	if m.GetMsg(h2).ID != 2 {
		t.Error("h2 should map to msg ID 2")
	}
	if m.MsgCount() != 2 {
		t.Errorf("MsgCount = %d, want 2", m.MsgCount())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(&Config{ScriptPath: "s.py"})
	mock := m.interp.(*mockPython3SInterp)
	mock.RegisterFunc("n", func(args ...interface{}) interface{} { return 1 })
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.CallFunction("n", &parser.SIPMsg{})
			h := m.RegisterMsg(&parser.SIPMsg{})
			_ = m.GetMsg(h)
			m.ReleaseMsg(h)
			_ = m.IsLoaded()
		}()
	}
	wg.Wait()
}

func TestPackageLevelHelpers(t *testing.T) {
	Init()
	if err := LoadScript("pkg.py"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	msg := &parser.SIPMsg{ID: 99}
	h := RegisterMsg(msg)
	if GetMsg(h) == nil {
		t.Fatal("GetMsg returned nil")
	}
	ReleaseMsg(h)
	d := DefaultAppPython3S()
	mock := d.interp.(*mockPython3SInterp)
	mock.RegisterFunc("f", func(args ...interface{}) interface{} { return 0 })
	ret, err := CallFunction("f", &parser.SIPMsg{})
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if ret != 0 {
		t.Errorf("ret = %d, want 0", ret)
	}
	if _, err := EvalExpr("x", &parser.SIPMsg{}); err != nil {
		t.Fatalf("EvalExpr: %v", err)
	}
}
