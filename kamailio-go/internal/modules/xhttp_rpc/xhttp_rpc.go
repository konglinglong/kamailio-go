// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xhttp_rpc module - JSON-RPC 2.0 over HTTP.
 * Port of the kamailio xhttp_rpc module (src/modules/xhttp_rpc).
 *
 * The xhttp_rpc module exposes Kamailio's RPC interface over HTTP using
 * the JSON-RPC 2.0 protocol. This Go counterpart registers named methods,
 * dispatches JSON-RPC request bodies to them, and serialises the result
 * (or error) back as a JSON-RPC 2.0 response.
 *
 * It is safe for concurrent use.
 */

package xhttp_rpc

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
)

// JSON-RPC 2.0 error codes (per spec).
const (
	errCodeParseError      = -32700
	errCodeInvalidRequest  = -32600
	errCodeMethodNotFound  = -32601
	errCodeInvalidParams   = -32602
	errCodeInternalError   = -32603
	errCodeServerError     = -32000
)

// XHTTPRPCConfig holds the configuration for an XHTTPRPCModule, mirroring
// the modparams of the C xhttp_rpc module (http_listen, http_rpc_path).
type XHTTPRPCConfig struct {
	ListenAddr string
	Path       string
}

// RPCHandler is a script-registered JSON-RPC method handler.
type RPCHandler func(params interface{}) (interface{}, error)

// XHTTPRPCModule dispatches JSON-RPC 2.0 requests to registered methods.
type XHTTPRPCModule struct {
	mu       sync.RWMutex
	cfg      *XHTTPRPCConfig
	methods  map[string]RPCHandler
	listener net.Listener
	server   *http.Server
	running  bool
}

// New creates an XHTTPRPCModule with empty method storage.
func New() *XHTTPRPCModule {
	return &XHTTPRPCModule{methods: make(map[string]RPCHandler)}
}

// Init configures the module from cfg. A nil cfg applies empty defaults.
//
//	C: mod_init()
func (m *XHTTPRPCModule) Init(cfg *XHTTPRPCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &XHTTPRPCConfig{}
	}
	m.cfg = cfg
	if m.methods == nil {
		m.methods = make(map[string]RPCHandler)
	}
	return nil
}

// RegisterMethod registers a JSON-RPC method handler. A handler
// registered for an existing name replaces the previous one.
func (m *XHTTPRPCModule) RegisterMethod(name string, handler func(params interface{}) (interface{}, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.methods == nil {
		m.methods = make(map[string]RPCHandler)
	}
	m.methods[name] = handler
}

// UnregisterMethod removes the method registered for name.
func (m *XHTTPRPCModule) UnregisterMethod(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.methods, name)
}

// ListMethods returns the sorted names of all registered methods.
func (m *XHTTPRPCModule) ListMethods() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.methods))
	for name := range m.methods {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// rpcRequest is the JSON-RPC 2.0 request envelope.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcResponse is the JSON-RPC 2.0 response envelope.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// isNotification reports whether the request carries no id (a
// notification per JSON-RPC 2.0).
func (r *rpcRequest) isNotification() bool {
	return len(r.ID) == 0
}

// HandleRequest parses a JSON-RPC 2.0 request body, dispatches it to the
// registered method, and returns the serialised JSON-RPC 2.0 response.
// Notifications (requests without an id) are executed but produce an
// empty byte slice. Parse errors yield a -32700 error response.
func (m *XHTTPRPCModule) HandleRequest(body []byte) ([]byte, error) {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return marshalResponse(&rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: errCodeParseError, Message: "Parse error"},
			ID:      json.RawMessage("null"),
		}), nil
	}

	notification := req.isNotification()

	// Look up the method.
	m.mu.RLock()
	handler, ok := m.methods[req.Method]
	m.mu.RUnlock()

	if !ok {
		if notification {
			return nil, nil
		}
		return marshalResponse(&rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: errCodeMethodNotFound, Message: "Method not found"},
			ID:      req.ID,
		}), nil
	}

	// Decode params into a generic value.
	var params interface{}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			if notification {
				return nil, nil
			}
			return marshalResponse(&rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: errCodeInvalidParams, Message: "Invalid params"},
				ID:      req.ID,
			}), nil
		}
	}

	result, err := handler(params)
	if err != nil {
		if notification {
			return nil, nil
		}
		return marshalResponse(&rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: errCodeServerError, Message: err.Error()},
			ID:      req.ID,
		}), nil
	}

	if notification {
		return nil, nil
	}
	return marshalResponse(&rpcResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      req.ID,
	}), nil
}

// marshalResponse serialises a response, falling back to an internal
// error envelope when marshalling itself fails.
func marshalResponse(resp *rpcResponse) []byte {
	out, err := json.Marshal(resp)
	if err != nil {
		fallback, _ := json.Marshal(&rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: errCodeInternalError, Message: "Internal error"},
			ID:      json.RawMessage("null"),
		})
		return fallback
	}
	return out
}

// ServeHTTP implements http.Handler, bridging net/http to HandleRequest.
func (m *XHTTPRPCModule) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	out, _ := m.HandleRequest(body)
	if len(out) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// Start launches the HTTP server on the configured ListenAddr, serving
// JSON-RPC at the configured Path. Calling Start while already running
// stops the previous server first.
func (m *XHTTPRPCModule) Start() error {
	m.Stop()
	m.mu.Lock()
	if m.cfg == nil {
		m.cfg = &XHTTPRPCConfig{}
	}
	addr := m.cfg.ListenAddr
	if addr == "" {
		addr = "0.0.0.0:8090"
	}
	path := m.cfg.Path
	if path == "" {
		path = "/RPC"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	mux := http.NewServeMux()
	mux.Handle(path, m)
	m.listener = ln
	m.server = &http.Server{Handler: mux}
	m.running = true
	ln = m.listener
	srv := m.server
	m.mu.Unlock()
	go srv.Serve(ln)
	return nil
}

// Stop shuts down the running HTTP server. It is idempotent.
func (m *XHTTPRPCModule) Stop() {
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

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *XHTTPRPCModule
)

// DefaultXHTTPRPC returns the process-wide XHTTPRPCModule, creating it on
// first use.
func DefaultXHTTPRPC() *XHTTPRPCModule {
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

// Init (re)initialises the process-wide XHTTPRPCModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM != nil {
		defaultM.Stop()
	}
	defaultM = New()
}
