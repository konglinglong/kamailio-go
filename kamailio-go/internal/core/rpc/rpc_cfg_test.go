// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the cfg.* RPC methods (cfg.get / cfg.set /
 * cfg.list / cfg.reset / cfg.reload / cfg.snapshot) and the wiring of
 * cfg_rpc + config.Manager into the rpc.Server.
 */

package rpc

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/config"
	"github.com/kamailio/kamailio-go/internal/modules/cfg_rpc"
)

// startCfgRPCServer wires a rpc.Server with the given cfg_rpc module
// and (optional) config.Manager. Returns the server and the full /rpc
// URL the client should target.
func startCfgRPCServer(t *testing.T, cr *cfg_rpc.CfgRPCModule, mgr *config.Manager) (*Server, string) {
	t.Helper()
	srv := NewExtended(ServerConfig{CfgRPC: cr, ConfigManager: mgr})
	hs := httptest.NewServer(srv.handler)
	t.Cleanup(hs.Close)
	return srv, hs.URL + "/rpc"
}

func TestClient_CfgGetSet_RoundTrip(t *testing.T) {
	cr := cfg_rpc.New()
	cr.SetDefault("core.realm", "default.example.com")
	_, addr := startCfgRPCServer(t, cr, nil)
	c := NewClient(addr, 5*time.Second)

	// Default value is returned before any set.
	res, err := c.Call("kamailio.cfg.get", "core.realm")
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	m := res.(map[string]interface{})
	if m["found"] != true || m["value"] != "default.example.com" {
		t.Fatalf("get default = %v", m)
	}

	// Override the value.
	if _, err := c.Call("kamailio.cfg.set", "core.realm", "override.example.com"); err != nil {
		t.Fatalf("set: %v", err)
	}

	// New value is returned.
	res, err = c.Call("kamailio.cfg.get", "core.realm")
	if err != nil {
		t.Fatalf("get override: %v", err)
	}
	m = res.(map[string]interface{})
	if m["value"] != "override.example.com" {
		t.Fatalf("get override = %v", m)
	}
}

func TestClient_CfgGet_UnknownKeyReportsNotFound(t *testing.T) {
	cr := cfg_rpc.New()
	_, addr := startCfgRPCServer(t, cr, nil)
	c := NewClient(addr, 5*time.Second)

	res, err := c.Call("kamailio.cfg.get", "does.not.exist")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	m := res.(map[string]interface{})
	if m["found"] != false {
		t.Fatalf("expected found=false, got %v", m)
	}
}

