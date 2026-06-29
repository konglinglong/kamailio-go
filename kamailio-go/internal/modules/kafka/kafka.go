// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * kafka module - Apache Kafka producer/consumer integration.
 *
 * This Go counterpart is an in-memory simulation of a Kafka client: it
 * tracks brokers, topics and consumer groups in memory and delivers
 * produced messages to registered handlers, so the message flow can be
 * exercised without a real Kafka cluster.
 *
 * It is safe for concurrent use.
 */

package kafka

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// KafkaConfig holds the connection parameters for a Kafka cluster.
type KafkaConfig struct {
	Brokers  []string
	Topic    string
	GroupID  string
	Username string
	Password string
}

// KafkaMessage is a single record produced to or consumed from a topic.
type KafkaMessage struct {
	Topic     string
	Key       string
	Value     []byte
	Headers   map[string]string
	Timestamp time.Time
}

// KafkaModule is an in-memory Kafka producer/consumer.
type KafkaModule struct {
	mu        sync.RWMutex
	cfg       KafkaConfig
	connected bool
	topics    map[string][]*KafkaMessage
	consumers map[string][]func(*KafkaMessage)
	closed    bool

	pending  sync.WaitGroup
	pendingN atomic.Int64
}

// New creates a KafkaModule that is not yet connected.
func New() *KafkaModule {
	return &KafkaModule{
		topics:    make(map[string][]*KafkaMessage),
		consumers: make(map[string][]func(*KafkaMessage)),
	}
}

// Init (re)configures the module from cfg. A nil cfg resets the module
// to a disconnected state. A non-nil cfg with at least one broker marks
// the module connected.
//
//	C: mod_init()
func (m *KafkaModule) Init(cfg *KafkaConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("kafka: module closed")
	}
	m.topics = make(map[string][]*KafkaMessage)
	m.consumers = make(map[string][]func(*KafkaMessage))
	if cfg == nil {
		m.cfg = KafkaConfig{}
		m.connected = false
		return nil
	}
	m.cfg = *cfg
	if len(cfg.Brokers) == 0 {
		m.connected = false
		return nil
	}
	m.connected = true
	return nil
}

// Produce synchronously publishes a message to its topic. The message
// is appended to the topic log and delivered to every registered
// consumer for that topic. Returns an error if the module is closed or
// not connected, or if the message has no topic.
//
//	C: kafka_send_message()
func (m *KafkaModule) Produce(msg *KafkaMessage) error {
	if msg == nil {
		return errors.New("kafka: nil message")
	}
	if msg.Topic == "" {
		return errors.New("kafka: empty topic")
	}
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return errors.New("kafka: module closed")
	}
	if !m.connected {
		m.mu.RUnlock()
		return errors.New("kafka: not connected")
	}
	topic := msg.Topic
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	m.mu.RUnlock()

	m.mu.Lock()
	m.topics[topic] = append(m.topics[topic], msg)
	handlers := make([]func(*KafkaMessage), len(m.consumers[topic]))
	copy(handlers, m.consumers[topic])
	m.mu.Unlock()

	for _, h := range handlers {
		h(msg)
	}
	return nil
}

// ProduceAsync publishes a message in a background goroutine and
// invokes callback (which may be nil) with the result. The request is
// tracked by the pending WaitGroup so Close can drain in-flight work.
// The pending counter is decremented before the callback fires so that
// PendingCount is accurate by the time the caller is notified.
//
//	C: kafka_send_message() with async semantics
func (m *KafkaModule) ProduceAsync(msg *KafkaMessage, callback func(error)) {
	m.pending.Add(1)
	m.pendingN.Add(1)
	go func() {
		defer m.pending.Done()
		err := m.Produce(msg)
		m.pendingN.Add(-1)
		if callback != nil {
			callback(err)
		}
	}()
}

// Consume registers a handler for a topic. Every subsequently produced
// message on that topic is delivered to the handler. Returns an error
// if the module is closed or not connected.
//
//	C: kafka_consume_topic()
func (m *KafkaModule) Consume(topic string, handler func(*KafkaMessage)) error {
	if handler == nil {
		return errors.New("kafka: nil handler")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("kafka: module closed")
	}
	if !m.connected {
		return errors.New("kafka: not connected")
	}
	m.consumers[topic] = append(m.consumers[topic], handler)
	return nil
}

// Close shuts the module down, clearing topic logs and consumer
// registrations and waiting for in-flight async produces to finish.
// It is safe to call multiple times.
func (m *KafkaModule) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.connected = false
	m.topics = make(map[string][]*KafkaMessage)
	m.consumers = make(map[string][]func(*KafkaMessage))
	m.mu.Unlock()

	m.pending.Wait()
}

// IsConnected reports whether the module has been initialised with a
// valid broker list.
func (m *KafkaModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// PendingCount returns the number of in-flight async produce requests.
func (m *KafkaModule) PendingCount() int {
	return int(m.pendingN.Load())
}

// Messages returns a snapshot of the messages logged for a topic. This
// is primarily useful for tests and inspection.
func (m *KafkaModule) Messages(topic string) []*KafkaMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.topics[topic]
	out := make([]*KafkaMessage, len(src))
	copy(out, src)
	return out
}

// Config returns a copy of the current configuration.
func (m *KafkaModule) Config() KafkaConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// --- package-level API ---

var defaultMu sync.RWMutex
var defaultModule = New()

// DefaultKafka returns the package-level default KafkaModule.
func DefaultKafka() *KafkaModule {
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
