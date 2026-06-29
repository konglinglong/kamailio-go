// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - kamcmd CLI tests.
 *
 * Mirrors the C tool in utils/kamcmd/kamcmd.c, exercising the JSON-RPC
 * request building, server parsing, execution against a mock JSON-RPC
 * endpoint (HTTP and raw TCP/FIFO) and result printing.
 */
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockJSONRPC starts an HTTP server speaking JSON-RPC 2.0. It echoes the
// received method/params back as the result so tests can verify round-trip.
func mockJSONRPC(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "core.ping":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": "pong",
			})
		case "core.version":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{"version": "5.8.0"},
			})
		case "htable.list":
			// Echo params back as the result so the caller can verify them.
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": req.Params,
			})
		case "core.error":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]interface{}{
					"code": -32601, "message": "method not found",
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": "ok",
			})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestExecute verifies JSON-RPC execution over HTTP.
func TestExecute(t *testing.T) {
	srv := mockJSONRPC(t)

	// Simple command returning a string result.
	res, err := Execute(srv.URL, "core.ping")
	if err != nil {
		t.Fatalf("Execute(ping): %v", err)
	}
	if res != "pong" {
		t.Errorf("Execute(ping) = %v; want pong", res)
	}

	// Command returning a structured result.
	res, err = Execute(srv.URL, "core.version")
	if err != nil {
		t.Fatalf("Execute(version): %v", err)
	}
	m, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("Execute(version) result type %T", res)
	}
	if m["version"] != "5.8.0" {
		t.Errorf("version = %v; want 5.8.0", m["version"])
	}

	// Command with positional params; server echoes them back.
	res, err = Execute(srv.URL, "htable.list", "mytable", 7)
	if err != nil {
		t.Fatalf("Execute(htable.list): %v", err)
	}
	params, ok := res.([]interface{})
	if !ok || len(params) != 2 {
		t.Fatalf("params = %v (%T)", res, res)
	}
	if params[0] != "mytable" {
		t.Errorf("param[0] = %v; want mytable", params[0])
	}
	if params[1] != float64(7) {
		t.Errorf("param[1] = %v; want 7", params[1])
	}

	// JSON-RPC error response surfaces as a Go error.
	_, err = Execute(srv.URL, "core.error")
	if err == nil {
		t.Fatal("Execute(core.error) should return an error")
	}
	if !strings.Contains(err.Error(), "method not found") {
		t.Errorf("error = %v; want it to mention 'method not found'", err)
	}

	// Unsupported scheme is rejected by ParseServer within Execute.
	if _, err := Execute("ftp://bad", "ping"); err == nil {
		t.Error("Execute with unsupported scheme should error")
	}
}

// TestExecuteTCP verifies JSON-RPC execution over a raw TCP (FIFO) socket.
func TestExecuteTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		var req Request
		if json.Unmarshal([]byte(line), &req) == nil && req.Method == "core.ping" {
			out, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID, "result": "pong",
			})
			conn.Write(out)
			conn.Write([]byte("\n"))
		}
	}()

	res, err := Execute("tcp://"+ln.Addr().String(), "core.ping")
	if err != nil {
		t.Fatalf("ExecuteTCP: %v", err)
	}
	if res != "pong" {
		t.Errorf("ExecuteTCP result = %v; want pong", res)
	}
}

// TestParseServer verifies server address normalisation.
func TestParseServer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"", DefaultServer, false},
		{"localhost:2048", "http://localhost:2048", false},
		{"http://localhost:2048", "http://localhost:2048", false},
		{"https://host:2048", "https://host:2048", false},
		{"tcp://host:2048", "tcp://host:2048", false},
		{"tcp:host:2048", "tcp://host:2048", false},
		{"unix:/tmp/kamailio.sock", "unix:/tmp/kamailio.sock", false},
		{"ftp://host", "", true},
	}
	for _, c := range cases {
		got, err := ParseServer(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseServer(%q) = %q; want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseServer(%q) err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseServer(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

// TestBuildRequest verifies JSON-RPC 2.0 request construction.
func TestBuildRequest(t *testing.T) {
	req := BuildRequest("core.ping")
	if req.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q; want 2.0", req.JSONRPC)
	}
	if req.Method != "core.ping" {
		t.Errorf("Method = %q; want core.ping", req.Method)
	}
	if req.ID <= 0 {
		t.Errorf("ID = %d; want > 0", req.ID)
	}
	if req.Params != nil {
		t.Errorf("Params = %v; want nil for no params", req.Params)
	}
	// Marshals to valid JSON-RPC.
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"jsonrpc":"2.0"`) {
		t.Errorf("marshalled request missing jsonrpc: %s", b)
	}

	// With positional params.
	req2 := BuildRequest("htable.list", "t1", 5)
	if req2.Method != "htable.list" {
		t.Errorf("Method = %q", req2.Method)
	}
	if req2.ID <= req.ID {
		t.Errorf("ID should increase; got %d after %d", req2.ID, req.ID)
	}
	params, ok := req2.Params.([]interface{})
	if !ok {
		t.Fatalf("Params type %T", req2.Params)
	}
	if len(params) != 2 || params[0] != "t1" || params[1] != 5 {
		t.Errorf("Params = %v; want [t1 5]", params)
	}
	// Params marshal as a JSON array.
	b2, _ := json.Marshal(req2)
	if !strings.Contains(string(b2), `"params":["t1",5]`) {
		t.Errorf("marshalled params unexpected: %s", b2)
	}
}

// TestPrintResult verifies pretty printing in json and text formats.
func TestPrintResult(t *testing.T) {
	oldOut := Output
	oldFmt := Format
	defer func() {
		Output = oldOut
		Format = oldFmt
	}()

	var buf bytes.Buffer
	Output = &buf
	Format = "json"

	PrintResult(map[string]interface{}{"status": "ok", "count": 3})
	out := buf.String()
	if !strings.Contains(out, `"status": "ok"`) {
		t.Errorf("json output missing pretty field: %q", out)
	}
	if !strings.Contains(out, "\n") {
		t.Errorf("json output should be indented (multi-line): %q", out)
	}

	// Text format prints the value directly.
	buf.Reset()
	Format = "text"
	PrintResult("pong")
	if got := buf.String(); !strings.Contains(got, "pong") {
		t.Errorf("text output = %q; want to contain pong", got)
	}

	// JSON format of a slice.
	buf.Reset()
	Format = "json"
	PrintResult([]string{"a", "b", "c"})
	if got := buf.String(); !strings.Contains(got, `"a"`) || !strings.Contains(got, `"c"`) {
		t.Errorf("json slice output = %q", got)
	}
}

// TestExecuteConcurrent exercises Execute under the race detector.
func TestExecuteConcurrent(t *testing.T) {
	srv := mockJSONRPC(t)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 20; j++ {
				_, _ = Execute(srv.URL, "core.ping")
				_ = BuildRequest(fmt.Sprintf("m.%d", j), j)
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
