// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * http_client module - synchronous HTTP client.
 * Port of the kamailio http_client module (src/modules/http_client).
 *
 * The original C module wraps libcurl to let the Kamailio script issue
 * HTTP requests (http_connect). This Go counterpart wraps net/http with
 * a connection-pooled client, configurable timeout, default headers and
 * TLS verification toggle.
 *
 * It is safe for concurrent use.
 */

package http_client

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DefaultTimeout is the default request timeout.
// C: default_connection_timeout (4 seconds)
const DefaultTimeout = 4 * time.Second

// HTTPClientConfig configures an HTTPClientModule.
type HTTPClientConfig struct {
	BaseURL   string
	Timeout   time.Duration
	Headers   map[string]string
	TLSVerify bool
}

// HTTPClientModule issues synchronous HTTP requests.
// It is the Go counterpart of the kamailio http_client module.
type HTTPClientModule struct {
	mu      sync.Mutex
	client  *http.Client
	baseURL string
	headers map[string]string
}

// New creates an HTTPClientModule with default settings.
func New() *HTTPClientModule {
	m := &HTTPClientModule{headers: make(map[string]string)}
	m.Init(&HTTPClientConfig{Timeout: DefaultTimeout, TLSVerify: true})
	return m
}

// Init (re)configures the module from cfg. A nil cfg applies defaults.
//
//	C: mod_init() / curl init
func (m *HTTPClientModule) Init(cfg *HTTPClientConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &HTTPClientConfig{Timeout: DefaultTimeout, TLSVerify: true}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	m.baseURL = cfg.BaseURL
	headers := make(map[string]string)
	for k, v := range cfg.Headers {
		headers[k] = v
	}
	m.headers = headers
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.TLSVerify},
	}
	m.client = &http.Client{Timeout: timeout, Transport: tr}
}

// resolveURL joins the base URL with url when url is not absolute.
func (m *HTTPClientModule) resolveURL(url string) string {
	if m.baseURL == "" {
		return url
	}
	if len(url) >= 7 && (url[:7] == "http://" || url[:8] == "https://") {
		return url
	}
	return m.baseURL + url
}

// snapshotHeaders returns a copy of the configured default headers.
func (m *HTTPClientModule) snapshotHeaders() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.headers))
	for k, v := range m.headers {
		out[k] = v
	}
	return out
}

// do performs a request and returns the status code, body and error.
func (m *HTTPClientModule) do(method, url, body, contentType string) (int, []byte, error) {
	if m.client == nil {
		m.Init(nil)
	}
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, m.resolveURL(url), bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("http_client: %w", err)
	}
	for k, v := range m.snapshotHeaders() {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("http_client: %w", err)
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return resp.StatusCode, nil, fmt.Errorf("http_client: read body: %w", rerr)
	}
	return resp.StatusCode, data, nil
}

// Get performs an HTTP GET request.
//
//	C: http_connect() GET
func (m *HTTPClientModule) Get(url string) (int, []byte, error) {
	return m.do(http.MethodGet, url, "", "")
}

// Post performs an HTTP POST request with a body and content type.
func (m *HTTPClientModule) Post(url string, body string, contentType string) (int, []byte, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return m.do(http.MethodPost, url, body, contentType)
}

// Put performs an HTTP PUT request with a body and content type.
func (m *HTTPClientModule) Put(url string, body string, contentType string) (int, []byte, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return m.do(http.MethodPut, url, body, contentType)
}

// Delete performs an HTTP DELETE request.
func (m *HTTPClientModule) Delete(url string) (int, []byte, error) {
	return m.do(http.MethodDelete, url, "", "")
}

// SetHeader sets a default header applied to all subsequent requests.
func (m *HTTPClientModule) SetHeader(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.headers[key] = value
}

// SetTimeout updates the request timeout for subsequent requests.
func (m *HTTPClientModule) SetTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	m.client.Timeout = timeout
}

// Close releases idle connections held by the underlying transport.
func (m *HTTPClientModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		if tr, ok := m.client.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
}

// --- package-level API ---

var defaultModule = New()

// DefaultHTTPClient returns the package-level default HTTPClientModule.
func DefaultHTTPClient() *HTTPClientModule {
	return defaultModule
}

// Init (re)initialises the package-level default module with defaults.
func Init() {
	defaultModule = New()
}
