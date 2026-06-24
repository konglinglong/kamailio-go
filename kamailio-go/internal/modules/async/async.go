// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Async module - asynchronous route execution.
 * Port of the kamailio async module (src/modules/async).
 *
 * The async module dispatches SIP messages to named route handlers and
 * provides a non-blocking sleep helper. Route handlers are registered
 * Go functions. It is safe for concurrent use.
 */

package async

import (
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RouteHandler is invoked by Route for a matching route name.
type RouteHandler func(msg *parser.SIPMsg) int

// AsyncModule maintains a registry of named route handlers.
type AsyncModule struct {
	mu     sync.RWMutex
	routes map[string]RouteHandler
	ready  bool
}

// New creates an AsyncModule with empty route storage, marked ready.
func New() *AsyncModule {
	return &AsyncModule{routes: make(map[string]RouteHandler), ready: true}
}

// Register associates a handler with a route name, overwriting any
// previous handler for that name.
func (m *AsyncModule) Register(routeName string, h RouteHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.routes == nil {
		m.routes = make(map[string]RouteHandler)
	}
	m.routes[routeName] = h
}

// Route dispatches msg to the handler registered for routeName. It
// returns the handler's result, or -1 when no handler is registered.
//
//	C: async_route()
func (m *AsyncModule) Route(routeName string, msg *parser.SIPMsg) int {
	m.mu.RLock()
	h, ok := m.routes[routeName]
	m.mu.RUnlock()
	if !ok || h == nil {
		return -1
	}
	return h(msg)
}

// Sleep blocks the calling goroutine for ms milliseconds.
//
//	C: async_sleep()
func (m *AsyncModule) Sleep(ms int) {
	if ms <= 0 {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// IsReady returns true when the module is ready to dispatch routes.
func (m *AsyncModule) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ready
}

// SetReady toggles the ready flag.
func (m *AsyncModule) SetReady(ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ready = ready
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *AsyncModule
)

// DefaultAsync returns the process-wide AsyncModule, creating it on first use.
func DefaultAsync() *AsyncModule {
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

// Init (re)initialises the process-wide AsyncModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Register is the package-level wrapper around DefaultAsync().Register.
func Register(routeName string, h RouteHandler) { DefaultAsync().Register(routeName, h) }

// Route is the package-level wrapper around DefaultAsync().Route.
func Route(routeName string, msg *parser.SIPMsg) int { return DefaultAsync().Route(routeName, msg) }

// Sleep is the package-level wrapper around DefaultAsync().Sleep.
func Sleep(ms int) { DefaultAsync().Sleep(ms) }

// IsReady is the package-level wrapper around DefaultAsync().IsReady.
func IsReady() bool { return DefaultAsync().IsReady() }
