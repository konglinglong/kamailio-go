// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - ctl module tests.
 *
 * These tests exercise command registration / dispatch, JSON-RPC handling
 * over a net.Conn, and the FIFO + Unix socket listeners. No external
 * dependencies are required.
 */

package ctl

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestInit verifies Init stores the listener configuration.
func TestInit(t *testing.T) {
	m := New()
	if err := m.Init("/tmp/ctl.fifo", "/tmp/ctl.sock", 0660); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.FIFOName() != "/tmp/ctl.fifo" {
		t.Errorf("fifo = %q, want /tmp/ctl.fifo", m.FIFOName())
	}
	if m.SockName() != "/tmp/ctl.sock" {
		t.Errorf("sock = %q, want /tmp/ctl.sock", m.SockName())
	}
	if m.SockMode() != 0660 {
		t.Errorf("mode = %o, want 0660", m.SockMode())
	}
}

// TestRegisterAndExecute verifies a command can be registered and executed.
func TestRegisterAndExecute(t *testing.T) {
	m := New()
	if err := m.RegisterCommand("echo", func(p map[string]string) (interface{}, error) {
		return p["msg"], nil
	}); err != nil {
		t.Fatalf("RegisterCommand: %v", err)
	}
	res, err := m.ExecuteCommand("echo", map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}
	if res != "hello" {
		t.Errorf("result = %v, want hello", res)
	}
}

// TestRegisterDuplicate verifies registering the same command twice fails.
func TestRegisterDuplicate(t *testing.T) {
	m := New()
	if err := m.RegisterCommand("dup", func(p map[string]string) (interface{}, error) { return nil, nil }); err != nil {
		t.Fatalf("first RegisterCommand: %v", err)
	}
	if err := m.RegisterCommand("dup", func(p map[string]string) (interface{}, error) { return nil, nil }); err == nil {
		t.Error("duplicate RegisterCommand: expected error, got nil")
	}
}

// TestExecuteUnknownCommand verifies executing an unregistered command errors.
func TestExecuteUnknownCommand(t *testing.T) {
	m := New()
	if _, err := m.ExecuteCommand("nope", nil); err == nil {
		t.Error("ExecuteCommand: expected error for unknown command, got nil")
	}
}

// TestListCommands verifies registered commands are listed.
func TestListCommands(t *testing.T) {
	m := New()
	_ = m.RegisterCommand("a", func(p map[string]string) (interface{}, error) { return nil, nil })
	_ = m.RegisterCommand("b", func(p map[string]string) (interface{}, error) { return nil, nil })
	cmds := m.ListCommands()
	if len(cmds) != 2 {
		t.Fatalf("len = %d, want 2", len(cmds))
	}
}

// TestHandleConnection verifies JSON-RPC dispatch over a net.Conn.
func TestHandleConnection(t *testing.T) {
	m := New()
	_ = m.RegisterCommand("echo", func(p map[string]string) (interface{}, error) {
		return p["msg"], nil
	})

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go m.HandleConnection(serverConn)

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "echo",
		"params":  map[string]string{"msg": "hi"},
		"id":      float64(1),
	}
	b, _ := json.Marshal(req)
	if _, err := clientConn.Write(b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var resp struct {
		JSONRPC string      `json:"jsonrpc"`
		Result  interface{} `json:"result"`
		ID      interface{}  `json:"id"`
	}
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Result != "hi" {
		t.Errorf("result = %v, want hi", resp.Result)
	}
}

// TestHandleConnectionUnknownMethod verifies an unknown method yields a
// JSON-RPC error response.
func TestHandleConnectionUnknownMethod(t *testing.T) {
	m := New()
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go m.HandleConnection(serverConn)

	req := map[string]interface{}{"jsonrpc": "2.0", "method": "missing", "id": float64(2)}
	b, _ := json.Marshal(req)
	if _, err := clientConn.Write(b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code == 0 {
		t.Error("expected non-zero error code")
	}
}

// TestStartStopUnixSocket verifies a Unix socket listener accepts a client,
// dispatches a command, and can be stopped.
func TestStartStopUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "ctl.sock")

	m := New()
	_ = m.RegisterCommand("ping", func(p map[string]string) (interface{}, error) {
		return "pong", nil
	})
	if err := m.Init("", sockPath, 0660); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req := map[string]interface{}{"jsonrpc": "2.0", "method": "ping", "id": float64(1)}
	b, _ := json.Marshal(req)
	if _, err := conn.Write(b); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var resp struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if resp.Result != "pong" {
		t.Errorf("result = %q, want pong", resp.Result)
	}
}

// TestStartStopFIFO verifies a FIFO listener reads a request and dispatches
// the command. The response is verified via a side-effect channel to avoid
// FIFO read-timing races.
func TestStartStopFIFO(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "ctl.fifo")

	m := New()
	done := make(chan string, 1)
	_ = m.RegisterCommand("who", func(p map[string]string) (interface{}, error) {
		done <- p["user"]
		return "ok", nil
	})
	if err := m.Init(fifoPath, "", 0660); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// Wait for the FIFO file to exist.
	if !waitForFile(fifoPath, time.Second) {
		t.Fatal("FIFO file was not created")
	}

	// Client writes a JSON-RPC request to the FIFO.
	req := `{"jsonrpc":"2.0","method":"who","params":{"user":"alice"},"id":1}` + "\n"
	if err := writeFIFO(fifoPath, req); err != nil {
		t.Fatalf("writeFIFO: %v", err)
	}

	select {
	case user := <-done:
		if user != "alice" {
			t.Errorf("user = %q, want alice", user)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for FIFO command dispatch")
	}
}

// TestStopWhenNotStarted verifies Stop is a no-op when nothing is running.
func TestStopWhenNotStarted(t *testing.T) {
	m := New()
	if err := m.Stop(); err != nil {
		t.Errorf("Stop when not started: %v", err)
	}
}

// TestConcurrentRegister exercises concurrent command registration to
// surface data races when run with -race.
func TestConcurrentRegister(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			name := "cmd" + strings.Repeat("x", i%4) + itoa(i)
			_ = m.RegisterCommand(name, func(p map[string]string) (interface{}, error) {
				return i, nil
			})
			_, _ = m.ExecuteCommand(name, nil)
		}()
	}
	wg.Wait()
	if len(m.ListCommands()) != 50 {
		t.Errorf("registered = %d, want 50", len(m.ListCommands()))
	}
}

// --- helpers ---

func writeFIFO(path, data string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.WriteString(f, data)
	return err
}

func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
