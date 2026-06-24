// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the xcap_client module - XCAP document CRUD over HTTP.
 */
package xcap_client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// newTestServer returns an httptest server that returns canned responses
// keyed by method.
func newTestServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/xcap-el+xml")
			_, _ = w.Write(body)
		case http.MethodPut:
			rb, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/xcap-el+xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(rb)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInitAndConnected(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("fresh module should not be connected")
	}
	if err := m.Init(&XCAPConfig{ServerURL: "http://example.org"}); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if !m.IsConnected() {
		t.Error("expected connected after Init with ServerURL")
	}
	// Trailing slash is trimmed.
	if got := m.serverURL; got != "http://example.org" {
		t.Errorf("serverURL = %q", got)
	}
}

func TestGetDocument(t *testing.T) {
	doc := []byte("<resource-lists/>")
	srv := newTestServer(t, doc)
	m := New()
	if err := m.Init(&XCAPConfig{ServerURL: srv.URL}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	got, err := m.GetDocument("resource-lists", "index", "alice")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if string(got) != string(doc) {
		t.Errorf("GetDocument body = %q, want %q", got, doc)
	}
	if !strings.Contains(m.docURL("resource-lists", "index", "alice"), "alice") {
		t.Errorf("docURL missing user: %s", m.docURL("resource-lists", "index", "alice"))
	}
}

func TestGetElement(t *testing.T) {
	elem := []byte("<list/>")
	srv := newTestServer(t, elem)
	m := New()
	_ = m.Init(&XCAPConfig{ServerURL: srv.URL})
	got, err := m.GetElement("resource-lists", "index", "alice", "/list")
	if err != nil {
		t.Fatalf("GetElement: %v", err)
	}
	if string(got) != string(elem) {
		t.Errorf("GetElement = %q", got)
	}
}

func TestPutAndDeleteDocument(t *testing.T) {
	srv := newTestServer(t, nil)
	m := New()
	_ = m.Init(&XCAPConfig{ServerURL: srv.URL, Username: "u", Password: "p"})
	body := []byte("<resource-lists><list/></resource-lists>")
	if err := m.PutDocument("resource-lists", "index", "alice", body); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	if err := m.DeleteDocument("resource-lists", "index", "alice"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
}

func TestNotConnectedErrors(t *testing.T) {
	m := New()
	if _, err := m.GetDocument("a", "b", "c"); err == nil {
		t.Error("GetDocument on disconnected module should error")
	}
	if err := m.PutDocument("a", "b", "c", []byte("x")); err == nil {
		t.Error("PutDocument on disconnected module should error")
	}
	if err := m.DeleteDocument("a", "b", "c"); err == nil {
		t.Error("DeleteDocument on disconnected module should error")
	}
}

func TestDefaultAndInit(t *testing.T) {
	if err := Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	d1 := DefaultXCAPClient()
	d2 := DefaultXCAPClient()
	if d1 != d2 {
		t.Error("DefaultXCAPClient should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	srv := newTestServer(t, []byte("<x/>"))
	m := New()
	_ = m.Init(&XCAPConfig{ServerURL: srv.URL})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.GetDocument("a", "b", "c")
			_ = m.PutDocument("a", "b", "c", []byte("<x/>"))
		}()
	}
	wg.Wait()
}
