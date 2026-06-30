// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Kafka adapter for the acc.Backend interface.
 *
 * CDRBackend wraps a KafkaModule so CDRs flushed by the accounting
 * service are published as JSON-encoded messages to a configured
 * topic. The underlying KafkaModule owns the connection lifecycle
 * (in-memory simulation today; a real sarama-backed implementation
 * can be dropped in later behind the same KafkaModule API).
 */

package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

// CDRBackend adapts a KafkaModule to the acc.Backend interface. Each
// CDR is JSON-encoded and produced to Topic, keyed by Call-ID so that
// all records for a single call land in the same partition.
type CDRBackend struct {
	module *KafkaModule
	topic  string
}

// NewCDRBackend wraps m as an acc.Backend that publishes CDRs to topic.
// A blank topic falls back to "cdr". The caller retains ownership of m's
// lifecycle (e.g. Close); the backend only produces through it.
func NewCDRBackend(m *KafkaModule, topic string) *CDRBackend {
	if topic == "" {
		topic = "cdr"
	}
	return &CDRBackend{module: m, topic: topic}
}

// Write serialises cdr to JSON and produces it to the configured topic.
// It implements acc.Backend.Write.
func (b *CDRBackend) Write(_ context.Context, cdr *acc.CDR) error {
	if b == nil || b.module == nil {
		return nil
	}
	if cdr == nil {
		return nil
	}
	payload, err := json.Marshal(cdr)
	if err != nil {
		return fmt.Errorf("kafka cdr: marshal: %w", err)
	}
	msg := &KafkaMessage{
		Topic:     b.topic,
		Key:       cdr.CallID,
		Value:     payload,
		Timestamp: time.Now(),
		Headers: map[string]string{
			"content-type": "application/json",
			"method":       cdr.Method,
		},
	}
	return b.module.Produce(msg)
}

// Close is a no-op: the KafkaModule lifecycle is owned by the bootstrap.
// It satisfies acc.Backend.Close so the backend can sit alongside
// persistent ones in the AccountingService.
func (b *CDRBackend) Close() error { return nil }

// Topic returns the topic CDRs are published to.
func (b *CDRBackend) Topic() string {
	if b == nil {
		return ""
	}
	return b.topic
}

// Module returns the underlying KafkaModule, primarily so RPC handlers
// and tests can inspect produced messages.
func (b *CDRBackend) Module() *KafkaModule {
	if b == nil {
		return nil
	}
	return b.module
}
