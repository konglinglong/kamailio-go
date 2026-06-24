// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * mohqueue - music-on-hold call queues.
 *
 * Holds named MOH queues with a maximum caller capacity; calls are
 * enqueued and dequeued in FIFO order. Mirrors the kamailio mohqueue
 * module.
 */

package mohqueue

import "sync"

// MOHQueue describes a music-on-hold queue.
type MOHQueue struct {
	Name       string
	URI        string
	MaxCallers int
}

// MOHQueueModule manages a registry of MOH queues.
type MOHQueueModule struct {
	mu      sync.Mutex
	queues  map[string]*MOHQueue
	callers map[string][]string
}

// New returns a new MOHQueueModule.
func New() *MOHQueueModule {
	return &MOHQueueModule{
		queues:  make(map[string]*MOHQueue),
		callers: make(map[string][]string),
	}
}

// AddQueue registers a MOH queue, overwriting any previous queue with the
// same name.
func (m *MOHQueueModule) AddQueue(q *MOHQueue) {
	if m == nil || q == nil || q.Name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues[q.Name] = q
}

// RemoveQueue removes a queue and any callers waiting in it.
func (m *MOHQueueModule) RemoveQueue(name string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.queues, name)
	delete(m.callers, name)
}

// Enqueue adds callID to the named queue if it exists and has capacity.
// Returns true on success.
func (m *MOHQueueModule) Enqueue(queue, callID string) bool {
	if m == nil || queue == "" || callID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	q, ok := m.queues[queue]
	if !ok {
		return false
	}
	if q.MaxCallers > 0 && len(m.callers[queue]) >= q.MaxCallers {
		return false
	}
	m.callers[queue] = append(m.callers[queue], callID)
	return true
}

// Dequeue removes and returns the oldest callID from the named queue.
// Returns "" if the queue is empty or unknown.
func (m *MOHQueueModule) Dequeue(queue string) string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.callers[queue]
	if len(c) == 0 {
		return ""
	}
	callID := c[0]
	m.callers[queue] = c[1:]
	if len(m.callers[queue]) == 0 {
		delete(m.callers, queue)
	}
	return callID
}

// QueueSize returns the number of callers currently waiting in the named
// queue.
func (m *MOHQueueModule) QueueSize(queue string) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.callers[queue])
}
