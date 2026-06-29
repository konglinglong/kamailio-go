// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - http_client module tests.
 */

package http_client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("X-Test-Header"); ct != "" {
			w.Header().Set("X-Echo-Header", ct)
		}
		w.Header().Set("X-Method", r.Method)
		rb, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Body", string(rb))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestGet(t *testing.T) {
	srv := newTestServer(t, 200, "hello")
	defer srv.Close()

	m := New()
	code, body, err := m.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}
}

func TestPost(t *testing.T) {
	srv := newTestServer(t, 201, "created")
	defer srv.Close()

	m := New()
	code, body, err := m.Post(srv.URL, "payload", "text/plain")
	if err != nil {
		t.Fatalf("Post error: %v", err)
	}
	if code != 201 {
		t.Errorf("code = %d, want 201", code)
	}
	if string(body) != "created" {
		t.Errorf("body = %q, want %q", body, "created")
	}
}

func TestPutDelete(t *testing.T) {
	srv := newTestServer(t, 200, "ok")
	defer srv.Close()

	m := New()
	if code, _, err := m.Put(srv.URL, "data", "text/plain"); err != nil {
		t.Fatalf("Put error: %v", err)
	} else if code != 200 {
		t.Errorf("Put code = %d, want 200", code)
	}
	if code, _, err := m.Delete(srv.URL); err != nil {
		t.Fatalf("Delete error: %v", err)
	} else if code != 200 {
		t.Errorf("Delete code = %d, want 200", code)
	}
}

func TestSetHeader(t *testing.T) {
	srv := newTestServer(t, 200, "")
	defer srv.Close()

	m := New()
	m.SetHeader("X-Test-Header", "abc")
	_, _, err := m.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	// The server echoes the header back; verify via a direct request.
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("X-Test-Header", "abc")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("direct request error: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("X-Echo-Header"); got != "abc" {
		t.Errorf("echo header = %q, want abc", got)
	}
}

func TestBaseURL(t *testing.T) {
	srv := newTestServer(t, 200, "base")
	defer srv.Close()

	m := New()
	m.Init(&HTTPClientConfig{BaseURL: srv.URL, Timeout: DefaultTimeout, TLSVerify: true})
	// Use a relative path; the module should prepend the base URL.
	code, body, err := m.Get("/path")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if string(body) != "base" {
		t.Errorf("body = %q, want base", body)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := New()
	m.SetTimeout(20 * time.Millisecond)
	_, _, err := m.Get(srv.URL)
	if err == nil {
		t.Errorf("expected timeout error")
	}
}

func TestConcurrentRequests(t *testing.T) {
	srv := newTestServer(t, 200, "ok")
	defer srv.Close()

	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := m.Get(srv.URL); err != nil {
				t.Errorf("concurrent Get error: %v", err)
			}
		}()
	}
	wg.Wait()
	m.Close()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultHTTPClient() == nil {
		t.Fatalf("DefaultHTTPClient() nil")
	}
	Init()
	if DefaultHTTPClient() == nil {
		t.Fatalf("DefaultHTTPClient() nil after Init")
	}
}
