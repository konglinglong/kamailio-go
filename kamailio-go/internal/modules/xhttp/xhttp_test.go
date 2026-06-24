// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - xhttp module tests.
 */

package xhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&XHTTPConfig{
		ListenAddr: "127.0.0.1:8080",
		Match:      "/http",
		Routes:     map[string]string{"/ping": "ping"},
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("ListenAddr = %q", m.cfg.ListenAddr)
	}
	if m.cfg.Match != "/http" {
		t.Errorf("Match = %q", m.cfg.Match)
	}
	if len(m.cfg.Routes) != 1 {
		t.Errorf("Routes = %v", m.cfg.Routes)
	}

	// nil config is accepted and applies defaults.
	if err := (&XHTTPModule{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestRegisterHandlerAndDispatch(t *testing.T) {
	m := New()
	m.RegisterHandler("/hello", func(req *XHTTPRequest) *XHTTPResponse {
		return &XHTTPResponse{
			Status:  200,
			Headers: map[string]string{"Content-Type": "text/plain"},
			Body:    "hi " + req.Query["name"],
		}
	})

	resp, err := m.HandleRequest(&XHTTPRequest{
		Method: "GET",
		Path:   "/hello",
		Query:  map[string]string{"name": "world"},
	})
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("Status = %d, want 200", resp.Status)
	}
	if resp.Body != "hi world" {
		t.Errorf("Body = %q, want %q", resp.Body, "hi world")
	}
	if resp.Headers["Content-Type"] != "text/plain" {
		t.Errorf("Content-Type = %q", resp.Headers["Content-Type"])
	}
}

func TestHandleRequestNotFound(t *testing.T) {
	m := New()
	resp, err := m.HandleRequest(&XHTTPRequest{Method: "GET", Path: "/nope"})
	if err != nil {
		t.Fatalf("HandleRequest() error = %v", err)
	}
	if resp.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", resp.Status)
	}
	if resp.Body == "" {
		t.Errorf("Body should not be empty for 404")
	}

	// nil request -> error.
	if _, err := m.HandleRequest(nil); err == nil {
		t.Errorf("HandleRequest(nil) should error")
	}
}

func TestRemoveHandler(t *testing.T) {
	m := New()
	m.RegisterHandler("/x", func(req *XHTTPRequest) *XHTTPResponse {
		return &XHTTPResponse{Status: 200, Body: "ok"}
	})
	resp, _ := m.HandleRequest(&XHTTPRequest{Path: "/x"})
	if resp.Status != 200 {
		t.Fatalf("Status = %d, want 200 before remove", resp.Status)
	}
	m.RemoveHandler("/x")
	resp, _ = m.HandleRequest(&XHTTPRequest{Path: "/x"})
	if resp.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404 after remove", resp.Status)
	}
	// Removing a non-existent handler is a no-op.
	m.RemoveHandler("/x")
}

func TestServeHTTPViaHttptest(t *testing.T) {
	m := New()
	m.RegisterHandler("/echo", func(req *XHTTPRequest) *XHTTPResponse {
		return &XHTTPResponse{
			Status:  201,
			Headers: map[string]string{"X-Echo": req.Headers["X-Msg"]},
			Body:    req.Body,
		}
	})

	srv := httptest.NewServer(m)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/echo?x=1", nil)
	req.Header.Set("X-Msg", "hello")
	req.Body = io.NopCloser(stringReader("payload"))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Echo"); got != "hello" {
		t.Errorf("X-Echo = %q, want hello", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "payload" {
		t.Errorf("body = %q, want payload", body)
	}

	// Unknown path through the real server -> 404.
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/missing", nil)
	resp2, err := srv.Client().Do(req2)
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
	m.RegisterHandler("/ping", func(req *XHTTPRequest) *XHTTPResponse {
		return &XHTTPResponse{Status: 200, Body: "pong"}
	})
	if err := m.Init(&XHTTPConfig{ListenAddr: "127.0.0.1:0"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !m.IsRunning() {
		t.Fatalf("IsRunning() = false, want true")
	}
	addr := m.listener.Addr().String()

	// Make a real request to the started server.
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
	// Stop is idempotent.
	m.Stop()
}

func TestStartBadAddress(t *testing.T) {
	m := New()
	if err := m.Init(&XHTTPConfig{ListenAddr: "127.0.0.1:1"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	// Port 1 is privileged and should fail to bind; tolerate either outcome.
	_ = m.Start()
	m.Stop()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultXHTTP() == nil {
		t.Fatalf("DefaultXHTTP() nil")
	}
	Init()
	d := DefaultXHTTP()
	if d == nil {
		t.Fatalf("DefaultXHTTP() nil after Init")
	}
	if d != DefaultXHTTP() {
		t.Fatalf("DefaultXHTTP() returned different instances")
	}
}

func TestConcurrentHandlers(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/p" + itoa(i)
			m.RegisterHandler(path, func(req *XHTTPRequest) *XHTTPResponse {
				return &XHTTPResponse{Status: 200, Body: path}
			})
			resp, err := m.HandleRequest(&XHTTPRequest{Path: path})
			if err != nil || resp.Status != 200 {
				t.Errorf("dispatch %s: resp=%+v err=%v", path, resp, err)
			}
			m.RemoveHandler(path)
		}(i)
	}
	wg.Wait()
}

// stringReader returns an io.Reader for a string.
func stringReader(s string) io.Reader {
	return &stringReaderType{s: s}
}

type stringReaderType struct {
	s   string
	pos int
}

func (r *stringReaderType) Read(p []byte) (int, error) {
	if r.pos >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.pos:])
	r.pos += n
	return n, nil
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
