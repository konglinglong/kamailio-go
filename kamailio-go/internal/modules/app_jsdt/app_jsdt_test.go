// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_jsdt module - JavaScript (QuickJS) script bindings (mock).
 */
package app_jsdt

import (
	"sync"
	"testing"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&JSDTConfig{ScriptPath: "route.js"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if err := m.LoadScript("other.js"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after LoadScript")
	}
}

func TestRegisterAndCall(t *testing.T) {
	m := New()
	_ = m.Init(&JSDTConfig{ScriptPath: "route.js"})
	m.RegisterFunction("concat", func(args ...interface{}) (interface{}, error) {
		out := ""
		for _, a := range args {
			if s, ok := a.(string); ok {
				out += s
			}
		}
		return out, nil
	})
	got, err := m.CallFunction("concat", "foo", "bar", "baz")
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != "foobarbaz" {
		t.Errorf("CallFunction = %v, want foobarbaz", got)
	}
}

func TestCallErrors(t *testing.T) {
	m := New()
	if _, err := m.CallFunction("x"); err == nil {
		t.Error("CallFunction before load should error")
	}
	_ = m.Init(&JSDTConfig{ScriptPath: "s.js"})
	if _, err := m.CallFunction("missing"); err == nil {
		t.Error("CallFunction missing should error")
	}
}

func TestLoadScriptEmpty(t *testing.T) {
	m := New()
	if err := m.LoadScript(""); err == nil {
		t.Error("LoadScript(empty) should error")
	}
}

func TestReload(t *testing.T) {
	m := New()
	if err := m.Reload(); err == nil {
		t.Error("Reload with no script should error")
	}
	_ = m.Init(&JSDTConfig{ScriptPath: "s.js"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultJSDT()
	d2 := DefaultJSDT()
	if d1 != d2 {
		t.Error("DefaultJSDT should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(&JSDTConfig{ScriptPath: "s.js"})
	m.RegisterFunction("n", func(args ...interface{}) (interface{}, error) {
		return len(args), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.CallFunction("n", 1, 2)
			_ = m.IsLoaded()
		}()
	}
	wg.Wait()
}
