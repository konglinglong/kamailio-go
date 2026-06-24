// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the xcap_server module - in-memory XCAP document store.
 */
package xcap_server

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// freePort returns a TCP port that is currently free on localhost.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// noProxyClient returns an HTTP client that bypasses any configured
// proxy, so tests against local servers are not routed through a proxy.
func noProxyClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil}}
}

// waitForReady polls addr until a TCP connection succeeds or times out.
func waitForReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
	}
	t.Fatalf("server at %s not ready", addr)
}

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&XCAPServerConfig{ListenAddr: ":0", DBDriver: "memory"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.listenAddr != ":0" {
		t.Errorf("listenAddr = %q", m.listenAddr)
	}
	if m.dbDriver != "memory" {
		t.Errorf("dbDriver = %q", m.dbDriver)
	}
}

func TestStoreGetDeleteDocument(t *testing.T) {
	m := New()
	body := []byte("<resource-lists/>")
	if err := m.StoreDocument("resource-lists", "index", "alice", body); err != nil {
		t.Fatalf("StoreDocument: %v", err)
	}
	got, err := m.GetDocument("resource-lists", "index", "alice")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("GetDocument = %q, want %q", got, body)
	}
	// Mutating the returned slice must not affect the store.
	got[0] = 'X'
	got2, _ := m.GetDocument("resource-lists", "index", "alice")
	if got2[0] != '<' {
		t.Errorf("store mutated by caller: %q", got2)
	}
	if err := m.DeleteDocument("resource-lists", "index", "alice"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if _, err := m.GetDocument("resource-lists", "index", "alice"); err == nil {
		t.Error("GetDocument after delete should error")
	}
}

func TestListDocuments(t *testing.T) {
	m := New()
	_ = m.StoreDocument("resource-lists", "index", "alice", []byte("a"))
	_ = m.StoreDocument("resource-lists", "index", "bob", []byte("b"))
	_ = m.StoreDocument("rls-services", "index", "carol", []byte("c"))
	got := m.ListDocuments("resource-lists")
	if len(got) != 2 {
		t.Fatalf("ListDocuments = %v, want 2 entries", got)
	}
	if len(m.ListDocuments("rls-services")) != 1 {
		t.Errorf("ListDocuments rls-services = %v", m.ListDocuments("rls-services"))
	}
	if len(m.ListDocuments("none")) != 0 {
		t.Errorf("ListDocuments none should be empty")
	}
}

func TestStartStop(t *testing.T) {
	m := New()
	port := freePort(t)
	_ = m.Init(&XCAPServerConfig{ListenAddr: "127.0.0.1:0"})
	m.listenAddr = "127.0.0.1:" + strconv.Itoa(port)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Start(); err == nil {
		t.Error("second Start should error")
	}
	m.Stop()
}

func TestHTTPServe(t *testing.T) {
	m := New()
	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	_ = m.Init(&XCAPServerConfig{ListenAddr: addr})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()
	waitForReady(t, addr)
	client := noProxyClient()
	base := "http://" + addr
	// PUT a document over HTTP.
	req, _ := http.NewRequest(http.MethodPut,
		base+"/resource-lists/users/alice/index", strings.NewReader("<list/>"))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}
	// GET it back.
	resp2, err := client.Get(base + "/resource-lists/users/alice/index")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if !strings.Contains(string(body), "<list/>") {
		t.Errorf("GET body = %q", body)
	}
}

func TestDefaultAndInit(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d1 := DefaultXCAPServer()
	d2 := DefaultXCAPServer()
	if d1 != d2 {
		t.Error("DefaultXCAPServer should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.StoreDocument("a", "b", "u", []byte("x"))
			_, _ = m.GetDocument("a", "b", "u")
			_ = m.ListDocuments("a")
		}(i)
	}
	wg.Wait()
}
