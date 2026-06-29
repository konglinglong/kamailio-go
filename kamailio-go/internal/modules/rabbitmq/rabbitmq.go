// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * rabbitmq module - RabbitMQ (AMQP) producer/consumer integration.
 *
 * This Go counterpart is an in-memory simulation of a RabbitMQ client:
 * it tracks exchanges, queues and bindings in memory and routes
 * published messages to bound queues, so the message flow can be
 * exercised without a real RabbitMQ broker.
 *
 * It is safe for concurrent use.
 */

package rabbitmq

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// RabbitMQConfig holds the connection parameters for a RabbitMQ broker.
type RabbitMQConfig struct {
	URL        string
	Exchange   string
	Queue      string
	RoutingKey string
}

// RabbitMQMessage is a single message published to an exchange.
type RabbitMQMessage struct {
	Exchange   string
	RoutingKey string
	Body       []byte
	Headers    map[string]interface{}
	Timestamp  time.Time
}

// queueInfo holds the state of a declared queue.
type queueInfo struct {
	durable  bool
	messages []*RabbitMQMessage
}

// binding links a queue to an exchange with a routing key.
type binding struct {
	queue      string
	exchange   string
	routingKey string
}

// RabbitMQModule is an in-memory RabbitMQ producer/consumer.
type RabbitMQModule struct {
	mu        sync.RWMutex
	cfg       RabbitMQConfig
	connected bool
	queues    map[string]*queueInfo
	bindings  []binding
	consumers map[string][]func(*RabbitMQMessage)
	closed    bool

	pending  sync.WaitGroup
	pendingN atomic.Int64
}

// New creates a RabbitMQModule that is not yet connected.
func New() *RabbitMQModule {
	return &RabbitMQModule{
		queues:    make(map[string]*queueInfo),
		consumers: make(map[string][]func(*RabbitMQMessage)),
	}
}

// Init (re)configures the module from cfg. A nil cfg resets the module
// to a disconnected state. A non-nil cfg with a non-empty URL marks the
// module connected.
//
//	C: mod_init() / amqp connection
func (m *RabbitMQModule) Init(cfg *RabbitMQConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("rabbitmq: module closed")
	}
	m.queues = make(map[string]*queueInfo)
	m.bindings = nil
	m.consumers = make(map[string][]func(*RabbitMQMessage))
	if cfg == nil {
		m.cfg = RabbitMQConfig{}
		m.connected = false
		return nil
	}
	m.cfg = *cfg
	if cfg.URL == "" {
		m.connected = false
		return nil
	}
	m.connected = true
	return nil
}

// DeclareQueue declares a queue with the given name. If the queue
// already exists it is updated to the requested durability. Returns an
// error if the module is closed or not connected, or if name is empty.
//
//	C: amqp_queue_declare()
func (m *RabbitMQModule) DeclareQueue(name string, durable bool) error {
	if name == "" {
		return errors.New("rabbitmq: empty queue name")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("rabbitmq: module closed")
	}
	if !m.connected {
		return errors.New("rabbitmq: not connected")
	}
	if q, ok := m.queues[name]; ok {
		q.durable = durable
		return nil
	}
	m.queues[name] = &queueInfo{durable: durable}
	return nil
}

// BindQueue binds a queue to an exchange with a routing key. Messages
// published to the exchange with a matching routing key are delivered
// to the queue. Returns an error if the module is closed or not
// connected, or if the queue has not been declared.
//
//	C: amqp_queue_bind()
func (m *RabbitMQModule) BindQueue(queue, exchange, routingKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("rabbitmq: module closed")
	}
	if !m.connected {
		return errors.New("rabbitmq: not connected")
	}
	if _, ok := m.queues[queue]; !ok {
		return errors.New("rabbitmq: queue not declared: " + queue)
	}
	m.bindings = append(m.bindings, binding{queue: queue, exchange: exchange, routingKey: routingKey})
	return nil
}

