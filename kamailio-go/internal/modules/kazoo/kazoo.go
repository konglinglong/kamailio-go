// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * kazoo module - Kazoo (RabbitMQ/AMQP) integration.
 *
 * A simplified AMQP-style publisher/subscriber. Init configures the
 * AMQP URI; Publish fans messages out to registered queue handlers;
 * Subscribe registers a handler for a queue. No network I/O is
 * performed. The module is safe for concurrent use.
 */

package kazoo

import (
	"fmt"
	"sync"
)

// MessageHandler is invoked for each message delivered to a subscribed
// queue.
type MessageHandler func([]byte)

// KazooModule is an in-memory AMQP-style broker.
type KazooModule struct {
	mu        sync.RWMutex
	amqpURI   string
	connected bool
	queues    map[string][]MessageHandler
}

// New creates a KazooModule with no connection.
func New() *KazooModule {
	return &KazooModule{queues: make(map[string][]MessageHandler)}
}

// Init configures the AMQP URI and marks the module connected. An empty
// URI leaves the module disconnected.
//
//	C: kazoo_init()
func (m *KazooModule) Init(amqpURI string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.amqpURI = amqpURI
	m.connected = amqpURI != ""
}

// IsConnected reports whether the module has an active connection.
//
//	C: kazoo_is_connected()
func (m *KazooModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Publish delivers body to every handler subscribed to queue. Returns an
// error when not connected or the queue/routing key is empty.
//
//	C: kazoo_publish()
func (m *KazooModule) Publish(exchange, routingKey string, body []byte) error {
	if exchange == "" && routingKey == "" {
		return fmt.Errorf("kazoo: empty exchange and routing key")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.connected {
		return fmt.Errorf("kazoo: not connected")
	}
	// routingKey doubles as the queue name in this simplified model.
	handlers := m.queues[routingKey]
	for _, h := range handlers {
		// Copy body so handlers cannot mutate the caller's slice.
		cp := make([]byte, len(body))
		copy(cp, body)
		h(cp)
	}
	return nil
}

// Subscribe registers handler for queue. Returns silently when queue or
// handler is missing.
//
//	C: kazoo_subscribe()
func (m *KazooModule) Subscribe(queue string, handler MessageHandler) {
	if queue == "" || handler == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues[queue] = append(m.queues[queue], handler)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultKazoo *KazooModule
)

// DefaultKazoo returns the process-wide KazooModule, creating it on first
// use.
func DefaultKazoo() *KazooModule {
	defaultMu.RLock()
	m := defaultKazoo
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultKazoo == nil {
		defaultKazoo = New()
	}
	return defaultKazoo
}

// Init (re)initialises the process-wide KazooModule to a fresh,
// unconfigured state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultKazoo = New()
}
