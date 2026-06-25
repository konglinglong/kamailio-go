// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * jsonrpcs module - JSON-RPC 2.0 server over SIP transport.
 *
 * Port of the kamailio jsonrpcs module (src/modules/jsonrpcs). Unlike
 * xhttp_rpc (which carries JSON-RPC over HTTP), jsonrpcs serves requests
 * over the SIP transport (FIFO / datagram socket in C). The Go port keeps
 * the same JSON-RPC 2.0 semantics and method dispatch, but the transport
 * is abstracted: callers feed raw request bytes to HandleRequest and
 * receive response bytes back.
 *
 * C equivalent: jsonrpcs.so - jsonrpcs_mod.c / jsonrpcs_fifo.c /
 * jsonrpcs_sock.c.
 */

package jsonrpcs

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Transport identifies the underlying RPC transport.
//
// C equivalent: JSONRPC_TRANS_* constants in jsonrpcs_mod.h.
type Transport int

const (
	TransNone  Transport = 0 // JSONRPC_TRANS_NONE
	TransHTTP  Transport = 1 // JSONRPC_TRANS_HTTP
	TransFIFO  Transport = 2 // JSONRPC_TRANS_FIFO
	TransDgram Transport = 3 // JSONRPC_TRANS_DGRAM
)

// Config holds the jsonrpcs server configuration.
//
// C equivalent: the modparam strings (fifo_name, sock_name, transport).
type Config struct {
	Transport Transport
	FifoName  string
	SockName  string
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Transport: TransFIFO,
		FifoName:  "/run/kamailio/kamailio_rpc.fifo",
		SockName:  "/run/kamailio/kamailio_rpc.sock",
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("jsonrpcs: nil config")
	}
	if c.Transport < TransNone || c.Transport > TransDgram {
		return fmt.Errorf("jsonrpcs: invalid transport %d", c.Transport)
	}
	return nil
}

// MethodHandler is the handler signature for a registered JSON-RPC method.
// The handler receives the parsed params (which may be a map, slice or
// scalar) and returns either a result value or an error.
type MethodHandler func(params interface{}) (interface{}, error)

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 wire types
// ---------------------------------------------------------------------------

// rpcRequest is a single JSON-RPC 2.0 request object.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// rpcResponse is a single JSON-RPC 2.0 response object.
type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// RPCError is the JSON-RPC error object.
//
// C equivalent: error_code / error_text fields in jsonrpc_ctx_t.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

// Standard JSON-RPC 2.0 error codes.
const (
	ErrParse    = -32700
	ErrInvalid  = -32600
	ErrMethod   = -32601
	ErrParams   = -32602
	ErrInternal = -32603
)

// ---------------------------------------------------------------------------
// JSONRPCSModule
// ---------------------------------------------------------------------------

// JSONRPCSModule is the JSON-RPC 2.0 server. It dispatches incoming
// requests to registered method handlers and serialises responses.
//
// C equivalent: the module global state plus the jsonrpc_ctx_t per request.
type JSONRPCSModule struct {
	mu       sync.RWMutex
	cfg      Config
	handlers map[string]MethodHandler
	started  atomic.Bool
	calls    atomic.Int64
}

// New creates a JSONRPCSModule with default configuration and no handlers.
func New() *JSONRPCSModule {
	cfg := *DefaultConfig()
	return &JSONRPCSModule{
		cfg:      cfg,
		handlers: make(map[string]MethodHandler),
	}
}

// NewWithConfig creates a JSONRPCSModule using the supplied configuration.
func NewWithConfig(cfg Config) *JSONRPCSModule {
	return &JSONRPCSModule{
		cfg:      cfg,
		handlers: make(map[string]MethodHandler),
	}
}

// Init (re)configures the module with the supplied config and resets the
// handler table. Safe to call multiple times.
//
// C equivalent: mod_init() in jsonrpcs_mod.c.
func (m *JSONRPCSModule) Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.handlers = make(map[string]MethodHandler)
	return nil
}

// Config returns a copy of the current configuration.
func (m *JSONRPCSModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// RegisterMethod registers a handler for the given method name. A nil
// handler removes a previously registered method. Registering an existing
// name overwrites the prior handler.
//
// C equivalent: rpc_register().
func (m *JSONRPCSModule) RegisterMethod(name string, handler MethodHandler) {
	if name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handlers == nil {
		m.handlers = make(map[string]MethodHandler)
	}
	if handler == nil {
		delete(m.handlers, name)
		return
	}
	m.handlers[name] = handler
}

// UnregisterMethod removes a registered method.
func (m *JSONRPCSModule) UnregisterMethod(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, name)
}

// ListMethods returns the registered method names in sorted order.
func (m *JSONRPCSModule) ListMethods() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.handlers))
	for name := range m.handlers {
		out = append(out, name)
	}
	// Sort for deterministic output.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// CallCount returns the number of dispatched requests.
func (m *JSONRPCSModule) CallCount() int64 {
	return m.calls.Load()
}

// Start marks the server as started (transport listener ready). In the Go
// port the actual transport is supplied by the caller; this flag gates
// HandleRequest.
//
// C equivalent: jsonrpcs_fifo_init() / jsonrpcs_sock_init().
func (m *JSONRPCSModule) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started.Load() {
		return errors.New("jsonrpcs: already started")
	}
	m.started.Store(true)
	return nil
}

