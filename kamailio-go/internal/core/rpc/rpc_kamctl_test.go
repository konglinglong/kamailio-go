// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * End-to-end tests for the kamctl-backed RPC methods (ul.dump /
 * ul.lookup / ul.rm) and the shared JSON-RPC Client.
 *
 * These tests start a real rpc.Server on a kernel-allocated port,
 * point a rpc.Client at it, and assert the round-trip behaviour that
 * kamctl (and kamcmd) rely on.
 */

package rpc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/registrar"
	"github.com/kamailio/kamailio-go/internal/core/usrloc"
)

// registerOneAOR creates a registrar with one domain and one AOR
// carrying a single contact. Useful for ul.* round-trip tests.
func registerOneAOR(t *testing.T, domain, aor, contactURI string) *registrar.Registrar {
	t.Helper()
	r := registrar.New(nil)
	d := r.Domain(domain)
	_, _ = d.AddContact(aor, &usrloc.Contact{
		AOR:     aor,
		URI:     contactURI,
		Expires: time.Now().Add(1 * time.Hour),
		Q:       1.0,
	})
	return r
}

// startRPCServer starts an httptest.Server that fronts a rpc.Server
// wired to the given registrar. The server is automatically cleaned up
// when the test ends. Returns the server and the full /rpc URL the
// client should target.
func startRPCServer(t *testing.T, r *registrar.Registrar) (*Server, string) {
	t.Helper()
	srv := NewExtended(ServerConfig{Usrloc: r})
	hs := httptest.NewServer(srv.handler)
	t.Cleanup(hs.Close)
	return srv, hs.URL + "/rpc"
}

func TestClient_ULDump_Empty(t *testing.T) {
	r := registrar.New(nil)
	_, addr := startRPCServer(t, r)

	c := NewClient(addr, 5*time.Second)
	res, err := c.Call("kamailio.ul.dump")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if m["ok"] != true {
		t.Errorf("ok = %v, want true", m["ok"])
	}
	doms, ok := m["domains"].([]interface{})
	if !ok {
		t.Fatalf("domains type %T", m["domains"])
	}
	if len(doms) != 0 {
		t.Errorf("expected 0 domains for empty registrar, got %d", len(doms))
	}
}

func TestClient_ULDump_OneAOR(t *testing.T) {
	r := registerOneAOR(t, "example.com", "sip:alice@example.com", "sip:alice@10.0.0.1:5060")
	_, addr := startRPCServer(t, r)

	c := NewClient(addr, 5*time.Second)
	res, err := c.Call("kamailio.ul.dump")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m := res.(map[string]interface{})
	doms := m["domains"].([]interface{})
	if len(doms) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(doms))
	}
	dom := doms[0].(map[string]interface{})
	if dom["name"] != "example.com" {
		t.Errorf("domain name = %v, want example.com", dom["name"])
	}
	aors := dom["aors"].([]interface{})
	if len(aors) != 1 {
		t.Fatalf("expected 1 aor, got %d", len(aors))
	}
	aor := aors[0].(map[string]interface{})
	if aor["aor"] != "sip:alice@example.com" {
		t.Errorf("aor = %v", aor["aor"])
	}
	contacts := aor["contacts"].([]interface{})
	if len(contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(contacts))
	}
	contact := contacts[0].(map[string]interface{})
	if contact["uri"] != "sip:alice@10.0.0.1:5060" {
		t.Errorf("contact uri = %v", contact["uri"])
	}
}

func TestClient_ULLookup_HitAndMiss(t *testing.T) {
	r := registerOneAOR(t, "example.com", "sip:alice@example.com", "sip:alice@10.0.0.1:5060")
	_, addr := startRPCServer(t, r)

	c := NewClient(addr, 5*time.Second)

	// Hit.
	res, err := c.Call("kamailio.ul.lookup", "example.com", "sip:alice@example.com")
	if err != nil {
		t.Fatalf("lookup hit: %v", err)
	}
	m := res.(map[string]interface{})
	if m["found"] != true {
		t.Errorf("found = %v, want true", m["found"])
	}
	if m["aor"] != "sip:alice@example.com" {
		t.Errorf("aor = %v", m["aor"])
	}

	// Miss.
	res, err = c.Call("kamailio.ul.lookup", "example.com", "sip:bob@example.com")
	if err != nil {
		t.Fatalf("lookup miss: %v", err)
	}
	m = res.(map[string]interface{})
	if m["found"] != false {
		t.Errorf("found = %v, want false for missing AOR", m["found"])
	}
}

