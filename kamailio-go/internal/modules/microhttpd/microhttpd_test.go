// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - microhttpd module tests.
 */

package microhttpd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&MicroHTTPConfig{
		ListenAddr:     "127.0.0.1:7070",
		MaxConnections: 100,
		Timeout:        5 * time.Second,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.ListenAddr != "127.0.0.1:7070" {
		t.Errorf("ListenAddr = %q", m.cfg.ListenAddr)
	}
	if m.cfg.MaxConnections != 100 {
		t.Errorf("MaxConnections = %d", m.cfg.MaxConnections)
	}
	if m.cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v", m.cfg.Timeout)
	}
	// nil config is accepted.
	if err := (&MicroHTTPModule{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestRegisterAndHandle(t *testing.T) {
	m := New()
	m.RegisterHandler("/upper", func(method, path, body string) (int, string) {
		return 200, strings.ToUpper(body)
	})

	code, respBody := m.handle("POST", "/upper", "hello")
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if respBody != "HELLO" {
		t.Errorf("body = %q, want HELLO", respBody)
	}
}

func TestHandleNotFound(t *testing.T) {
	m := New()
	code, body := m.handle("GET", "/missing", "")
	if code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
	if body == "" {
		t.Errorf("body should not be empty for 404")
	}
}

func TestRemoveHandler(t *testing.T) {
	m := New()
	m.RegisterHandler("/x", func(method, path, body string) (int, string) {
		return 200, "ok"
	})
	if code, _ := m.handle("GET", "/x", ""); code != 200 {
		t.Fatalf("code = %d, want 200 before remove", code)
	}
	m.RemoveHandler("/x")
	if code, _ := m.handle("GET", "/x", ""); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404 after remove", code)
	}
	m.RemoveHandler("/x")
}

func TestServeHTTPViaHttptest(t *testing.T) {
	m := New()
	m.RegisterHandler("/echo", func(method, path, body string) (int, string) {
		return 201, method + ":" + body
	})

	srv := httptest.NewServer(m)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/echo", "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "POST:payload" {
		t.Errorf("body = %q, want POST:payload", body)
	}

	// Unknown path -> 404.
	resp2, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp2.StatusCode)
	}
}

func TestStartStopLifecycle(t *testing.T) {
	m := New()
	m.RegisterHandler("/ping", func(method, path, body string) (int, string) {
		return 200, "pong"
	})
	if err := m.Init(&MicroHTTPConfig{ListenAddr: "127.0.0.1:0"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !m.IsRunning() {
		t.Fatalf("IsRunning() = false, want true")
	}
	addr := m.listener.Addr().String()

	resp, err := http.Get("http://" + addr + "/ping")
	if err != nil {
		t.Fatalf("http.Get error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("body = %q, want pong", body)
	}

	m.Stop()
	if m.IsRunning() {
		t.Errorf("IsRunning() = true, want false after Stop")
	}
	m.Stop()
}

func TestConnectionCount(t *testing.T) {
	m := New()
	release := make(chan struct{})
	entered := make(chan struct{})
	m.RegisterHandler("/slow", func(method, path, body string) (int, string) {
		entered <- struct{}{}
		<-release
		return 200, "done"
	})

	srv := httptest.NewServer(m)
	defer srv.Close()

	go func() {
		resp, err := http.Get(srv.URL + "/slow")
		if err != nil {
			t.Errorf("request error: %v", err)
			return
		}
		resp.Body.Close()
	}()

	<-entered
	// While the handler is blocked, exactly one connection is in flight.
	if got := m.ConnectionCount(); got != 1 {
		t.Errorf("ConnectionCount() = %d, want 1", got)
	}
	close(release)

	// Wait for the in-flight request to drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && m.ConnectionCount() != 0 {
		time.Sleep(time.Millisecond)
	}
	if got := m.ConnectionCount(); got != 0 {
		t.Errorf("ConnectionCount() = %d, want 0 after drain", got)
	}
}

func TestMaxConnectionsRejects(t *testing.T) {
	m := New()
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	m.RegisterHandler("/slow", func(method, path, body string) (int, string) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return 200, "ok"
	})
	if err := m.Init(&MicroHTTPConfig{ListenAddr: "127.0.0.1:0", MaxConnections: 1}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer m.Stop()
	addr := m.listener.Addr().String()

	// First request occupies the single slot.
	go func() { http.Get("http://" + addr + "/slow") }()
	<-entered

	// A second concurrent request must be rejected (503).
	resp, err := http.Get("http://" + addr + "/slow")
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (max connections)", resp.StatusCode)
	}
	close(release)
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultMicroHTTP() == nil {
		t.Fatalf("DefaultMicroHTTP() nil")
	}
	Init()
	d := DefaultMicroHTTP()
	if d == nil {
		t.Fatalf("DefaultMicroHTTP() nil after Init")
	}
	if d != DefaultMicroHTTP() {
		t.Fatalf("DefaultMicroHTTP() returned different instances")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/p" + itoa(i)
			m.RegisterHandler(path, func(method, p, body string) (int, string) {
				return 200, p
			})
			if code, _ := m.handle("GET", path, ""); code != 200 {
				t.Errorf("handle %s: code %d", path, code)
			}
			m.RemoveHandler(path)
		}(i)
	}
	wg.Wait()
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
