// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Event API module - event dispatch to subscribed handlers.
 * Port of the kamailio evapi module (src/modules/evapi).
 *
 * The module maintains a list of subscriber handlers and dispatches
 * (event, data) pairs to all of them. It is safe for concurrent use.
 */

package evapi

import (
	"errors"
	"sync"
)

// EventHandler is invoked by Dispatch for each subscriber.
type EventHandler func(event string, data []byte)

// EVAPIModule maintains a set of event subscribers.
type EVAPIModule struct {
	mu          sync.RWMutex
	addr        string
	running     bool
	subscribers []EventHandler
}

// New creates an EVAPIModule with no subscribers, not running.
func New() *EVAPIModule {
	return &EVAPIModule{}
}

// Init configures the listening address and marks the module running.
//
//	C: evapi_init()
func (m *EVAPIModule) Init(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addr = addr
	m.running = true
}

// IsRunning returns true when the module has been initialised.
func (m *EVAPIModule) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// Subscribe registers a handler that will be invoked on every Dispatch.
//
//	C: evapi_subscribe()
func (m *EVAPIModule) Subscribe(handler EventHandler) {
	if handler == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = append(m.subscribers, handler)
}

// Dispatch invokes every registered subscriber with the given event and
// data. It returns an error when the module is not running.
//
//	C: evapi_dispatch()
func (m *EVAPIModule) Dispatch(event string, data []byte) error {
	m.mu.RLock()
	running := m.running
	subs := make([]EventHandler, len(m.subscribers))
	copy(subs, m.subscribers)
	m.mu.RUnlock()
	if !running {
		return errors.New("evapi: not running")
	}
	for _, h := range subs {
		h(event, data)
	}
	return nil
}

// SubscriberCount returns the number of registered subscribers.
func (m *EVAPIModule) SubscriberCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subscribers)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *EVAPIModule
)

// DefaultEVAPI returns the process-wide EVAPIModule.
func DefaultEVAPI() *EVAPIModule {
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

// Init is the package-level wrapper that (re)initialises the process-wide
// EVAPIModule and configures the address.
func Init(addr string) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
	defaultM.addr = addr
	defaultM.running = true
}

// Dispatch is the package-level wrapper around DefaultEVAPI().Dispatch.
func Dispatch(event string, data []byte) error { return DefaultEVAPI().Dispatch(event, data) }

// Subscribe is the package-level wrapper around DefaultEVAPI().Subscribe.
func Subscribe(handler EventHandler) { DefaultEVAPI().Subscribe(handler) }

// IsRunning is the package-level wrapper around DefaultEVAPI().IsRunning.
func IsRunning() bool { return DefaultEVAPI().IsRunning() }
