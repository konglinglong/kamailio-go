// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * microhttpd module - minimal embedded HTTP daemon.
 * Port of the kamailio microhttpd module (src/modules/microhttpd).
 *
 * The microhttpd module embeds a tiny HTTP daemon that dispatches
 * requests to script-registered handlers keyed by URL path. Each handler
 * receives the method, path and body and returns a status code and body.
 * The daemon enforces a maximum number of concurrent connections and an
 * optional request timeout.
 *
 * It is safe for concurrent use.
 */

package microhttpd

import (
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MicroHTTPConfig holds the configuration for a MicroHTTPModule, mirroring
// the modparams of the C microhttpd module (listen, max_connections,
// timeout).
type MicroHTTPConfig struct {
	ListenAddr     string
	MaxConnections int
	Timeout        time.Duration
}

// MicroHTTPHandler is a script-registered request handler. It returns the
// HTTP status code and response body.
type MicroHTTPHandler func(method, path, body string) (int, string)

// MicroHTTPModule runs a minimal embedded HTTP daemon. It is the Go
// counterpart of the C microhttpd module.
type MicroHTTPModule struct {
	mu       sync.RWMutex
	cfg      *MicroHTTPConfig
	handlers map[string]MicroHTTPHandler

	connMu    sync.Mutex
	connCount int

	listener net.Listener
	server   *http.Server
	running  bool
}

// New creates a MicroHTTPModule with empty handler storage.
func New() *MicroHTTPModule {
	return &MicroHTTPModule{handlers: make(map[string]MicroHTTPHandler)}
}

// Init configures the module from cfg. A nil cfg applies empty defaults.
//
//	C: mod_init()
func (m *MicroHTTPModule) Init(cfg *MicroHTTPConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &MicroHTTPConfig{}
	}
	m.cfg = cfg
	if m.handlers == nil {
		m.handlers = make(map[string]MicroHTTPHandler)
	}
	return nil
}

// RegisterHandler registers a handler for the given path. A handler
// registered for an existing path replaces the previous one.
func (m *MicroHTTPModule) RegisterHandler(path string, handler func(method, path, body string) (int, string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlers == nil {
		m.handlers = make(map[string]MicroHTTPHandler)
	}
	m.handlers[path] = handler
}

// RemoveHandler removes the handler registered for the given path.
func (m *MicroHTTPModule) RemoveHandler(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, path)
}

// lookupHandler returns the handler for path, trying an exact match first
// and then the longest registered prefix.
func (m *MicroHTTPModule) lookupHandler(path string) MicroHTTPHandler {
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
func (m *MicroHTTPModule) handle(method, path, body string) (int, string) {
	h := m.lookupHandler(path)
	if h == nil {
		return http.StatusNotFound, "404 page not found: " + path
	}
	code, respBody := h(method, path, body)
	if code == 0 {
		code = http.StatusOK
	}
	return code, respBody
}

// ServeHTTP implements http.Handler, bridging net/http to the script
// handlers while tracking the in-flight connection count and enforcing
// the configured MaxConnections limit.
func (m *MicroHTTPModule) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	max := m.maxConnections()
	m.connMu.Lock()
	if max > 0 && m.connCount >= max {
		m.connMu.Unlock()
		http.Error(w, "max connections exceeded", http.StatusServiceUnavailable)
		return
	}
	m.connCount++
	m.connMu.Unlock()
	defer func() {
		m.connMu.Lock()
		m.connCount--
		m.connMu.Unlock()
	}()

	body, _ := io.ReadAll(r.Body)
	code, respBody := m.handle(r.Method, r.URL.Path, string(body))
	w.WriteHeader(code)
	w.Write([]byte(respBody))
}

// maxConnections returns the configured connection cap (0 = unlimited).
func (m *MicroHTTPModule) maxConnections() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil {
		return 0
	}
	return m.cfg.MaxConnections
}

// ConnectionCount returns the number of currently in-flight requests.
func (m *MicroHTTPModule) ConnectionCount() int {
	m.connMu.Lock()
	defer m.connMu.Unlock()
	return m.connCount
}

// Start launches the HTTP server on the configured ListenAddr. Calling
// Start while already running stops the previous server first.
//
//	C: child_init() / listen analogue
func (m *MicroHTTPModule) Start() error {
	m.Stop()
	m.mu.Lock()
	if m.cfg == nil {
		m.cfg = &MicroHTTPConfig{}
	}
	addr := m.cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:7070"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	srv := &http.Server{Handler: m}
	if m.cfg.Timeout > 0 {
		srv.ReadTimeout = m.cfg.Timeout
		srv.WriteTimeout = m.cfg.Timeout
	}
	m.listener = ln
	m.server = srv
	m.running = true
	ln = m.listener
	m.mu.Unlock()
	go srv.Serve(ln)
	return nil
}

// Stop shuts down the running HTTP server. It is idempotent.
func (m *MicroHTTPModule) Stop() {
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

// IsRunning reports whether the HTTP server is currently running.
func (m *MicroHTTPModule) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *MicroHTTPModule
)

// DefaultMicroHTTP returns the process-wide MicroHTTPModule, creating it on
// first use.
func DefaultMicroHTTP() *MicroHTTPModule {
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

// Init (re)initialises the process-wide MicroHTTPModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM != nil {
		defaultM.Stop()
	}
	defaultM = New()
}
