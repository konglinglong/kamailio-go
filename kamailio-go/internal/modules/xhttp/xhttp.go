// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xhttp module - embedded HTTP server.
 * Port of the kamailio xhttp module (src/modules/xhttp).
 *
 * The xhttp module exposes Kamailio control plane over HTTP. This Go
 * counterpart runs an embedded net/http server, dispatches requests to
 * script-registered handlers keyed by URL path, and writes back the
 * response produced by the handler.
 *
 * It is safe for concurrent use: the handler map is guarded by a
 * read/write lock and the process-wide singleton is guarded by a mutex.
 */

package xhttp

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

// XHTTPConfig holds the configuration for an XHTTPModule, mirroring the
// modparams of the C xhttp module (http_listen, http_match, http_routes).
type XHTTPConfig struct {
	ListenAddr string
	Match     string
	Routes    map[string]string
}

// XHTTPRequest is the script-facing representation of an inbound HTTP
// request.
type XHTTPRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    string
	Query   map[string]string
}

// XHTTPResponse is the script-facing representation of an HTTP response.
type XHTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    string
}

// XHTTPHandler is a script-registered request handler.
type XHTTPHandler func(*XHTTPRequest) *XHTTPResponse

// XHTTPModule runs an embedded HTTP server and dispatches requests to
// registered handlers. It is the Go counterpart of the C xhttp module.
type XHTTPModule struct {
	mu       sync.RWMutex
	cfg      *XHTTPConfig
	handlers map[string]XHTTPHandler
	listener net.Listener
	server   *http.Server
	running  bool
}

// New creates an XHTTPModule with empty handler storage.
func New() *XHTTPModule {
	return &XHTTPModule{handlers: make(map[string]XHTTPHandler)}
}

// Init configures the module from cfg. A nil cfg applies empty defaults.
//
//	C: mod_init()
func (m *XHTTPModule) Init(cfg *XHTTPConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &XHTTPConfig{}
	}
	m.cfg = cfg
	if m.handlers == nil {
		m.handlers = make(map[string]XHTTPHandler)
	}
	return nil
}

// RegisterHandler registers a handler for the given path. A handler
// registered for an existing path replaces the previous one.
//
//	C: xhttp registration analogue
func (m *XHTTPModule) RegisterHandler(path string, handler func(*XHTTPRequest) *XHTTPResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlers == nil {
		m.handlers = make(map[string]XHTTPHandler)
	}
	m.handlers[path] = handler
}

// RemoveHandler removes the handler registered for the given path.
func (m *XHTTPModule) RemoveHandler(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, path)
}

// lookupHandler returns the handler for path, trying an exact match first
// and then the longest registered prefix. It returns nil when no handler
// matches.
func (m *XHTTPModule) lookupHandler(path string) XHTTPHandler {
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

// matchAllowed reports whether path is permitted by the configured Match
// prefix. When Match is empty every path is allowed.
func (m *XHTTPModule) matchAllowed(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cfg == nil || m.cfg.Match == "" {
		return true
	}
	return strings.HasPrefix(path, m.cfg.Match)
}

// HandleRequest dispatches req to the registered handler for its path.
// Unknown paths (or paths outside the configured Match prefix) yield a
// 404 response. A nil request returns an error.
func (m *XHTTPModule) HandleRequest(req *XHTTPRequest) (*XHTTPResponse, error) {
	if req == nil {
		return nil, errors.New("xhttp: nil request")
	}
	if !m.matchAllowed(req.Path) {
		return notFoundResponse(req.Path), nil
	}
	h := m.lookupHandler(req.Path)
	if h == nil {
		return notFoundResponse(req.Path), nil
	}
	resp := h(req)
	if resp == nil {
		return &XHTTPResponse{Status: http.StatusOK}, nil
	}
	if resp.Status == 0 {
		resp.Status = http.StatusOK
	}
	return resp, nil
}

// notFoundResponse builds a 404 response for path.
func notFoundResponse(path string) *XHTTPResponse {
	return &XHTTPResponse{
		Status:  http.StatusNotFound,
		Headers: map[string]string{"Content-Type": "text/plain"},
		Body:    "404 page not found: " + path,
	}
}

// ServeHTTP implements http.Handler, bridging net/http to the script
// handlers. It is used both by Start and by external httptest servers.
func (m *XHTTPModule) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	headers := make(map[string]string, len(r.Header))
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	query := make(map[string]string, len(r.URL.Query()))
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	req := &XHTTPRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
		Body:    string(body),
		Query:   query,
	}
	resp, _ := m.HandleRequest(req)
	if resp == nil {
		resp = &XHTTPResponse{Status: http.StatusInternalServerError}
	}
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(resp.Status)
	w.Write([]byte(resp.Body))
}

// Start launches the HTTP server on the configured ListenAddr. It is an
// error to call Start without Init. Calling Start while already running
// stops the previous server first.
//
//	C: child_init() / http_listen analogue
func (m *XHTTPModule) Start() error {
	m.Stop()
	m.mu.Lock()
	if m.cfg == nil {
		m.cfg = &XHTTPConfig{}
	}
	addr := m.cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:8080"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.listener = ln
	m.server = &http.Server{Handler: m}
	m.running = true
	ln = m.listener
	srv := m.server
	m.mu.Unlock()
	go srv.Serve(ln)
	return nil
}

// Stop shuts down the running HTTP server. It is idempotent.
func (m *XHTTPModule) Stop() {
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
func (m *XHTTPModule) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *XHTTPModule
)

// DefaultXHTTP returns the process-wide XHTTPModule, creating it on first
// use.
func DefaultXHTTP() *XHTTPModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide XHTTPModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM != nil {
		defaultM.Stop()
	}
	defaultM = New()
}
