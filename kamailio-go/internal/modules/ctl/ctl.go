// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * ctl module - control interface over FIFO / Unix socket.
 *
 * Port of the kamailio ctl module (src/modules/ctl). Exposes a JSON-RPC 2.0
 * command endpoint reachable through a Unix domain socket (binrpc-style) or a
 * named pipe (FIFO). Commands are registered dynamically via
 * RegisterCommand and dispatched by HandleConnection.
 *
 * C equivalent: ctl.so - io_listener.c / fifo_server.c / ctrl_socks.c.
 */

package ctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// CommandHandler is the function signature for a registered control command.
// It receives string-typed parameters and returns an arbitrary JSON-encodable
// result.
type CommandHandler func(params map[string]string) (interface{}, error)

// CTLModule is the control interface module. It owns zero or more listeners
// (Unix socket and/or FIFO) and a registry of named commands.
//
// C equivalent: the ctl module state plus the io_listen loop.
type CTLModule struct {
	mu        sync.RWMutex
	fifoName  string
	sockName  string
	sockMode  uint32
	listeners []net.Listener
	conns     map[net.Conn]struct{}
	fifoFile  *os.File
	handlers  map[string]CommandHandler
	started   bool
	stop      chan struct{}
	wg        sync.WaitGroup
}

// JSON-RPC 2.0 wire types.

type jsonRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params,omitempty"`
	ID      interface{}            `json:"id"`
}

type jsonResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	errParse    = -32700
	errInvalid  = -32600
	errMethod   = -32601
	errInternal = -32603
)

// New creates a CTLModule that is not yet listening.
func New() *CTLModule {
	return &CTLModule{
		handlers: make(map[string]CommandHandler),
		conns:    make(map[net.Conn]struct{}),
	}
}

// Init configures the listener endpoints and socket permission. It must be
// called before Start. Calling it after Start has no effect and returns an
// error.
func (m *CTLModule) Init(fifoName, sockName string, mode uint32) error {
	if m == nil {
		return errors.New("ctl: nil module")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("ctl: already started")
	}
	m.fifoName = fifoName
	m.sockName = sockName
	m.sockMode = mode
	return nil
}

// FIFOName returns the configured FIFO path.
func (m *CTLModule) FIFOName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.fifoName
}

// SockName returns the configured Unix socket path.
func (m *CTLModule) SockName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sockName
}

// SockMode returns the configured socket permission bits.
func (m *CTLModule) SockMode() uint32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sockMode
}

// RegisterCommand registers a named command handler. Registering the same
// name twice is an error.
func (m *CTLModule) RegisterCommand(name string, handler CommandHandler) error {
	if m == nil {
		return errors.New("ctl: nil module")
	}
	if name == "" {
		return errors.New("ctl: empty command name")
	}
	if handler == nil {
		return errors.New("ctl: nil handler")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.handlers[name]; ok {
		return fmt.Errorf("ctl: command %q already registered", name)
	}
	m.handlers[name] = handler
	return nil
}

// ExecuteCommand looks up and runs a registered command.
func (m *CTLModule) ExecuteCommand(name string, params map[string]string) (interface{}, error) {
	if m == nil {
		return nil, errors.New("ctl: nil module")
	}
	m.mu.RLock()
	handler, ok := m.handlers[name]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("ctl: unknown command %q", name)
	}
	return handler(params)
}

// ListCommands returns the sorted names of all registered commands.
func (m *CTLModule) ListCommands() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.handlers))
	for name := range m.handlers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Start opens the configured FIFO and/or Unix socket listeners and begins
// accepting connections in the background.
func (m *CTLModule) Start() error {
	if m == nil {
		return errors.New("ctl: nil module")
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("ctl: already started")
	}
	m.stop = make(chan struct{})
	m.listeners = nil
	m.conns = make(map[net.Conn]struct{})
	m.mu.Unlock()

	// Unix socket listener.
	if m.sockName != "" {
		ln, err := m.listenUnix()
		if err != nil {
			m.cleanupStartFailure()
			return err
		}
		m.mu.Lock()
		m.listeners = append(m.listeners, ln)
		m.mu.Unlock()
		m.wg.Add(1)
		go m.acceptLoop(ln)
	}

	// FIFO listener.
	if m.fifoName != "" {
		f, err := m.listenFIFO()
		if err != nil {
			m.cleanupStartFailure()
			return err
		}
		m.mu.Lock()
		m.fifoFile = f
		m.mu.Unlock()
		m.wg.Add(1)
		go m.fifoLoop(f)
	}

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	return nil
}

// listenUnix creates the Unix domain socket listener, removing any stale
// socket file first.
func (m *CTLModule) listenUnix() (net.Listener, error) {
	_ = os.Remove(m.sockName)
	ln, err := net.Listen("unix", m.sockName)
	if err != nil {
		return nil, fmt.Errorf("ctl: listen unix %q: %w", m.sockName, err)
	}
	if m.sockMode != 0 {
		_ = os.Chmod(m.sockName, os.FileMode(m.sockMode))
	}
	return ln, nil
}

// listenFIFO creates the named pipe and opens it read/write so the server
// read loop does not block when no client writer is connected.
func (m *CTLModule) listenFIFO() (*os.File, error) {
	mode := m.sockMode
	if mode == 0 {
		mode = 0660
	}
	if err := syscall.Mkfifo(m.fifoName, mode); err != nil {
		return nil, fmt.Errorf("ctl: mkfifo %q: %w", m.fifoName, err)
	}
	fd, err := syscall.Open(m.fifoName, syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("ctl: open fifo %q: %w", m.fifoName, err)
	}
	return os.NewFile(uintptr(fd), m.fifoName), nil
}