func TestClient_CfgList_ReturnsDefaultsAndOverrides(t *testing.T) {
	cr := cfg_rpc.New()
	cr.SetDefault("a", "1")
	cr.SetDefault("b", "2")
	_, addr := startCfgRPCServer(t, cr, nil)
	c := NewClient(addr, 5*time.Second)

	// Add an override.
	if _, err := c.Call("kamailio.cfg.set", "b", "20"); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Add a brand new key.
	if _, err := c.Call("kamailio.cfg.set", "c", "3"); err != nil {
		t.Fatalf("set: %v", err)
	}

	res, err := c.Call("kamailio.cfg.list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m := res.(map[string]interface{})
	if m["ok"] != true {
		t.Fatalf("ok = %v", m["ok"])
	}
	entries := m["entries"].([]interface{})
	got := map[string]string{}
	for _, e := range entries {
		em := e.(map[string]interface{})
		got[em["key"].(string)] = em["value"].(string)
	}
	if got["a"] != "1" {
		t.Errorf("a = %q, want 1", got["a"])
	}
	if got["b"] != "20" {
		t.Errorf("b = %q, want 20 (override)", got["b"])
	}
	if got["c"] != "3" {
		t.Errorf("c = %q, want 3", got["c"])
	}
}

func TestClient_CfgReset_RestoresDefault(t *testing.T) {
	cr := cfg_rpc.New()
	cr.SetDefault("k", "default")
	_, addr := startCfgRPCServer(t, cr, nil)
	c := NewClient(addr, 5*time.Second)

	// Override then reset.
	if _, err := c.Call("kamailio.cfg.set", "k", "override"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := c.Call("kamailio.cfg.reset", "k"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	res, err := c.Call("kamailio.cfg.get", "k")
	if err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	m := res.(map[string]interface{})
	if m["value"] != "default" {
		t.Fatalf("after reset = %v, want default", m["value"])
	}
}

func TestClient_CfgReload_ReReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeFile(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: first.example.com
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	mgr := config.NewManager(cfg, path)
	_, addr := startCfgRPCServer(t, cfg_rpc.New(), mgr)
	c := NewClient(addr, 5*time.Second)

	// Snapshot before reload.
	res, err := c.Call("kamailio.cfg.snapshot")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if res.(map[string]interface{})["realm"] != "first.example.com" {
		t.Fatalf("pre-reload snapshot = %v", res)
	}

	// Rewrite file & reload.
	writeFile(t, path, `
core:
  log_level: debug
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: second.example.com
`)
	if _, err := c.Call("kamailio.cfg.reload"); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Snapshot after reload reflects the new realm.
	res, err = c.Call("kamailio.cfg.snapshot")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	m := res.(map[string]interface{})
	if m["realm"] != "second.example.com" {
		t.Fatalf("post-reload realm = %v, want second.example.com", m["realm"])
	}
	if m["log_level"] != "debug" {
		t.Fatalf("post-reload log_level = %v, want debug", m["log_level"])
	}
}

func TestClient_CfgReload_NotifiesSubscribers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeFile(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: v1
`)
	cfg, _ := config.Load(path)
	mgr := config.NewManager(cfg, path)
	seen := make(chan string, 4)
	mgr.Subscribe(func(_, new *config.Config) error {
		seen <- new.Realm
		return nil
	})
	_, addr := startCfgRPCServer(t, cfg_rpc.New(), mgr)
	c := NewClient(addr, 5*time.Second)

	writeFile(t, path, `
core:
  log_level: info
  workers: 4
  listen: ["udp:0.0.0.0:5060"]
realm: v2
`)
	if _, err := c.Call("kamailio.cfg.reload"); err != nil {
		t.Fatalf("reload: %v", err)
	}
	select {
	case r := <-seen:
		if r != "v2" {
			t.Fatalf("subscriber saw %q, want v2", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber was not notified within 2s")
	}
}

func TestClient_CfgReload_FailureReturnsOkFalse(t *testing.T) {
	// Manager is wired but has no path, so Reload() fails. The dispatch
	// case surfaces this as a JSON result with ok=false rather than an
	// RPC error, so operators can distinguish "not wired" (RPC error)
	// from "wired but reload failed" (ok=false).
	mgr := config.NewManager(config.DefaultConfig(), "")
	_, addr := startCfgRPCServer(t, cfg_rpc.New(), mgr)
	c := NewClient(addr, 5*time.Second)
	res, err := c.Call("kamailio.cfg.reload")
	if err != nil {
		t.Fatalf("Call returned transport error: %v", err)
	}
	m := res.(map[string]interface{})
	if m["ok"] != false {
		t.Fatalf("expected ok=false, got %v", m)
	}
	if m["error"] == nil || m["error"] == "" {
		t.Fatalf("expected non-empty error, got %v", m)
	}
}

func TestClient_CfgMethods_DisabledWhenNotWired(t *testing.T) {
	// Server with neither CfgRPC nor ConfigManager.
	srv := NewExtended(ServerConfig{})
	hs := httptest.NewServer(srv.handler)
	t.Cleanup(hs.Close)
	c := NewClient(hs.URL+"/rpc", 5*time.Second)

	for _, method := range []string{
		"kamailio.cfg.get",
		"kamailio.cfg.set",
		"kamailio.cfg.list",
		"kamailio.cfg.reset",
		"kamailio.cfg.reload",
		"kamailio.cfg.snapshot",
	} {
		if _, err := c.Call(method); err == nil {
			t.Errorf("%s: expected disabled error, got nil", method)
		}
	}
}

// writeFile is a small helper that writes content to path, failing the
// test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
