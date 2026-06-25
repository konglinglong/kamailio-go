// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - xhttp_pi module tests.
 *
 * These tests exercise the HTTP provisioning interface against an
 * in-memory config tree, including the HTTP handler and concurrent access.
 */

package xhttp_pi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Listen != "127.0.0.1:8082" {
		t.Errorf("listen = %q", cfg.Listen)
	}
	if cfg.RootDir != "/pi" {
		t.Errorf("root dir = %q", cfg.RootDir)
	}
}

func TestConfigValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Errorf("default config: %v", err)
	}
	if err := (&Config{}).Validate(); err == nil {
		t.Error("empty config expected error")
	}
}

func TestSetters(t *testing.T) {
	m := New()
	m.SetListen("0.0.0.0:9090")
	m.SetRootDir("/admin")
	cfg := m.Config()
	if cfg.Listen != "0.0.0.0:9090" {
		t.Errorf("listen = %q", cfg.Listen)
	}
	if cfg.RootDir != "/admin" {
		t.Errorf("root dir = %q", cfg.RootDir)
	}
}

func TestAddModuleAndParam(t *testing.T) {
	m := New()
	mod := m.AddModule("corex")
	mod.AddParam("alias_subdomains", "1")
	mod.AddParam("debug", "0")
	mods := m.ListModules()
	if len(mods) != 1 || mods[0].Name != "corex" {
		t.Fatalf("modules = %v", mods)
	}
	if len(mods[0].Params) != 2 {
		t.Fatalf("params = %d, want 2", len(mods[0].Params))
	}
}

func TestGetParameter(t *testing.T) {
	m := New()
	mod := m.AddModule("tm")
	mod.AddParam("fr_timer", "5")
	v, err := m.GetParameter("root.tm.fr_timer")
	if err != nil {
		t.Fatalf("GetParameter: %v", err)
	}
	if v != "5" {
		t.Errorf("value = %q, want 5", v)
	}
}

func TestGetParameterNotFound(t *testing.T) {
	m := New()
	if _, err := m.GetParameter("root.no.such"); err == nil {
		t.Error("GetParameter(missing) expected error")
	}
}

func TestSetParameter(t *testing.T) {
	m := New()
	mod := m.AddModule("tm")
	mod.AddParam("fr_timer", "5")
	if err := m.SetParameter("root.tm.fr_timer", "10"); err != nil {
		t.Fatalf("SetParameter: %v", err)
	}
	v, _ := m.GetParameter("root.tm.fr_timer")
	if v != "10" {
		t.Errorf("value = %q, want 10", v)
	}
}

func TestSetParameterNotParam(t *testing.T) {
	m := New()
	m.AddModule("tm")
	// Setting a module node (not a param) should fail.
	if err := m.SetParameter("root.tm", "x"); err == nil {
		t.Error("SetParameter on module expected error")
	}
}

func TestSetParameterNotFound(t *testing.T) {
	m := New()
	if err := m.SetParameter("root.no.such", "x"); err == nil {
		t.Error("SetParameter(missing) expected error")
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "framework.txt")
	content := "section global\n  param debug 1\n  param loglevel 3\nmodule corex\n  param alias 1\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := New()
	if err := m.LoadConfigFile(path); err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	v, err := m.GetParameter("root.global.debug")
	if err != nil {
		t.Fatalf("GetParameter: %v", err)
	}
	if v != "1" {
		t.Errorf("debug = %q, want 1", v)
	}
	mods := m.ListModules()
	if len(mods) != 1 || mods[0].Name != "corex" {
		t.Errorf("modules = %v", mods)
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	m := New()
	if err := m.LoadConfigFile("/no/such/file"); err == nil {
		t.Error("LoadConfigFile(missing) expected error")
	}
	if err := m.LoadConfigFile(""); err == nil {
		t.Error("LoadConfigFile('') expected error")
	}
}

func TestGetConfigTree(t *testing.T) {
	m := New()
	mod := m.AddModule("sl")
	mod.AddParam("p", "v")
	tree := m.GetConfigTree()
	if tree == nil || tree.Type != NodeRoot {
		t.Fatalf("tree = %v", tree)
	}
	// Mutating the clone must not affect the module.
	tree.Children[0].Name = "mutated"
	mods := m.ListModules()
	if mods[0].Name == "mutated" {
		t.Error("clone leaked mutation to module")
	}
}

func TestServeHTTPGetTree(t *testing.T) {
	m := New()
	m.AddModule("tm").AddParam("fr_timer", "5")
	srv := httptest.NewServer(m)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var tree ConfigNode
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if tree.Type != NodeRoot {
		t.Errorf("tree type = %q, want root", tree.Type)
	}
}

func TestServeHTTPGetParam(t *testing.T) {
	m := New()
	m.AddModule("tm").AddParam("fr_timer", "5")
	srv := httptest.NewServer(m)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/?get=root.tm.fr_timer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out["value"] != "5" {
		t.Errorf("value = %q, want 5", out["value"])
	}
}

func TestServeHTTPGetParamMissing(t *testing.T) {
	m := New()
	srv := httptest.NewServer(m)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/?get=root.no.such")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServeHTTPListModules(t *testing.T) {
	m := New()
	m.AddModule("tm").AddParam("fr_timer", "5")
	m.AddModule("sl")
	srv := httptest.NewServer(m)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/?list=modules")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	var mods []ModuleInfo
	if err := json.NewDecoder(resp.Body).Decode(&mods); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(mods) != 2 {
		t.Errorf("modules = %d, want 2", len(mods))
	}
}

func TestServeHTTPPostSet(t *testing.T) {
	m := New()
	m.AddModule("tm").AddParam("fr_timer", "5")
	srv := httptest.NewServer(m)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/?set=root.tm.fr_timer&value=20", "application/json", nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	v, _ := m.GetParameter("root.tm.fr_timer")
	if v != "20" {
		t.Errorf("after POST, value = %q, want 20", v)
	}
}

func TestServeHTTPMethodNotAllowed(t *testing.T) {
	m := New()
	srv := httptest.NewServer(m)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestInitResetsTree(t *testing.T) {
	m := New()
	m.AddModule("tm")
	cfg := *DefaultConfig()
	if err := m.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(m.ListModules()) != 0 {
		t.Errorf("Init did not reset tree: %v", m.ListModules())
	}
}

func TestDefaultAndInit(t *testing.T) {
	cfg := *DefaultConfig()
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d := DefaultXHTTPPI()
	if d == nil {
		t.Fatal("DefaultXHTTPPI nil")
	}
	d.AddModule("pkg").AddParam("p", "1")
	v, err := GetParameter("root.pkg.p")
	if err != nil || v != "1" {
		t.Errorf("GetParameter = %q %v", v, err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	mod := m.AddModule("tm")
	for i := 0; i < 10; i++ {
		mod.AddParam("p"+itoa(i), "0")
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			path := "root.tm.p" + itoa(i%10)
			if err := m.SetParameter(path, itoa(i)); err != nil {
				t.Errorf("SetParameter %d: %v", i, err)
			}
			if _, err := m.GetParameter(path); err != nil {
				t.Errorf("GetParameter %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if got := m.RequestCount(); got != 0 {
		// No HTTP requests in this test; counter stays 0.
		t.Errorf("RequestCount = %d, want 0", got)
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
