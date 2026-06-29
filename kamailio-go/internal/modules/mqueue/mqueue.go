// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * mqueue - in-memory named message queues (FIFO).
 *
 * Provides simple push/pop string queues addressable by name, mirroring
 * the kamailio mqueue module.
 */

package mqueue

import "sync"

// MQueueModule holds a set of named FIFO queues.
type MQueueModule struct {
	mu     sync.Mutex
	queues map[string][]string
}

// New returns a new MQueueModule.
func New() *MQueueModule {
	return &MQueueModule{queues: make(map[string][]string)}
}

// Push appends msg to the named queue, creating it if necessary, and
// returns the new queue size.
func (m *MQueueModule) Push(queue string, msg string) int {
	if m == nil || queue == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues[queue] = append(m.queues[queue], msg)
	return len(m.queues[queue])
}

// Pop removes and returns the oldest message from the named queue.
// Returns ("", false) if the queue is empty or unknown.
func (m *MQueueModule) Pop(queue string) (string, bool) {
	if m == nil || queue == "" {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[queue]
	if len(q) == 0 {
		return "", false
	}
	msg := q[0]
	m.queues[queue] = q[1:]
	if len(m.queues[queue]) == 0 {
		delete(m.queues, queue)
	}
	return msg, true
}

// Size returns the number of messages currently in the named queue.
func (m *MQueueModule) Size(queue string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queues[queue])
}

// List returns the names of all non-empty queues.
func (m *MQueueModule) List() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.queues))
	for name := range m.queues {
		out = append(out, name)
	}
	return out
}

// Clear removes all messages from the named queue.
func (m *MQueueModule) Clear(queue string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.queues, queue)
}