func TestClient_ULRM_RemovesAOR(t *testing.T) {
	r := registerOneAOR(t, "example.com", "sip:alice@example.com", "sip:alice@10.0.0.1:5060")
	_, addr := startRPCServer(t, r)

	c := NewClient(addr, 5*time.Second)

	// Remove.
	res, err := c.Call("kamailio.ul.rm", "example.com", "sip:alice@example.com")
	if err != nil {
		t.Fatalf("rm: %v", err)
	}
	m := res.(map[string]interface{})
	if m["removed"] != true {
		t.Errorf("removed = %v, want true", m["removed"])
	}

	// Subsequent lookup must miss.
	res, _ = c.Call("kamailio.ul.lookup", "example.com", "sip:alice@example.com")
	m = res.(map[string]interface{})
	if m["found"] != false {
		t.Errorf("found = %v after rm, want false", m["found"])
	}

	// Removing again returns removed=false.
	res, _ = c.Call("kamailio.ul.rm", "example.com", "sip:alice@example.com")
	m = res.(map[string]interface{})
	if m["removed"] != false {
		t.Errorf("second rm removed = %v, want false", m["removed"])
	}
}

func TestClient_ULRM_MissingRegistrar(t *testing.T) {
	// Server with no registrar wired up: must surface a JSON-RPC error.
	srv := NewExtended(ServerConfig{})
	hs := httptest.NewServer(srv.handler)
	t.Cleanup(hs.Close)

	c := NewClient(hs.URL+"/rpc", 5*time.Second)
	_, err := c.Call("kamailio.ul.rm", "example.com", "sip:alice@example.com")
	if err == nil {
		t.Fatal("expected error when usrloc not wired up")
	}
	if !strings.Contains(err.Error(), "not wired up") {
		t.Errorf("error = %v, want it to mention 'not wired up'", err)
	}
}

func TestClient_ParamsRoundTrip(t *testing.T) {
	// Use a hand-rolled HTTP handler that echoes the request so we can
	// verify positional params survive the round trip exactly.
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ClientRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0", "id": req.ID, "result": req.Params,
		})
	}))
	t.Cleanup(hs.Close)

	c := NewClient(hs.URL, 5*time.Second)
	res, err := c.Call("echo", "alice", "example.com", 42)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	params, ok := res.([]interface{})
	if !ok || len(params) != 3 {
		t.Fatalf("params = %v (%T)", res, res)
	}
	if params[0] != "alice" || params[1] != "example.com" {
		t.Errorf("string params = %v %v", params[0], params[1])
	}
	if params[2] != float64(42) {
		t.Errorf("int param = %v, want 42", params[2])
	}
}

func TestNormalizeServer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"", DefaultClientServer, false},
		{"localhost:2048", "http://localhost:2048", false},
		{"http://localhost:2048", "http://localhost:2048", false},
		{"https://host:2048", "https://host:2048", false},
		{"tcp://host:2048", "tcp://host:2048", false},
		{"tcp:host:2048", "tcp://host:2048", false},
		{"unix:/tmp/kamailio.sock", "unix:/tmp/kamailio.sock", false},
		{"ftp://host", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeServer(c.in)
		if c.err {
			if err == nil {
				t.Errorf("NormalizeServer(%q) = %q; want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeServer(%q) err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeServer(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestPrintResult(t *testing.T) {
	var sb strings.Builder
	PrintResult(&sb, map[string]interface{}{"status": "ok"}, "json")
	out := sb.String()
	if !strings.Contains(out, `"status": "ok"`) {
		t.Errorf("json output = %q", out)
	}

	sb.Reset()
	PrintResult(&sb, "pong", "text")
	if got := sb.String(); !strings.Contains(got, "pong") {
		t.Errorf("text output = %q", got)
	}
}
