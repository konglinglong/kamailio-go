// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - memcached module tests.
 *
 * These tests do NOT require a running Memcached server. They exercise the
 * client against an in-memory mock MemcacheClient so the full cache API is
 * verified, including concurrent access.
 */

package memcached

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.Servers) != 1 || cfg.Servers[0] != "127.0.0.1:11211" {
		t.Errorf("servers = %v, want [127.0.0.1:11211]", cfg.Servers)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", cfg.Timeout)
	}
	if cfg.MaxConns != 10 {
		t.Errorf("max conns = %d, want 10", cfg.MaxConns)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: %v", err)
	}
	if err := (&Config{}).Validate(); err == nil {
		t.Error("empty config expected error")
	}
	if err := (&Config{Servers: []string{"  "}}).Validate(); err == nil {
		t.Error("whitespace server expected error")
	}
	if err := (&Config{Servers: []string{"h:1"}, Timeout: -1}).Validate(); err == nil {
		t.Error("negative timeout expected error")
	}
}

func TestSetters(t *testing.T) {
	m := New()
	m.SetServers([]string{"cache1:11211", "cache2:11211"})
	m.SetTimeout(2 * time.Second)
	cfg := m.Config()
	if len(cfg.Servers) != 2 || cfg.Servers[0] != "cache1:11211" {
		t.Errorf("servers = %v", cfg.Servers)
	}
	if cfg.Timeout != 2*time.Second {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
}

func TestSetAndGet(t *testing.T) {
	m := New()
	if err := m.Set("k1", "v1", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := m.Get("k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "v1" {
		t.Errorf("Get = %q, want v1", v)
	}
}

func TestGetNotFound(t *testing.T) {
	m := New()
	_, err := m.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestGetEmptyKey(t *testing.T) {
	m := New()
	if _, err := m.Get(""); err == nil {
		t.Error("Get('') expected error")
	}
	if err := m.Set("", "v", 0); err == nil {
		t.Error("Set('',...) expected error")
	}
	if err := m.Delete(""); err == nil {
		t.Error("Delete('') expected error")
	}
}

func TestDelete(t *testing.T) {
	m := New()
	m.Set("k", "v", 0)
	if err := m.Delete("k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get("k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after Delete, Get err = %v, want ErrNotFound", err)
	}
	// Deleting a missing key is not an error.
	if err := m.Delete("nope"); err != nil {
		t.Errorf("Delete(missing) = %v, want nil", err)
	}
}

func TestSetWithTTL(t *testing.T) {
	mc := newMockClient()
	if err := mc.Set("k", "v", 1); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := mc.Get("k")
	if err != nil || v != "v" {
		t.Fatalf("Get = %q %v, want v nil", v, err)
	}
	// Manually expire the entry.
	mc.mu.Lock()
	e := mc.data["k"]
	e.expiry = time.Now().Add(-time.Second)
	mc.data["k"] = e
	mc.mu.Unlock()
	if _, err := mc.Get("k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired Get err = %v, want ErrNotFound", err)
	}
}

func TestGetMulti(t *testing.T) {
	m := New()
	m.Set("a", "1", 0)
	m.Set("b", "2", 0)
	m.Set("c", "3", 0)
	out, err := m.GetMulti([]string{"a", "b", "missing", "c"})
	if err != nil {
		t.Fatalf("GetMulti: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("GetMulti = %d items, want 3", len(out))
	}
	if out["a"] != "1" || out["b"] != "2" || out["c"] != "3" {
		t.Errorf("GetMulti = %v", out)
	}
}

func TestGetMultiEmpty(t *testing.T) {
	m := New()
	out, err := m.GetMulti(nil)
	if err != nil {
		t.Fatalf("GetMulti(nil): %v", err)
	}
	if len(out) != 0 {
		t.Errorf("GetMulti(nil) = %v, want empty", out)
	}
}

func TestStats(t *testing.T) {
	m := New()
	m.Set("a", "1", 0)
	m.Set("b", "2", 0)
	stats, err := m.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats["curr_items"] != "2" {
		t.Errorf("curr_items = %q, want 2", stats["curr_items"])
	}
}

func TestFlushAll(t *testing.T) {
	m := New()
	m.Set("a", "1", 0)
	m.Set("b", "2", 0)
	if err := m.FlushAll(); err != nil {
		t.Fatalf("FlushAll: %v", err)
	}
	if _, err := m.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after FlushAll, Get(a) err = %v, want ErrNotFound", err)
	}
}

func TestInitResetsPool(t *testing.T) {
	m := New()
	m.Set("k", "v", 0)
	cfg := *DefaultConfig()
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.OpsCount() != 0 {
		t.Errorf("OpsCount = %d after Init, want 0", m.OpsCount())
	}
}

func TestDefaultAndInit(t *testing.T) {
	cfg := *DefaultConfig()
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d := DefaultMemcached()
	if d == nil {
		t.Fatal("DefaultMemcached nil")
	}
	if err := Set("pk", "pv", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := Get("pk")
	if err != nil || v != "pv" {
		t.Errorf("Get = %q %v, want pv nil", v, err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			key := "k" + itoa(i%5)
			if err := m.Set(key, itoa(i), 0); err != nil {
				t.Errorf("Set %d: %v", i, err)
				return
			}
			if _, err := m.Get(key); err != nil && !errors.Is(err, ErrNotFound) {
				t.Errorf("Get %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if got := m.OpsCount(); got != 100 {
		t.Errorf("OpsCount = %d, want 100", got)
	}
}

func TestMockClientClosed(t *testing.T) {
	mc := newMockClient()
	if err := mc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mc.Set("k", "v", 0); err == nil {
		t.Error("Set after Close expected error")
	}
	if _, err := mc.Get("k"); err == nil {
		t.Error("Get after Close expected error")
	}
	if _, err := mc.Stats(); err == nil {
		t.Error("Stats after Close expected error")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