// Publish routes a message to all queues bound to the message's
// exchange with a matching routing key. Each matching queue receives a
// copy of the message and its registered consumers are invoked.
// Returns an error if the module is closed or not connected.
//
//	C: amqp_basic_publish()
func (m *RabbitMQModule) Publish(msg *RabbitMQMessage) error {
	if msg == nil {
		return errors.New("rabbitmq: nil message")
	}
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return errors.New("rabbitmq: module closed")
	}
	if !m.connected {
		m.mu.RUnlock()
		return errors.New("rabbitmq: not connected")
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	// Collect matching queues and their consumers.
	var targets []string
	for _, b := range m.bindings {
		if b.exchange == msg.Exchange && b.routingKey == msg.RoutingKey {
			targets = append(targets, b.queue)
		}
	}
	m.mu.RUnlock()

	m.mu.Lock()
	for _, qn := range targets {
		if q, ok := m.queues[qn]; ok {
			q.messages = append(q.messages, msg)
		}
	}
	// Snapshot consumers per target queue.
	dispatch := make(map[string][]func(*RabbitMQMessage))
	for _, qn := range targets {
		hs := m.consumers[qn]
		cp := make([]func(*RabbitMQMessage), len(hs))
		copy(cp, hs)
		dispatch[qn] = cp
	}
	m.mu.Unlock()

	for _, qn := range targets {
		for _, h := range dispatch[qn] {
			h(msg)
		}
	}
	return nil
}

// PublishAsync publishes a message in a background goroutine and
// invokes callback (which may be nil) with the result. The request is
// tracked by the pending WaitGroup so Close can drain in-flight work.
func (m *RabbitMQModule) PublishAsync(msg *RabbitMQMessage, callback func(error)) {
	m.pending.Add(1)
	m.pendingN.Add(1)
	go func() {
		defer m.pending.Done()
		defer m.pendingN.Add(-1)
		err := m.Publish(msg)
		if callback != nil {
			callback(err)
		}
	}()
}

// Consume registers a handler for a queue. Every subsequently routed
// message to that queue is delivered to the handler. Returns an error
// if the module is closed or not connected, or if the queue has not
// been declared.
//
//	C: amqp_basic_consume()
func (m *RabbitMQModule) Consume(queue string, handler func(*RabbitMQMessage)) error {
	if handler == nil {
		return errors.New("rabbitmq: nil handler")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("rabbitmq: module closed")
	}
	if !m.connected {
		return errors.New("rabbitmq: not connected")
	}
	if _, ok := m.queues[queue]; !ok {
		return errors.New("rabbitmq: queue not declared: " + queue)
	}
	m.consumers[queue] = append(m.consumers[queue], handler)
	return nil
}

// Close shuts the module down, clearing queues, bindings and consumers
// and waiting for in-flight async publishes to finish. It is safe to
// call multiple times.
func (m *RabbitMQModule) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.connected = false
	m.queues = make(map[string]*queueInfo)
	m.bindings = nil
	m.consumers = make(map[string][]func(*RabbitMQMessage))
	m.mu.Unlock()

	m.pending.Wait()
}

// IsConnected reports whether the module has been initialised with a
// valid broker URL.
func (m *RabbitMQModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// QueueMessages returns a snapshot of the messages routed to a queue.
// This is primarily useful for tests and inspection.
func (m *RabbitMQModule) QueueMessages(queue string) []*RabbitMQMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.queues[queue]
	if !ok {
		return nil
	}
	out := make([]*RabbitMQMessage, len(q.messages))
	copy(out, q.messages)
	return out
}

// Bindings returns a snapshot of the current bindings. This is
// primarily useful for tests and inspection.
func (m *RabbitMQModule) Bindings() []binding {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]binding, len(m.bindings))
	copy(out, m.bindings)
	return out
}

// Config returns a copy of the current configuration.
func (m *RabbitMQModule) Config() RabbitMQConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// --- package-level API ---

var defaultMu sync.RWMutex
var defaultModule = New()

// DefaultRabbitMQ returns the package-level default RabbitMQModule.
func DefaultRabbitMQ() *RabbitMQModule {
	defaultMu.RLock()
	m := defaultModule
	defaultMu.RUnlock()
	return m
}

// Init (re)initialises the package-level default module to a fresh
// state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultModule = New()
}
