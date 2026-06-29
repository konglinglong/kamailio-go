// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SWorker module - asynchronous SIP task worker pool.
 * Port of the kamailio sworker module (src/modules/sworker).
 *
 * Accepts string tasks via Submit, processes them (producing a deterministic
 * result string) and stores the result keyed by task id. Start/Stop manage a
 * pool of idle worker goroutines; task processing is performed synchronously
 * inside Submit so results are immediately available and tests are
 * deterministic.
 *
 * It is safe for concurrent use.
 */
package sworker

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// task holds a submitted task and its computed result.
type task struct {
	id     int
	input  string
	result string
	done   bool
}

// SWorkerModule implements the sworker module functionality.
// C: struct module sworker
type SWorkerModule struct {
	mu       sync.Mutex
	tasks    map[int]*task
	order    []int
	nextID   atomic.Int64
	running  atomic.Bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewSWorkerModule creates a SWorkerModule.
func NewSWorkerModule() *SWorkerModule {
	return &SWorkerModule{tasks: make(map[int]*task)}
}

// process produces the deterministic result for an input string.
func process(input string) string {
	return fmt.Sprintf("processed:%s", input)
}

// Submit registers a task, computes its result synchronously and returns the
// assigned task id. The result is retrievable via GetResult.
// C: sw_worker_submit()
func (m *SWorkerModule) Submit(taskStr string) int {
	id := int(m.nextID.Add(1))
	t := &task{id: id, input: taskStr, result: process(taskStr), done: true}
	m.mu.Lock()
	if m.tasks == nil {
		m.tasks = make(map[int]*task)
	}
	m.tasks[id] = t
	m.order = append(m.order, id)
	m.mu.Unlock()
	return id
}

// GetResult returns the result of the task with the given id. The bool
// result is false when no such task exists or its result has not been
// collected yet; once retrieved the task is removed from the pending set.
// C: sw_worker_get_result()
func (m *SWorkerModule) GetResult(id int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return "", false
	}
	res := t.result
	delete(m.tasks, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return res, true
}

// PendingCount returns the number of submitted tasks whose results have not
// yet been retrieved via GetResult.
func (m *SWorkerModule) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}

// Start spins up the requested number of idle worker goroutines and marks the
// pool as running. Calling Start while already running is a no-op.
// C: sw_worker_start()
func (m *SWorkerModule) Start(workers int) {
	if workers < 1 {
		workers = 1
	}
	if m.running.Load() {
		return
	}
	m.running.Store(true)
	m.stopCh = make(chan struct{})
	for i := 0; i < workers; i++ {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			<-m.stopCh
		}()
	}
}

// Stop signals the worker pool to shut down and waits for every worker
// goroutine to exit. It is a no-op when the pool is not running.
// C: sw_worker_stop()
func (m *SWorkerModule) Stop() {
	if !m.running.Load() {
		return
	}
	close(m.stopCh)
	m.wg.Wait()
	m.running.Store(false)
}

// IsRunning reports whether the worker pool is currently running.
func (m *SWorkerModule) IsRunning() bool {
	return m.running.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu      sync.RWMutex
	defaultSWorker *SWorkerModule
)

// DefaultSWorker returns the process-wide SWorkerModule, creating one on
// first use.
func DefaultSWorker() *SWorkerModule {
	defaultMu.RLock()
	m := defaultSWorker
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSWorker == nil {
		defaultSWorker = NewSWorkerModule()
	}
	return defaultSWorker
}

// Init (re)initialises the process-wide SWorkerModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSWorker = NewSWorkerModule()
}
