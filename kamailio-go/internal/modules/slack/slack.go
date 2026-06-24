// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Slack module - Slack webhook integration.
 * Port of the kamailio slack module (src/modules/slack).
 *
 * Posts messages and alerts to a Slack incoming webhook. The actual HTTP
 * delivery is delegated to a pluggable Poster function (default no-op) so
 * the module is usable and testable without network access; every message
 * is also recorded for inspection.
 *
 * It is safe for concurrent use.
 */
package slack

import (
	"fmt"
	"sync"
)

// slackMessage is the recorded form of a dispatched message.
type slackMessage struct {
	text  string
	alert bool
	title string
}

// Poster delivers a payload string to the configured webhook URL. The
// default poster always succeeds; tests may inject a mock to assert the
// payload or simulate failures.
type Poster func(webhookURL, payload string) error

// defaultPoster always succeeds without performing any I/O.
func defaultPoster(webhookURL, payload string) error { return nil }

// SlackModule implements the slack module functionality.
// C: struct module slack
type SlackModule struct {
	mu        sync.Mutex
	webhook   string
	connected bool
	sent      []slackMessage
	poster    Poster
}

// NewSlackModule creates a SlackModule with the default (no-op) poster and
// no connection.
func NewSlackModule() *SlackModule {
	return &SlackModule{poster: defaultPoster}
}

// Init configures the webhook URL and marks the module connected. Passing
// an empty URL leaves the module disconnected.
// C: slack_init()
func (m *SlackModule) Init(webhookURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.webhook = webhookURL
	m.connected = webhookURL != ""
}

// SetPoster replaces the delivery poster. Passing nil restores the default
// (no-op) poster.
func (m *SlackModule) SetPoster(p Poster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p == nil {
		m.poster = defaultPoster
		return
	}
	m.poster = p
}

// Send posts message to the configured webhook. Returns an error when the
// module is not connected.
// C: slack_send()
func (m *SlackModule) Send(message string) error {
	m.mu.Lock()
	webhook := m.webhook
	connected := m.connected
	poster := m.poster
	if !connected {
		m.mu.Unlock()
		return fmt.Errorf("slack: not connected")
	}
	m.sent = append(m.sent, slackMessage{text: message})
	m.mu.Unlock()
	return poster(webhook, message)
}

// SendAlert posts an alert with a title and message to the configured
// webhook. Returns an error when the module is not connected.
// C: slack_send_alert()
func (m *SlackModule) SendAlert(title, message string) error {
	m.mu.Lock()
	webhook := m.webhook
	connected := m.connected
	poster := m.poster
	if !connected {
		m.mu.Unlock()
		return fmt.Errorf("slack: not connected")
	}
	m.sent = append(m.sent, slackMessage{text: message, alert: true, title: title})
	m.mu.Unlock()
	payload := fmt.Sprintf("ALERT: %s - %s", title, message)
	return poster(webhook, payload)
}

// IsConnected reports whether Init has configured a usable webhook.
func (m *SlackModule) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// SentCount returns the number of messages dispatched via Send/SendAlert.
func (m *SlackModule) SentCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu    sync.RWMutex
	defaultSlack *SlackModule
)

// DefaultSlack returns the process-wide SlackModule, creating one on first
// use.
func DefaultSlack() *SlackModule {
	defaultMu.RLock()
	m := defaultSlack
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSlack == nil {
		defaultSlack = NewSlackModule()
	}
	return defaultSlack
}

// Init (re)initialises the process-wide SlackModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSlack = NewSlackModule()
}
