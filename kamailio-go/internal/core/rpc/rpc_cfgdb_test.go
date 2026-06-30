// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the cfg_db (cfgdb.*) and cfgutils ($shv / $cnt)
 * RPC methods wired into the rpc.Server.
 */

package rpc

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/modules/cfg_db"
	"github.com/kamailio/kamailio-go/internal/modules/cfgutils"
)

// startCfgDBRPCServer wires a rpc.Server with the given cfg_db and
// cfgutils modules. Returns the server and the full /rpc URL.
func startCfgDBRPCServer(t *testing.T, db *cfg_db.CfgDBModule, cu *cfgutils.CfgUtilsModule) (*Server, string) {
	t.Helper()
	srv := NewExtended(ServerConfig{CfgDB: db, CfgUtils: cu})
	hs := httptest.NewServer(srv.handler)
	t.Cleanup(hs.Close)
	return srv, hs.URL + "/rpc"
}

// --- cfg_db (cfgdb.*) ---

func TestClient_CfgDB_StoreLoadDelete(t *testing.T) {
	db := cfg_db.New()
	_, addr := startCfgDBRPCServer(t, db, nil)
	c := NewClient(addr, 5*time.Second)

	// Load missing key.
	res, err := c.Call("kamailio.cfgdb.load", "missing")
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if res.(map[string]interface{})["found"] != false {
		t.Fatalf("expected found=false, got %v", res)
	}

	// Store.
	if _, err := c.Call("kamailio.cfgdb.store", "k", "v1"); err != nil {
		t.Fatalf("store: %v", err)
	}
	// Load.
	res, err = c.Call("kamailio.cfgdb.load", "k")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	m := res.(map[string]interface{})
	if m["found"] != true || m["value"] != "v1" {
		t.Fatalf("load = %v", m)
	}

	// Delete.
	if _, err := c.Call("kamailio.cfgdb.delete", "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	res, err = c.Call("kamailio.cfgdb.load", "k")
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if res.(map[string]interface{})["found"] != false {
		t.Fatalf("expected found=false after delete, got %v", res)
	}
}

func TestClient_CfgDB_List(t *testing.T) {
	db := cfg_db.New()
	_, addr := startCfgDBRPCServer(t, db, nil)
	c := NewClient(addr, 5*time.Second)

	if _, err := c.Call("kamailio.cfgdb.store", "a", "1"); err != nil {
		t.Fatalf("store a: %v", err)
	}
	if _, err := c.Call("kamailio.cfgdb.store", "b", "2"); err != nil {
		t.Fatalf("store b: %v", err)
	}

	res, err := c.Call("kamailio.cfgdb.list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m := res.(map[string]interface{})
	entries := m["entries"].([]interface{})
	got := map[string]string{}
	for _, e := range entries {
		em := e.(map[string]interface{})
		got[em["key"].(string)] = em["value"].(string)
	}
	if got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("list = %v", got)
	}
}

// --- cfgutils $shv (shv.*) ---

func TestClient_SHV_SetGet(t *testing.T) {
	cu := cfgutils.NewCfgUtilsModule()
	_, addr := startCfgDBRPCServer(t, nil, cu)
	c := NewClient(addr, 5*time.Second)

	// Get before set — exists=false, value="".
	res, err := c.Call("kamailio.shv.get", "var1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	m := res.(map[string]interface{})
	if m["exists"] != false {
		t.Fatalf("expected exists=false, got %v", m)
	}

	// Set.
	if _, err := c.Call("kamailio.shv.set", "var1", "hello"); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Get after set.
	res, err = c.Call("kamailio.shv.get", "var1")
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	m = res.(map[string]interface{})
	if m["exists"] != true || m["value"] != "hello" {
		t.Fatalf("get after set = %v", m)
	}
}

func TestClient_SHV_List(t *testing.T) {
	cu := cfgutils.NewCfgUtilsModule()
	cu.SetVar("x", "10")
	cu.SetVar("y", "20")
	_, addr := startCfgDBRPCServer(t, nil, cu)
	c := NewClient(addr, 5*time.Second)

	// List specific names.
	res, err := c.Call("kamailio.shv.list", "x", "y", "z")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m := res.(map[string]interface{})
	entries := m["entries"].([]interface{})
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	got := map[string]string{}
	for _, e := range entries {
		em := e.(map[string]interface{})
		got[em["name"].(string)] = em["value"].(string)
	}
	if got["x"] != "10" || got["y"] != "20" || got["z"] != "" {
		t.Fatalf("list = %v", got)
	}
}

// --- cfgutils $cnt (cnt.*) ---

func TestClient_CNT_SetGetIncReset(t *testing.T) {
	cu := cfgutils.NewCfgUtilsModule()
	_, addr := startCfgDBRPCServer(t, nil, cu)
	c := NewClient(addr, 5*time.Second)

	// Get before set — value=0.
	res, err := c.Call("kamailio.cnt.get", "reqs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if res.(map[string]interface{})["value"] != float64(0) {
		t.Fatalf("initial value = %v", res)
	}

	// Set.
	if _, err := c.Call("kamailio.cnt.set", "reqs", 100); err != nil {
		t.Fatalf("set: %v", err)
	}
	res, err = c.Call("kamailio.cnt.get", "reqs")
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if res.(map[string]interface{})["value"] != float64(100) {
		t.Fatalf("value after set = %v", res)
	}

	// Inc by default (+1).
	if _, err := c.Call("kamailio.cnt.inc", "reqs"); err != nil {
		t.Fatalf("inc: %v", err)
	}
	res, err = c.Call("kamailio.cnt.get", "reqs")
	if err != nil {
		t.Fatalf("get after inc: %v", err)
	}
	if res.(map[string]interface{})["value"] != float64(101) {
		t.Fatalf("value after inc = %v", res)
	}

	// Inc by explicit delta.
	if _, err := c.Call("kamailio.cnt.inc", "reqs", 9); err != nil {
		t.Fatalf("inc delta: %v", err)
	}
	res, err = c.Call("kamailio.cnt.get", "reqs")
	if err != nil {
		t.Fatalf("get after inc delta: %v", err)
	}
	if res.(map[string]interface{})["value"] != float64(110) {
		t.Fatalf("value after inc delta = %v", res)
	}

	// Reset.
	if _, err := c.Call("kamailio.cnt.reset", "reqs"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	res, err = c.Call("kamailio.cnt.get", "reqs")
	if err != nil {
		t.Fatalf("get after reset: %v", err)
	}
	if res.(map[string]interface{})["value"] != float64(0) {
		t.Fatalf("value after reset = %v", res)
	}
}

func TestClient_CNT_SetAcceptsStringAndFloat(t *testing.T) {
	cu := cfgutils.NewCfgUtilsModule()
	_, addr := startCfgDBRPCServer(t, nil, cu)
	c := NewClient(addr, 5*time.Second)

	// JSON numbers arrive as float64.
	if _, err := c.Call("kamailio.cnt.set", "n", 42.0); err != nil {
		t.Fatalf("set float: %v", err)
	}
	res, _ := c.Call("kamailio.cnt.get", "n")
	if res.(map[string]interface{})["value"] != float64(42) {
		t.Fatalf("float set = %v", res)
	}

	// String-encoded integer.
	if _, err := c.Call("kamailio.cnt.set", "n", "99"); err != nil {
		t.Fatalf("set string: %v", err)
	}
	res, _ = c.Call("kamailio.cnt.get", "n")
	if res.(map[string]interface{})["value"] != float64(99) {
		t.Fatalf("string set = %v", res)
	}
}

// --- Disabled when not wired ---

func TestClient_CfgDB_SHV_CNT_DisabledWhenNotWired(t *testing.T) {
	srv := NewExtended(ServerConfig{})
	hs := httptest.NewServer(srv.handler)
	t.Cleanup(hs.Close)
	c := NewClient(hs.URL+"/rpc", 5*time.Second)

	for _, method := range []string{
		"kamailio.cfgdb.load",
		"kamailio.cfgdb.store",
		"kamailio.cfgdb.delete",
		"kamailio.cfgdb.list",
		"kamailio.shv.get",
		"kamailio.shv.set",
		"kamailio.shv.list",
		"kamailio.cnt.get",
		"kamailio.cnt.set",
		"kamailio.cnt.inc",
		"kamailio.cnt.reset",
	} {
		if _, err := c.Call(method); err == nil {
			t.Errorf("%s: expected disabled error, got nil", method)
		}
	}
}