// acceptLoop accepts Unix socket connections and hands each to
// HandleConnection in its own goroutine.
func (m *CTLModule) acceptLoop(ln net.Listener) {
	defer m.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		select {
		case <-m.stop:
			conn.Close()
			return
		default:
		}
		m.wg.Add(1)
		go func(c net.Conn) {
			defer m.wg.Done()
			m.HandleConnection(c)
		}(conn)
	}
}

// fifoLoop reads newline-delimited JSON-RPC requests from the FIFO and writes
// responses back to the same file descriptor.
func (m *CTLModule) fifoLoop(f *os.File) {
	defer m.wg.Done()
	defer f.Close()
	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var req jsonRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			m.writeFIFOResponse(f, jsonResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: errParse, Message: "parse error"},
			})
			continue
		}
		m.writeFIFOResponse(f, m.dispatchRequest(&req))
	}
}

func (m *CTLModule) writeFIFOResponse(f *os.File, resp jsonResponse) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = f.Write(b)
}

// HandleConnection serves JSON-RPC requests over a single connection until
// the peer closes it or an I/O error occurs.
func (m *CTLModule) HandleConnection(conn net.Conn) {
	if m == nil || conn == nil {
		return
	}
	m.mu.Lock()
	m.conns[conn] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.conns, conn)
		m.mu.Unlock()
		conn.Close()
	}()

	dec := json.NewDecoder(conn)
	for {
		var req jsonRequest
		if err := dec.Decode(&req); err != nil {
			return
		}
		resp := m.dispatchRequest(&req)
		b, err := json.Marshal(resp)
		if err != nil {
			return
		}
		b = append(b, '\n')
		if _, err := conn.Write(b); err != nil {
			return
		}
	}
}

// dispatchRequest converts the JSON-RPC params to strings and runs the
// command, mapping the outcome to a JSON-RPC response.
func (m *CTLModule) dispatchRequest(req *jsonRequest) jsonResponse {
	resp := jsonResponse{JSONRPC: "2.0", ID: req.ID}
	if req.Method == "" {
		resp.Error = &rpcError{Code: errInvalid, Message: "invalid request: missing method"}
		return resp
	}
	params := toStringMap(req.Params)
	result, err := m.ExecuteCommand(req.Method, params)
	if err != nil {
		resp.Error = &rpcError{Code: errInternal, Message: err.Error()}
		return resp
	}
	resp.Result = result
	return resp
}

// Stop closes all listeners and active connections and waits for the accept
// loops to terminate. It is safe to call when not started.
func (m *CTLModule) Stop() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = false
	close(m.stop)
	listeners := m.listeners
	conns := make([]net.Conn, 0, len(m.conns))
	for c := range m.conns {
		conns = append(conns, c)
	}
	fifo := m.fifoFile
	m.mu.Unlock()

	for _, ln := range listeners {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	if fifo != nil {
		_ = fifo.Close()
	}
	m.wg.Wait()

	m.mu.Lock()
	m.listeners = nil
	m.conns = nil
	m.fifoFile = nil
	fifoName := m.fifoName
	sockName := m.sockName
	m.mu.Unlock()

	if fifoName != "" {
		_ = os.Remove(fifoName)
	}
	if sockName != "" {
		_ = os.Remove(sockName)
	}
	return nil
}

// cleanupStartFailure rolls back any partially-opened listeners when Start
// aborts midway.
func (m *CTLModule) cleanupStartFailure() {
	m.mu.Lock()
	listeners := m.listeners
	fifo := m.fifoFile
	m.listeners = nil
	m.fifoFile = nil
	m.stop = nil
	m.mu.Unlock()
	for _, ln := range listeners {
		_ = ln.Close()
	}
	if fifo != nil {
		_ = fifo.Close()
	}
}

// toStringMap converts a JSON-decoded parameter object into a string map.
func toStringMap(params map[string]interface{}) map[string]string {
	out := make(map[string]string, len(params))
	for k, v := range params {
		out[k] = toStr(v)
	}
	return out
}

// toStr renders a JSON-decoded scalar as a string.
func toStr(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ---------------------------------------------------------------------------
// Package-level singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *CTLModule
)

// DefaultCTL returns the process-wide module, creating it on first use.
func DefaultCTL() *CTLModule {
	defaultMu.RLock()
	mm := defaultM
	defaultMu.RUnlock()
	if mm != nil {
		return mm
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init is the package-level (re)initialiser for the default module.
func Init(fifoName, sockName string, mode uint32) error {
	return DefaultCTL().Init(fifoName, sockName, mode)
}

// Start is the package-level wrapper.
func Start() error { return DefaultCTL().Start() }

// Stop is the package-level wrapper.
func Stop() error { return DefaultCTL().Stop() }

// RegisterCommand is the package-level wrapper.
func RegisterCommand(name string, handler CommandHandler) error {
	return DefaultCTL().RegisterCommand(name, handler)
}

// ExecuteCommand is the package-level wrapper.
func ExecuteCommand(name string, params map[string]string) (interface{}, error) {
	return DefaultCTL().ExecuteCommand(name, params)
}

// ListCommands is the package-level wrapper.
func ListCommands() []string { return DefaultCTL().ListCommands() }