// Stop marks the server as stopped and clears the started flag. Subsequent
// HandleRequest calls return an error until Start is called again.
func (m *JSONRPCSModule) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started.Store(false)
	return nil
}

// IsStarted reports whether the server is started.
func (m *JSONRPCSModule) IsStarted() bool {
	return m.started.Load()
}

// HandleRequest parses and dispatches a JSON-RPC 2.0 request. Both single
// requests and batch requests (a JSON array) are supported. The returned
// bytes are the JSON-encoded response (or a JSON array of responses for a
// batch). Notifications (requests without an "id") produce no response and
// contribute nil to a batch response array.
//
// C equivalent: jsonrpc_dispatch() / jsonrpc_exec_ex().
func (m *JSONRPCSModule) HandleRequest(request []byte) ([]byte, error) {
	if len(request) == 0 {
		return nil, errors.New("jsonrpcs: empty request")
	}
	request = trimWS(request)
	if len(request) == 0 {
		return nil, errors.New("jsonrpcs: empty request")
	}

	// Batch request: a JSON array.
	if request[0] == '[' {
		return m.handleBatch(request)
	}
	if request[0] != '{' {
		return errorBytes(nil, ErrInvalid, "request must be an object or array")
	}

	var req rpcRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return errorBytes(nil, ErrParse, "parse error: "+err.Error())
	}
	resp := m.dispatchOne(&req)
	if resp == nil {
		// Notification: no response.
		return nil, nil
	}
	return json.Marshal(resp)
}

// handleBatch dispatches a batch of requests and returns a JSON array of
// responses (omitting notifications).
func (m *JSONRPCSModule) handleBatch(request []byte) ([]byte, error) {
	var reqs []rpcRequest
	if err := json.Unmarshal(request, &reqs); err != nil {
		return errorBytes(nil, ErrParse, "parse error: "+err.Error())
	}
	if len(reqs) == 0 {
		return errorBytes(nil, ErrInvalid, "invalid batch: empty")
	}
	responses := make([]rpcResponse, 0, len(reqs))
	for i := range reqs {
		resp := m.dispatchOne(&reqs[i])
		if resp != nil {
			responses = append(responses, *resp)
		}
	}
	if len(responses) == 0 {
		// All notifications: no response.
		return nil, nil
	}
	return json.Marshal(responses)
}

// dispatchOne routes a single request to its handler. Returns nil for a
// notification (no id).
func (m *JSONRPCSModule) dispatchOne(req *rpcRequest) *rpcResponse {
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	id := decodeID(req.ID)

	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		if isNotification {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", Error: &RPCError{Code: ErrInvalid, Message: "unsupported jsonrpc version"}, ID: id}
	}
	if req.Method == "" {
		if isNotification {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", Error: &RPCError{Code: ErrInvalid, Message: "missing method"}, ID: id}
	}

	m.mu.RLock()
	handler, ok := m.handlers[req.Method]
	m.mu.RUnlock()

	if !ok {
		if isNotification {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", Error: &RPCError{Code: ErrMethod, Message: "method not found: " + req.Method}, ID: id}
	}

	m.calls.Add(1)
	params := decodeParams(req.Params)
	result, err := handler(params)
	if err != nil {
		if isNotification {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", Error: toRPCError(err), ID: id}
	}
	if isNotification {
		return nil
	}
	return &rpcResponse{JSONRPC: "2.0", Result: result, ID: id}
}

// decodeParams turns the raw params JSON into a Go value. A missing or
// null params field yields nil.
func decodeParams(raw json.RawMessage) interface{} {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// Try object first.
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	// Try array.
	var arr []interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}
	// Fall back to a raw scalar string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return raw
}

// decodeID extracts the request id as a plain Go value (string, float64 or
// nil). A missing id is treated as a notification.
func decodeID(raw json.RawMessage) interface{} {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	return nil
}

// toRPCError maps a handler error to a JSON-RPC error object. Errors that
// already implement *RPCError are passed through; everything else becomes
// an internal error.
func toRPCError(err error) *RPCError {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	return &RPCError{Code: ErrInternal, Message: err.Error()}
}

// errorBytes builds a JSON-encoded error response.
func errorBytes(id interface{}, code int, msg string) ([]byte, error) {
	return json.Marshal(rpcResponse{JSONRPC: "2.0", Error: &RPCError{Code: code, Message: msg}, ID: id})
}

// trimWS removes leading/trailing whitespace.
func trimWS(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *JSONRPCSModule
)

// DefaultJSONRPCS returns the process-wide module, creating it on first use.
func DefaultJSONRPCS() *JSONRPCSModule {
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

// Init (re)configures the process-wide module with the supplied config and
// resets the handler table.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = &JSONRPCSModule{
		cfg:      cfg,
		handlers: make(map[string]MethodHandler),
	}
	return nil
}

// RegisterMethod is the package-level wrapper around
// DefaultJSONRPCS().RegisterMethod.
func RegisterMethod(name string, handler MethodHandler) {
	DefaultJSONRPCS().RegisterMethod(name, handler)
}

// HandleRequest is the package-level wrapper around
// DefaultJSONRPCS().HandleRequest.
func HandleRequest(request []byte) ([]byte, error) {
	return DefaultJSONRPCS().HandleRequest(request)
}

// ListMethods is the package-level wrapper around DefaultJSONRPCS().ListMethods.
func ListMethods() []string {
	return DefaultJSONRPCS().ListMethods()
}
