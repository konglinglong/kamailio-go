// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Integration tests for the bootstrap runtime-config hot-reload path.
 * Verifies that NewBootstrap wires the ConfigManager, CfgRPC module,
 * and registrar/log subscribers, and that handleReload() re-reads the
 * config file and propagates the new realm into the registrar.
 */

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/rpc"
)

// writeBootConfig writes a YAML config to path suitable for NewBootstrap.
// It uses a non-zero UDP port on 127.0.0.1 so the strict validator
// (which rejects port 0) accepts it.
func writeBootConfig(t *testing.T, path, realm, level string) {
	t.Helper()
	content := `
core:
  log_level: ` + level + `
  workers: 4
  listen: ["udp:127.0.0.1:25060"]
realm: ` + realm + `
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestBootstrap_WiresConfigManagerAndCfgRPC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeBootConfig(t, path, "first.example.com", "info")

	boot, err := NewBootstrap(BootstrapOptions{ConfigFile: path, RPCAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	defer boot.Shutdown()

	if boot.ConfigMgr == nil {
		t.Fatal("expected non-nil ConfigMgr")
	}
	if boot.CfgRPC == nil {
		t.Fatal("expected non-nil CfgRPC")
	}
	if boot.CfgDB == nil {
		t.Fatal("expected non-nil CfgDB")
	}
	if boot.CfgUtils == nil {
		t.Fatal("expected non-nil CfgUtils")
	}
	if boot.ConfigMgr.Path() != path {
		t.Fatalf("ConfigMgr.Path = %q, want %q", boot.ConfigMgr.Path(), path)
	}
	if got := boot.ConfigMgr.Get().Realm; got != "first.example.com" {
		t.Fatalf("initial realm = %q", got)
	}
}

func TestBootstrap_HandleReload_PropagatesToRegistrar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeBootConfig(t, path, "first.example.com", "info")

	boot, err := NewBootstrap(BootstrapOptions{ConfigFile: path, RPCAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	defer boot.Shutdown()

	// Rewrite the config with a new realm and trigger a reload via the
	// same internal entry point SIGHUP uses.
	writeBootConfig(t, path, "second.example.com", "debug")
	boot.handleReload()

	if got := boot.ConfigMgr.Get().Realm; got != "second.example.com" {
		t.Fatalf("post-reload realm = %q, want second.example.com", got)
	}
	// The cfg_rpc defaults subscriber should have updated the seeded
	// core.realm key, proving subscribers ran.
	val, err := boot.CfgRPC.Get("core.realm")
	if err != nil {
		t.Fatalf("CfgRPC.Get(core.realm): %v", err)
	}
	if val != "second.example.com" {
		t.Fatalf("cfg_rpc core.realm = %q, want second.example.com (subscriber did not fire)", val)
	}
}

func TestBootstrap_HandleReload_InvalidConfigLeavesLiveConfigIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeBootConfig(t, path, "keep.example.com", "info")

	boot, err := NewBootstrap(BootstrapOptions{ConfigFile: path, RPCAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	defer boot.Shutdown()

	// Corrupt the file with an invalid log level. handleReload should
	// log the error and leave the live config untouched.
	if err := os.WriteFile(path, []byte(`
core:
  log_level: not-a-real-level
  workers: 4
  listen: ["udp:127.0.0.1:25060"]
realm: broken.example.com
`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	boot.handleReload()
	if got := boot.ConfigMgr.Get().Realm; got != "keep.example.com" {
		t.Fatalf("live realm = %q, want keep.example.com", got)
	}
}

func TestBootstrap_RPC_CfgReloadEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	writeBootConfig(t, path, "rpc1.example.com", "info")

	boot, err := NewBootstrap(BootstrapOptions{ConfigFile: path, RPCAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	defer boot.Shutdown()

	// Wait for the RPC listener to bind.
	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		addr = boot.RPCServer.Addr()
		if addr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("RPC server never bound")
	}

	// Rewrite config & invoke cfg.reload over JSON-RPC.
	writeBootConfig(t, path, "rpc2.example.com", "debug")
	c := rpc.NewClient("http://"+addr+"/rpc", 5*time.Second)
	res, err := c.Call("kamailio.cfg.reload")
	if err != nil {
		t.Fatalf("cfg.reload: %v", err)
	}
	m := res.(map[string]interface{})
	if m["ok"] != true {
		t.Fatalf("cfg.reload ok=false: %v", m)
	}
	if m["realm"] != "rpc2.example.com" {
		t.Fatalf("cfg.reload realm = %v, want rpc2.example.com", m["realm"])
	}

	// cfg.snapshot should also reflect the new realm.
	res, err = c.Call("kamailio.cfg.snapshot")
	if err != nil {
		t.Fatalf("cfg.snapshot: %v", err)
	}
	m = res.(map[string]interface{})
	if m["realm"] != "rpc2.example.com" {
		t.Fatalf("cfg.snapshot realm = %v, want rpc2.example.com", m["realm"])
	}

	// cfg.list should expose the seeded defaults.
	res, err = c.Call("kamailio.cfg.list")
	if err != nil {
		t.Fatalf("cfg.list: %v", err)
	}
	m = res.(map[string]interface{})
	entries := m["entries"].([]interface{})
	found := false
	for _, e := range entries {
		em := e.(map[string]interface{})
		if em["key"] == "core.realm" && em["value"] == "rpc2.example.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cfg.list did not contain core.realm=rpc2.example.com: %v", entries)
	}
}

// Ensure the boot RPC server can be pinged via plain HTTP too.
func TestBootstrap_RPC_PingEndpoint(t *testing.T) {
	boot, err := NewBootstrap(BootstrapOptions{RPCAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("NewBootstrap: %v", err)
	}
	defer boot.Shutdown()

	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		addr = boot.RPCServer.Addr()
		if addr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("RPC server never bound")
	}
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
}
