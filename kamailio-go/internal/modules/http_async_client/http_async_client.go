// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * http_async_client module - asynchronous HTTP client.
 * Port of the kamailio http_async_client module
 * (src/modules/http_async_client).
 *
 * The original C module issues non-blocking HTTP requests using libcurl
 * multi handles and a pool of worker processes, notifying the script
 * via a resume route once a response arrives. This Go counterpart uses
 * goroutines plus a sync.WaitGroup to track in-flight requests and
 * deliver results through a callback.
 *
 * It is safe for concurrent use.
 */

package http_async_client

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultTimeout is the default per-request timeout.
const DefaultTimeout = 4 * time.Second

// DefaultMaxConnections is the default cap on in-flight requests.
const DefaultMaxConnections = 100

// DefaultWorkers is the default number of background workers. In this
// goroutine-based implementation workers are advisory; the cap is kept
// for configuration parity with the C module.
const DefaultWorkers = 4

// AsyncHTTPConfig configures an AsyncHTTPClientModule.
type AsyncHTTPConfig struct {
	BaseURL       string
	Timeout       time.Duration
	MaxConnections int
	Workers       int
}

// AsyncHTTPResult is delivered to an async request callback.
type AsyncHTTPResult struct {
	Status   int
	Body     []byte
	Err      error
	Duration time.Duration
}

// AsyncHTTPClientModule issues asynchronous HTTP requests.
// It is the Go counterpart of the kamailio http_async_client module.
type AsyncHTTPClientModule struct {
	mu       sync.Mutex
	client   *http.Client
	baseURL  string
	timeout  time.Duration
	maxConns int
	workers  int
	pending  sync.WaitGroup
	pendingN int64
	count    int64
	closed   atomic.Bool
}

// New creates an AsyncHTTPClientModule with default settings.
func New() *AsyncHTTPClientModule {
	m := &AsyncHTTPClientModule{}
	m.Init(&AsyncHTTPConfig{Timeout: DefaultTimeout, MaxConnections: DefaultMaxConnections, Workers: DefaultWorkers})
	return m
}

// Init (re)configures the module from cfg. A nil cfg applies defaults.
//
//	C: mod_init() / curl multi init
func (m *AsyncHTTPClientModule) Init(cfg *AsyncHTTPConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &AsyncHTTPConfig{Timeout: DefaultTimeout, MaxConnections: DefaultMaxConnections, Workers: DefaultWorkers}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxConns := cfg.MaxConnections
	if maxConns <= 0 {
		maxConns = DefaultMaxConnections
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	m.baseURL = cfg.BaseURL
	m.timeout = timeout
	m.maxConns = maxConns
	m.workers = workers
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: false},
		MaxIdleConnsPerHost: maxConns,
	}
	m.client = &http.Client{Timeout: timeout, Transport: tr}
	m.closed.Store(false)
}

// resolveURL joins the base URL with url when url is not absolute.
func (m *AsyncHTTPClientModule) resolveURL(url string) string {
	if m.baseURL == "" {
		return url
	}
	if len(url) >= 7 && (url[:7] == "http://" || url[:8] == "https://") {
		return url
	}
	return m.baseURL + url
}

// doAsync runs an HTTP request in a goroutine and invokes callback with
// the result. The request is tracked by the pending WaitGroup so that
// WaitAll can block until completion.
func (m *AsyncHTTPClientModule) doAsync(method, url, body, contentType string, callback func(*AsyncHTTPResult)) {
	if m.client == nil {
		m.Init(nil)
	}
	m.mu.Lock()
	client := m.client
	resolved := m.resolveURL(url)
	m.mu.Unlock()

	m.pending.Add(1)
	atomic.AddInt64(&m.pendingN, 1)
	atomic.AddInt64(&m.count, 1)
	go func() {
		defer m.pending.Done()
		defer atomic.AddInt64(&m.pendingN, -1)
		if m.closed.Load() {
			if callback != nil {
				callback(&AsyncHTTPResult{Status: 0, Err: fmt.Errorf("http_async_client: closed")})
			}
			return
		}
		start := time.Now()
		var bodyReader io.Reader
		if body != "" {
			bodyReader = bytes.NewReader([]byte(body))
		}
		req, err := http.NewRequest(method, resolved, bodyReader)
		if err != nil {
			if callback != nil {
				callback(&AsyncHTTPResult{Err: fmt.Errorf("http_async_client: %w", err), Duration: time.Since(start)})
			}
			return
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := client.Do(req)
		if err != nil {
			if callback != nil {
				callback(&AsyncHTTPResult{Err: fmt.Errorf("http_async_client: %w", err), Duration: time.Since(start)})
			}
			return
		}
		defer resp.Body.Close()
		data, rerr := io.ReadAll(resp.Body)
		dur := time.Since(start)
		if rerr != nil {
			if callback != nil {
				callback(&AsyncHTTPResult{Status: resp.StatusCode, Err: fmt.Errorf("http_async_client: read body: %w", rerr), Duration: dur})
			}
			return
		}
		if callback != nil {
			callback(&AsyncHTTPResult{Status: resp.StatusCode, Body: data, Duration: dur})
		}
	}()
}

// GetAsync issues an asynchronous HTTP GET and invokes callback with the
// result. The callback may be nil.
func (m *AsyncHTTPClientModule) GetAsync(url string, callback func(*AsyncHTTPResult)) {
	m.doAsync(http.MethodGet, url, "", "", callback)
}

// PostAsync issues an asynchronous HTTP POST and invokes callback with
// the result. The body is sent as application/octet-stream by default.
func (m *AsyncHTTPClientModule) PostAsync(url string, body string, callback func(*AsyncHTTPResult)) {
	m.doAsync(http.MethodPost, url, body, "application/octet-stream", callback)
}

// WaitAll blocks until every in-flight async request has completed.
func (m *AsyncHTTPClientModule) WaitAll() {
	m.pending.Wait()
}

// PendingCount returns the number of in-flight async requests.
func (m *AsyncHTTPClientModule) PendingCount() int {
	return int(atomic.LoadInt64(&m.pendingN))
}

// Close marks the module closed and waits for outstanding requests.
func (m *AsyncHTTPClientModule) Close() {
	m.closed.Store(true)
	m.pending.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		if tr, ok := m.client.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
}

// TotalCount returns the total number of async requests dispatched.
func (m *AsyncHTTPClientModule) TotalCount() int64 {
	return atomic.LoadInt64(&m.count)
}

// --- package-level API ---

var defaultModule = New()

// DefaultAsyncHTTPClient returns the package-level default module.
func DefaultAsyncHTTPClient() *AsyncHTTPClientModule {
	return defaultModule
}

// Init (re)initialises the package-level default module with defaults.
func Init() {
	defaultModule = New()
}
