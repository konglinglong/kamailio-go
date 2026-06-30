// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the runtime config Manager (hot-reload).
 */

package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// writeConfig writes a YAML config file to a temp path and returns the
// path. The caller is responsible for cleanup (t.TempDir() handles it).
func writeConfig(t *testing.T, path, yaml string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestNewManager_StoresConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Realm = "test.example.com"
	m := NewManager(cfg, "/tmp/whatever.yaml")
	if got := m.Get(); got != cfg {
		t.Fatalf("Get returned %p, want %p", got, cfg)
	}
	if m.Path() != "/tmp/whatever.yaml" {
		t.Fatalf("Path = %q, want /tmp/whatever.yaml", m.Path())
	}
}

func TestNewManager_NilConfigFallsBackToDefault(t *testing.T) {
	m := NewManager(nil, "")
	got := m.Get()
	if got == nil {
		t.Fatal("expected a non-nil default config")
	}
	// DefaultConfig populates Core.Listen and Core.Workers; the flat
	// Realm overlay is intentionally left empty.
	if len(got.Core.Listen) == 0 {
		t.Fatalf("expected default listen list, got empty")
	}
	if got.Core.Workers <= 0 {
		t.Fatalf("expected default workers > 0, got %d", got.Core.Workers)
	}
	if m.Path() != "" {
		t.Fatalf("Path = %q, want empty", m.Path())
	}
}

func TestReload_ReReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: first.example.com
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := NewManager(cfg, path)
	if got := m.Get().Realm; got != "first.example.com" {
		t.Fatalf("initial realm = %q", got)
	}

	// Rewrite the file with a new realm and reload.
	writeConfig(t, path, `
core:
  log_level: debug
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: second.example.com
`)
	newCfg, err := m.Reload()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if newCfg.Realm != "second.example.com" {
		t.Fatalf("reloaded realm = %q, want second.example.com", newCfg.Realm)
	}
	if got := m.Get().Realm; got != "second.example.com" {
		t.Fatalf("post-reload Get() = %q", got)
	}
	if newCfg.Core.LogLevel != "debug" {
		t.Fatalf("reloaded level = %q, want debug", newCfg.Core.LogLevel)
	}
}

func TestReload_NoPathReturnsError(t *testing.T) {
	m := NewManager(DefaultConfig(), "")
	if _, err := m.Reload(); err == nil {
		t.Fatal("expected error when no path is set")
	}
}

func TestReload_InvalidConfigDoesNotSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: keep.example.com
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := NewManager(cfg, path)

	// Corrupt the file with an invalid log level. ValidateStrict
	// rejects unknown levels, while Validate() (called inside Load)
	// only auto-corrects workers — so this reliably trips the reload
	// validator without being silently fixed first.
	writeConfig(t, path, `
core:
  log_level: not-a-real-level
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: broken.example.com
`)
	_, err = m.Reload()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	// Live config must be untouched.
	if got := m.Get().Realm; got != "keep.example.com" {
		t.Fatalf("live realm = %q, want keep.example.com", got)
	}
}

func TestReload_NotifiesSubscribers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: v1
`)
	cfg, _ := Load(path)
	m := NewManager(cfg, path)

	var calls int32
	var lastNew string
	unsub := m.Subscribe(func(old, new *Config) error {
		atomic.AddInt32(&calls, 1)
		lastNew = new.Realm
		return nil
	})
	_ = unsub

	// Rewrite & reload.
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: v2
`)
	if _, err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("subscriber calls = %d, want 1", calls)
	}
	if lastNew != "v2" {
		t.Fatalf("lastNew = %q, want v2", lastNew)
	}
}

func TestSubscribe_UnsubscribeStopsCallbacks(t *testing.T) {
	m := NewManager(DefaultConfig(), "")
	var calls int32
	unsub := m.Subscribe(func(_, _ *Config) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	unsub()
	// SetConfig also notifies subscribers.
	if err := m.SetConfig(DefaultConfig()); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("unsubscribed callback fired %d times", calls)
	}
}

func TestSetConfig_SwapsAndNotifies(t *testing.T) {
	m := NewManager(DefaultConfig(), "")
	var seen string
	m.Subscribe(func(_, new *Config) error {
		seen = new.Realm
		return nil
	})
	cfg := DefaultConfig()
	cfg.Realm = "set.example.com"
	if err := m.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if m.Get().Realm != "set.example.com" {
		t.Fatalf("Get = %q", m.Get().Realm)
	}
	if seen != "set.example.com" {
		t.Fatalf("subscriber saw %q", seen)
	}
}

func TestSetConfig_RejectsInvalid(t *testing.T) {
	m := NewManager(DefaultConfig(), "")
	bad := DefaultConfig()
	bad.Core.Workers = 0 // validator rejects this
	if err := m.SetConfig(bad); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSetPath_EnablesSubsequentReload(t *testing.T) {
	m := NewManager(DefaultConfig(), "")
	if _, err := m.Reload(); err == nil {
		t.Fatal("expected error with no path")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: late.example.com
`)
	m.SetPath(path)
	if _, err := m.Reload(); err != nil {
		t.Fatalf("Reload after SetPath: %v", err)
	}
	if m.Get().Realm != "late.example.com" {
		t.Fatalf("realm = %q", m.Get().Realm)
	}
}

// TestSubscriberCallbackReceivesOldAndNew verifies the (old, new)
// contract: the first reload delivers the original config as `old`.
func TestSubscriberCallbackReceivesOldAndNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: original
`)
	cfg, _ := Load(path)
	m := NewManager(cfg, path)

	var oldRealm, newRealm string
	m.Subscribe(func(old, new *Config) error {
		oldRealm = old.Realm
		newRealm = new.Realm
		return nil
	})
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: updated
`)
	if _, err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if oldRealm != "original" {
		t.Fatalf("old = %q, want original", oldRealm)
	}
	if newRealm != "updated" {
		t.Fatalf("new = %q, want updated", newRealm)
	}
}

// TestConcurrentGetDuringReload ensures Get() never blocks or panics
// while a reload is in flight. This is the core correctness guarantee
// for the atomic-pointer design.
func TestConcurrentGetDuringReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: r0
`)
	cfg, _ := Load(path)
	m := NewManager(cfg, path)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_ = m.Get()
			time.Sleep(100 * time.Microsecond)
		}
		close(done)
	}()
	for i := 0; i < 20; i++ {
		writeConfig(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: r-`+string(rune('a'+i))+`
`)
		_, _ = m.Reload()
	}
	<-done
}
