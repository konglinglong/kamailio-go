// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * mqtt module - MQTT publish/subscribe integration.
 *
 * This Go counterpart is an in-memory simulation of an MQTT client:
 * it tracks topic subscriptions (with +/# wildcard support) and
 * retained messages, delivering published payloads to matching
 * subscribers, so the message flow can be exercised without a real
 * MQTT broker.
 *
 * It is safe for concurrent use.
 */

package mqtt

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// MQTTConfig holds the connection parameters for an MQTT broker.
type MQTTConfig struct {
	Broker   string
	ClientID string
	Username string
	Password string
	QoS      int
}

// MQTTMessage is a single message published to a topic.
type MQTTMessage struct {
	Topic    string
	Payload  []byte
	QoS      int
	Retained bool
}

// subscription pairs a topic filter with its handler.
type subscription struct {
	filter  string
	handler func(*MQTTMessage)
}

// MQTTModule is an in-memory MQTT publish/subscribe client.
type MQTTModule struct {
	mu        sync.RWMutex
	cfg       MQTTConfig
	connected bool
	subs      map[string]*subscription
	retained  map[string]*MQTTMessage
	closed    bool
}

// New creates an MQTTModule that is not yet connected.
func New() *MQTTModule {
	return &MQTTModule{
		subs:     make(map[string]*subscription),
		retained: make(map[string]*MQTTMessage),
	}
}

// Init (re)configures the module from cfg. A nil cfg resets the module
// to a disconnected state. A non-nil cfg with a non-empty Broker marks
// the module connected.
//
//	C: mod_init() / mqtt connect
func (m *MQTTModule) Init(cfg *MQTTConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mqtt: module closed")
	}
	m.subs = make(map[string]*subscription)
	m.retained = make(map[string]*MQTTMessage)
	if cfg == nil {
		m.cfg = MQTTConfig{}
		m.connected = false
		return nil
	}
	m.cfg = *cfg
	if cfg.Broker == "" {
		m.connected = false
		return nil
	}
	m.connected = true
	return nil
}

// Publish delivers a message to every subscriber whose topic filter
// matches the topic. If retained is true the message is stored and
// replayed to future subscribers. Returns an error if the module is
// closed or not connected, or if the topic is empty.
//
//	C: mqtt_publish()
func (m *MQTTModule) Publish(topic string, payload []byte, qos int, retained bool) error {
	if topic == "" {
		return errors.New("mqtt: empty topic")
	}
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return errors.New("mqtt: module closed")
	}
	if !m.connected {
		m.mu.RUnlock()
		return errors.New("mqtt: not connected")
	}
	// Collect matching handlers.
	var handlers []func(*MQTTMessage)
	for _, s := range m.subs {
		if topicMatches(s.filter, topic) {
			handlers = append(handlers, s.handler)
		}
	}
	m.mu.RUnlock()

	msg := &MQTTMessage{
		Topic:    topic,
		Payload:  payload,
		QoS:      qos,
		Retained: retained,
	}

	if retained {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return errors.New("mqtt: module closed")
		}
		if len(payload) == 0 {
			// A zero-length retained payload clears the retained message.
			delete(m.retained, topic)
		} else {
			m.retained[topic] = msg
		}
		m.mu.Unlock()
	}

	for _, h := range handlers {
		h(msg)
	}
	return nil
}

// Subscribe registers a handler for a topic filter (which may contain
// + and # wildcards). Any retained message matching the filter is
// delivered immediately to the new subscriber. Returns an error if the
// module is closed or not connected, or if the filter is empty.
//
//	C: mqtt_subscribe()
func (m *MQTTModule) Subscribe(topic string, handler func(*MQTTMessage)) error {
	if topic == "" {
		return errors.New("mqtt: empty topic filter")
	}
	if handler == nil {
		return errors.New("mqtt: nil handler")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("mqtt: module closed")
	}
	if !m.connected {
		m.mu.Unlock()
		return errors.New("mqtt: not connected")
	}
	m.subs[topic] = &subscription{filter: topic, handler: handler}
	// Snapshot retained messages matching the filter for replay.
	var replay []*MQTTMessage
	for t, rmsg := range m.retained {
		if topicMatches(topic, t) {
			cp := *rmsg
			replay = append(replay, &cp)
		}
	}
	m.mu.Unlock()

	for _, rmsg := range replay {
		handler(rmsg)
	}
	return nil
}

// Unsubscribe removes the subscription for a topic filter. Returns an
// error if the module is closed or not connected. It is not an error to
// unsubscribe a filter that was never subscribed.
//
//	C: mqtt_unsubscribe()
func (m *MQTTModule) Unsubscribe(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mqtt: module closed")
	}
	if !m.connected {
		return errors.New("mqtt: not connected")
	}
	delete(m.subs, topic)
	return nil
}

// Close shuts the module down, clearing subscriptions and retained
// messages. It is safe to call multiple times.
func (m *MQTTModule) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	m.connected = false
	m.subs = make(map[string]*subscription)
	m.retained = make(map[string]*MQTTMessage)
}

// IsConnected reports whether the module has been initialised with a
// valid broker address.
func (m *MQTTModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// SubscribedTopics returns a sorted snapshot of the currently
// subscribed topic filters.
func (m *MQTTModule) SubscribedTopics() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.subs))
	for f := range m.subs {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Config returns a copy of the current configuration.
func (m *MQTTModule) Config() MQTTConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// topicMatches reports whether the subscription filter matches the
// published topic. The filter supports the MQTT wildcards:
//
//   - matches exactly one topic level
//     # matches the current and all remaining levels (must be last)
func topicMatches(filter, topic string) bool {
	if filter == topic {
		return true
	}
	f := strings.Split(filter, "/")
	t := strings.Split(topic, "/")
	for i := 0; i < len(f); i++ {
		if f[i] == "#" {
			// # matches the rest (zero or more levels).
			return true
		}
		if i >= len(t) {
			return false
		}
		if f[i] == "+" {
			continue
		}
		if f[i] != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

// --- package-level API ---

var defaultMu sync.RWMutex
var defaultModule = New()

// DefaultMQTT returns the package-level default MQTTModule.
func DefaultMQTT() *MQTTModule {
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
