// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - http_async_client module tests.
 */

package http_async_client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newAsyncServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rb, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Method", r.Method)
		w.Header().Set("X-Body", string(rb))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestGetAsync(t *testing.T) {
	srv := newAsyncServer(t, 200, "hello")
	defer srv.Close()

	m := New()
	var got AsyncHTTPResult
	var wg sync.WaitGroup
	wg.Add(1)
	m.GetAsync(srv.URL, func(r *AsyncHTTPResult) {
		got = *r
		wg.Done()
	})
	wg.Wait()
	if got.Err != nil {
		t.Fatalf("GetAsync error: %v", got.Err)
	}
	if got.Status != 200 {
		t.Errorf("status = %d, want 200", got.Status)
	}
	if string(got.Body) != "hello" {
		t.Errorf("body = %q, want hello", got.Body)
	}
	if got.Duration <= 0 {
		t.Errorf("duration should be positive")
	}
}

func TestPostAsync(t *testing.T) {
	srv := newAsyncServer(t, 201, "created")
	defer srv.Close()

	m := New()
	var got AsyncHTTPResult
	var wg sync.WaitGroup
	wg.Add(1)
	m.PostAsync(srv.URL, "payload", func(r *AsyncHTTPResult) {
		got = *r
		wg.Done()
	})
	wg.Wait()
	if got.Err != nil {
		t.Fatalf("PostAsync error: %v", got.Err)
	}
	if got.Status != 201 {
		t.Errorf("status = %d, want 201", got.Status)
	}
	if string(got.Body) != "created" {
		t.Errorf("body = %q, want created", got.Body)
	}
}

func TestWaitAll(t *testing.T) {
	srv := newAsyncServer(t, 200, "ok")
	defer srv.Close()

	m := New()
	var ok int64
	for i := 0; i < 10; i++ {
		m.GetAsync(srv.URL, func(r *AsyncHTTPResult) {
			if r.Err == nil && r.Status == 200 {
				atomic.AddInt64(&ok, 1)
			}
		})
	}
	m.WaitAll()
	if atomic.LoadInt64(&ok) != 10 {
		t.Errorf("ok = %d, want 10", ok)
	}
	if m.PendingCount() != 0 {
		t.Errorf("pending after WaitAll = %d, want 0", m.PendingCount())
	}
}

func TestPendingCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := New()
	block := make(chan struct{})
	m.GetAsync(srv.URL, func(r *AsyncHTTPResult) {
		<-block
	})
	// Give the goroutine a moment to register.
	time.Sleep(20 * time.Millisecond)
	if pc := m.PendingCount(); pc != 1 {
		t.Errorf("pending = %d, want 1", pc)
	}
	close(block)
	m.WaitAll()
	if pc := m.PendingCount(); pc != 0 {
		t.Errorf("pending after = %d, want 0", pc)
	}
	if m.TotalCount() < 1 {
		t.Errorf("total count = %d, want >= 1", m.TotalCount())
	}
}

func TestBaseURL(t *testing.T) {
	srv := newAsyncServer(t, 200, "base")
	defer srv.Close()

	m := New()
	m.Init(&AsyncHTTPConfig{BaseURL: srv.URL, Timeout: DefaultTimeout, MaxConnections: DefaultMaxConnections, Workers: DefaultWorkers})
	var got AsyncHTTPResult
	var wg sync.WaitGroup
	wg.Add(1)
	m.GetAsync("/path", func(r *AsyncHTTPResult) {
		got = *r
		wg.Done()
	})
	wg.Wait()
	if got.Err != nil {
		t.Fatalf("GetAsync error: %v", got.Err)
	}
	if got.Status != 200 {
		t.Errorf("status = %d, want 200", got.Status)
	}
}

func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := New()
	m.Init(&AsyncHTTPConfig{Timeout: 20 * time.Millisecond, MaxConnections: DefaultMaxConnections, Workers: DefaultWorkers})
	var got AsyncHTTPResult
	var wg sync.WaitGroup
	wg.Add(1)
	m.GetAsync(srv.URL, func(r *AsyncHTTPResult) {
		got = *r
		wg.Done()
	})
	wg.Wait()
	if got.Err == nil {
		t.Errorf("expected timeout error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultAsyncHTTPClient() == nil {
		t.Fatalf("DefaultAsyncHTTPClient() nil")
	}
	Init()
	if DefaultAsyncHTTPClient() == nil {
		t.Fatalf("DefaultAsyncHTTPClient() nil after Init")
	}
}
