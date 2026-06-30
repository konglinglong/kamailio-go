// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * RabbitMQ adapter for the acc.Backend interface.
 *
 * CDRBackend wraps a RabbitMQModule so CDRs flushed by the accounting
 * service are published as JSON-encoded messages to a configured
 * exchange/routing-key. The underlying RabbitMQModule owns the
 * connection lifecycle (in-memory simulation today; a real
 * streadway/amqp-backed implementation can be dropped in later behind
 * the same RabbitMQModule API).
 */

package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/acc"
)

// CDRBackend adapts a RabbitMQModule to the acc.Backend interface. Each
// CDR is JSON-encoded and published to Exchange with RoutingKey.
type CDRBackend struct {
	module     *RabbitMQModule
	exchange   string
	routingKey string
}

// NewCDRBackend wraps m as an acc.Backend that publishes CDRs to the
// given exchange/routing-key. Blank values fall back to sane defaults.
// The caller retains ownership of m's lifecycle (e.g. Close); the
// backend only publishes through it.
func NewCDRBackend(m *RabbitMQModule, exchange, routingKey string) *CDRBackend {
	if exchange == "" {
		exchange = "cdr"
	}
	if routingKey == "" {
		routingKey = "cdr"
	}
	return &CDRBackend{module: m, exchange: exchange, routingKey: routingKey}
}

// Write serialises cdr to JSON and publishes it to the configured
// exchange/routing-key. It implements acc.Backend.Write.
func (b *CDRBackend) Write(_ context.Context, cdr *acc.CDR) error {
	if b == nil || b.module == nil {
		return nil
	}
	if cdr == nil {
		return nil
	}
	payload, err := json.Marshal(cdr)
	if err != nil {
		return fmt.Errorf("rabbitmq cdr: marshal: %w", err)
	}
	msg := &RabbitMQMessage{
		Exchange:   b.exchange,
		RoutingKey: b.routingKey,
		Body:       payload,
		Timestamp:  time.Now(),
		Headers: map[string]interface{}{
			"content-type": "application/json",
			"method":       cdr.Method,
			"call-id":      cdr.CallID,
		},
	}
	return b.module.Publish(msg)
}

// Close is a no-op: the RabbitMQModule lifecycle is owned by the
// bootstrap. It satisfies acc.Backend.Close so the backend can sit
// alongside persistent ones in the AccountingService.
func (b *CDRBackend) Close() error { return nil }

// Exchange returns the exchange CDRs are published to.
func (b *CDRBackend) Exchange() string {
	if b == nil {
		return ""
	}
	return b.exchange
}

// RoutingKey returns the routing key CDRs are published with.
func (b *CDRBackend) RoutingKey() string {
	if b == nil {
		return ""
	}
	return b.routingKey
}

// Module returns the underlying RabbitMQModule, primarily so RPC
// handlers and tests can inspect published messages.
func (b *CDRBackend) Module() *RabbitMQModule {
	if b == nil {
		return nil
	}
	return b.module
}
