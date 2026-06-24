// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the app_lua module - Lua script bindings (mock).
 */
package app_lua

import (
	"sync"
	"testing"
)

func TestInitAndLoad(t *testing.T) {
	m := New()
	if m.IsLoaded() {
		t.Error("fresh module should not be loaded")
	}
	if err := m.Init(&LuaConfig{ScriptPath: "route.lua"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Init with ScriptPath")
	}
	if err := m.LoadScript("other.lua"); err != nil {
		t.Fatalf("LoadScript: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after LoadScript")
	}
}

func TestRegisterAndCall(t *testing.T) {
	m := New()
	_ = m.Init(&LuaConfig{ScriptPath: "route.lua"})
	m.RegisterFunction("add", func(args ...interface{}) (interface{}, error) {
		sum := 0
		for _, a := range args {
			if n, ok := a.(int); ok {
				sum += n
			}
		}
		return sum, nil
	})
	got, err := m.CallFunction("add", 1, 2, 3)
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}
	if got != 6 {
		t.Errorf("CallFunction = %v, want 6", got)
	}
}

func TestCallErrors(t *testing.T) {
	m := New()
	// Not loaded.
	if _, err := m.CallFunction("x"); err == nil {
		t.Error("CallFunction before load should error")
	}
	_ = m.Init(&LuaConfig{ScriptPath: "s.lua"})
	// Loaded but unregistered function.
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
	_ = m.Init(&LuaConfig{ScriptPath: "s.lua"})
	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !m.IsLoaded() {
		t.Error("expected loaded after Reload")
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultLua()
	d2 := DefaultLua()
	if d1 != d2 {
		t.Error("DefaultLua should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	_ = m.Init(&LuaConfig{ScriptPath: "s.lua"})
	m.RegisterFunction("add", func(args ...interface{}) (interface{}, error) {
		return len(args), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.CallFunction("add", 1, 2)
			_ = m.IsLoaded()
		}()
	}
	wg.Wait()
}
