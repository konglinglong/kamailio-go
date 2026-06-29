// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * nsq module - NSQ producer/consumer integration.
 *
 * This Go counterpart is an in-memory simulation of an NSQ client:
 * it tracks topic/channel subscriptions and delivers published
 * messages to every channel subscribed to a topic, so the message
 * flow can be exercised without a real nsqd instance.
 *
 * It is safe for concurrent use.
 */

package nsq

import (
	"errors"
	"sync"
	"time"
)

// NSQConfig holds the connection parameters for nsqd / nsqlookupd.
type NSQConfig struct {
	NSQDAddr    string
	LookupDAddr string
	Topic       string
	Channel     string
}

// NSQMessage is a single message published to a topic.
type NSQMessage struct {
	Topic     string
	Body      []byte
	Timestamp time.Time
	Attempts  int
}

// subKey is the composite key for a (topic, channel) subscription.
type subKey struct {
	topic   string
	channel string
}

// NSQModule is an in-memory NSQ producer/consumer.
type NSQModule struct {
	mu        sync.RWMutex
	cfg       NSQConfig
	connected bool
	subs      map[subKey][]func(*NSQMessage)
	topics    map[string][]*NSQMessage
	closed    bool
}

// New creates an NSQModule that is not yet connected.
func New() *NSQModule {
	return &NSQModule{
		subs:   make(map[subKey][]func(*NSQMessage)),
		topics: make(map[string][]*NSQMessage),
	}
}

// Init (re)configures the module from cfg. A nil cfg resets the module
// to a disconnected state. A non-nil cfg with a non-empty NSQDAddr or
// LookupDAddr marks the module connected.
//
//	C: mod_init() / nsq connection
func (m *NSQModule) Init(cfg *NSQConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("nsq: module closed")
	}
	m.subs = make(map[subKey][]func(*NSQMessage))
	m.topics = make(map[string][]*NSQMessage)
	if cfg == nil {
		m.cfg = NSQConfig{}
		m.connected = false
		return nil
	}
	m.cfg = *cfg
	if cfg.NSQDAddr == "" && cfg.LookupDAddr == "" {
		m.connected = false
		return nil
	}
	m.connected = true
	return nil
}

// Publish delivers a message to every channel subscribed to the topic.
// The message is also appended to the topic log. Returns an error if
// the module is closed or not connected, or if the topic is empty.
//
//	C: nsq_publish()
func (m *NSQModule) Publish(topic string, body []byte) error {
	if topic == "" {
		return errors.New("nsq: empty topic")
	}
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return errors.New("nsq: module closed")
	}
	if !m.connected {
		m.mu.RUnlock()
		return errors.New("nsq: not connected")
	}
	m.mu.RUnlock()

	msg := &NSQMessage{
		Topic:     topic,
		Body:      body,
		Timestamp: time.Now(),
		Attempts:  0,
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("nsq: module closed")
	}
	m.topics[topic] = append(m.topics[topic], msg)
	// Snapshot handlers per channel subscribed to this topic.
	dispatch := make(map[subKey][]func(*NSQMessage))
	for key, hs := range m.subs {
		if key.topic == topic {
			cp := make([]func(*NSQMessage), len(hs))
			copy(cp, hs)
			dispatch[key] = cp
		}
	}
	m.mu.Unlock()

	for _, hs := range dispatch {
		for _, h := range hs {
			h(msg)
		}
	}
	return nil
}

// Subscribe registers a handler for a (topic, channel) pair. Every
// subsequently published message on the topic is delivered to the
// handler. Returns an error if the module is closed or not connected,
// or if the topic or channel is empty.
//
//	C: nsq_subscribe()
func (m *NSQModule) Subscribe(topic, channel string, handler func(*NSQMessage)) error {
	if topic == "" {
		return errors.New("nsq: empty topic")
	}
	if channel == "" {
		return errors.New("nsq: empty channel")
	}
	if handler == nil {
		return errors.New("nsq: nil handler")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("nsq: module closed")
	}
	if !m.connected {
		return errors.New("nsq: not connected")
	}
	key := subKey{topic: topic, channel: channel}
	m.subs[key] = append(m.subs[key], handler)
	return nil
}

// Unsubscribe removes the subscription for a (topic, channel) pair.
// Returns an error if the module is closed or not connected. It is not
// an error to unsubscribe a pair that was never subscribed.
//
//	C: nsq_unsubscribe()
func (m *NSQModule) Unsubscribe(topic, channel string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("nsq: module closed")
	}
	if !m.connected {
		return errors.New("nsq: not connected")
	}
	delete(m.subs, subKey{topic: topic, channel: channel})
	return nil
}

// Close shuts the module down, clearing subscriptions and topic logs.
// It is safe to call multiple times.
func (m *NSQModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.connected = false
	m.subs = make(map[subKey][]func(*NSQMessage))
	m.topics = make(map[string][]*NSQMessage)
}

// IsConnected reports whether the module has been initialised with a
// valid nsqd or nsqlookupd address.
func (m *NSQModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// Messages returns a snapshot of the messages logged for a topic. This
// is primarily useful for tests and inspection.
func (m *NSQModule) Messages(topic string) []*NSQMessage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.topics[topic]
	out := make([]*NSQMessage, len(src))
	copy(out, src)
	return out
}

// Subscriptions returns a snapshot of the (topic, channel) pairs that
// currently have handlers. This is primarily useful for tests and
// inspection.
func (m *NSQModule) Subscriptions() []subKey {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]subKey, 0, len(m.subs))
	for k := range m.subs {
		out = append(out, k)
	}
	return out
}

// Config returns a copy of the current configuration.
func (m *NSQModule) Config() NSQConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// --- package-level API ---

var defaultMu sync.RWMutex
var defaultModule = New()

// DefaultNSQ returns the package-level default NSQModule.
func DefaultNSQ() *NSQModule {
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
