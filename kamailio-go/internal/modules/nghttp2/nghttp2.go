// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * nghttp2 module - HTTP/2 server.
 * Port of the kamailio nghttp2 module (src/modules/nghttp2).
 *
 * The nghttp2 module serves Kamailio control plane over HTTP/2 (h2) with
 * TLS. This Go counterpart runs an embedded TLS net/http server (which
 * negotiates HTTP/2 automatically), dispatches requests to script-
 * registered handlers keyed by URL path, and writes back the status, body
 * and headers produced by the handler.
 *
 * It is safe for concurrent use.
 */

package nghttp2

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// NGHTTP2Config holds the configuration for an NGHTTP2Module, mirroring the
// modparams of the C nghttp2 module (listen, cert, key, max_streams).
type NGHTTP2Config struct {
	ListenAddr           string
	CertFile             string
	KeyFile              string
	MaxConcurrentStreams int
}

// NGHTTP2Handler is a script-registered request handler. It returns the
// HTTP status code, response body and response headers.
type NGHTTP2Handler func(method, path, body string, headers map[string]string) (int, string, map[string]string)

// NGHTTP2Module runs an embedded HTTP/2 server. It is the Go counterpart
// of the C nghttp2 module.
type NGHTTP2Module struct {
	mu       sync.RWMutex
	cfg      *NGHTTP2Config
	handlers map[string]NGHTTP2Handler

	streamMu    sync.Mutex
	streamCount int

	listener net.Listener
	server   *http.Server
	running  bool
}

// New creates an NGHTTP2Module with empty handler storage.
func New() *NGHTTP2Module {
	return &NGHTTP2Module{handlers: make(map[string]NGHTTP2Handler)}
}

// Init configures the module from cfg. A nil cfg applies empty defaults.
//
//	C: mod_init()
func (m *NGHTTP2Module) Init(cfg *NGHTTP2Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &NGHTTP2Config{}
	}
	m.cfg = cfg
	if m.handlers == nil {
		m.handlers = make(map[string]NGHTTP2Handler)
	}
	return nil
}

// RegisterHandler registers a handler for the given path. A handler
// registered for an existing path replaces the previous one.
func (m *NGHTTP2Module) RegisterHandler(path string, handler func(method, path, body string, headers map[string]string) (int, string, map[string]string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlers == nil {
		m.handlers = make(map[string]NGHTTP2Handler)
	}
	m.handlers[path] = handler
}

// RemoveHandler removes the handler registered for the given path.
func (m *NGHTTP2Module) RemoveHandler(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, path)
}

// lookupHandler returns the handler for path, trying an exact match first
// and then the longest registered prefix.
func (m *NGHTTP2Module) lookupHandler(path string) NGHTTP2Handler {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if h, ok := m.handlers[path]; ok {
		return h
	}
	var best string
	for p := range m.handlers {
		if strings.HasPrefix(path, p) && len(p) > len(best) {
			best = p
		}
	}
	if best != "" {
		return m.handlers[best]
	}
	return nil
}

// handle dispatches a request to the registered handler for path. Unknown
// paths yield a 404 status with a descriptive body.
func (m *NGHTTP2Module) handle(method, path, body string, headers map[string]string) (int, string, map[string]string) {
	h := m.lookupHandler(path)
	if h == nil {
		return http.StatusNotFound, "404 page not found: " + path, nil
	}
	code, respBody, respHeaders := h(method, path, body, headers)
	if code == 0 {
		code = http.StatusOK
	}
	return code, respBody, respHeaders
}

// ServeHTTP implements http.Handler, bridging net/http to the script
// handlers while tracking the number of in-flight streams.
func (m *NGHTTP2Module) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.streamMu.Lock()
	m.streamCount++
	m.streamMu.Unlock()
	defer func() {
		m.streamMu.Lock()
		m.streamCount--
		m.streamMu.Unlock()
	}()

	body, _ := io.ReadAll(r.Body)
	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	code, respBody, respHeaders := m.handle(r.Method, r.URL.Path, string(body), headers)
	for k, v := range respHeaders {
		w.Header().Set(k, v)
	}
	w.WriteHeader(code)
	w.Write([]byte(respBody))
}

// StreamCount returns the number of currently in-flight HTTP/2 streams.
func (m *NGHTTP2Module) StreamCount() int {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	return m.streamCount
}

// Start launches the TLS HTTP/2 server on the configured ListenAddr using
// the configured CertFile and KeyFile. Calling Start while already
// running stops the previous server first.
//
//	C: child_init() / listen analogue
func (m *NGHTTP2Module) Start() error {
	m.Stop()
	m.mu.Lock()
	if m.cfg == nil {
		m.cfg = &NGHTTP2Config{}
	}
	addr := m.cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:443"
	}
	certFile := m.cfg.CertFile
	keyFile := m.cfg.KeyFile
	m.mu.Unlock()

	// Validate the certificate pair up front so a missing or malformed
	// cert is reported as a Start error rather than failing asynchronously.
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: m}

	m.mu.Lock()
	m.listener = ln
	m.server = srv
	m.running = true
	m.mu.Unlock()

	go func() {
		if err := srv.ServeTLS(ln, certFile, keyFile); err != nil && err != http.ErrServerClosed {
			// Server failed to start or stopped unexpectedly; mark
			// not-running so IsRunning reflects reality.
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
		}
	}()
	return nil
}

// Stop shuts down the running HTTP/2 server. It is idempotent.
func (m *NGHTTP2Module) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	ln := m.listener
	srv := m.server
	m.listener = nil
	m.server = nil
	m.mu.Unlock()
	if srv != nil {
		srv.Close()
	}
	if ln != nil {
		ln.Close()
	}
}

// IsRunning reports whether the HTTP/2 server is currently running.
func (m *NGHTTP2Module) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *NGHTTP2Module
)

// DefaultNGHTTP2 returns the process-wide NGHTTP2Module, creating it on
// first use.
func DefaultNGHTTP2() *NGHTTP2Module {
	defaultMu.RLock()
	mod := defaultM
	defaultMu.RUnlock()
	if mod != nil {
		return mod
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide NGHTTP2Module to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM != nil {
		defaultM.Stop()
	}
	defaultM = New()
}
