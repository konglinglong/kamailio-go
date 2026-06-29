// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Async task framework - matching C async_task.c
 * Provides a worker pool for asynchronous script execution
 */

package asynctask

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// TaskStatus represents the current state of a task.
type TaskStatus int

const (
	StatusPending TaskStatus = iota
	StatusRunning
	StatusCompleted
	StatusFailed
)

// TaskFunc is the function executed by a task.
type TaskFunc func(param interface{}) (interface{}, error)

// Task represents an asynchronous task.
type Task struct {
	ID          int64
	Name        string
	Func        TaskFunc
	Param       interface{}
	Result      interface{}
	Err         error
	status      atomic.Int32
	CreatedAt   time.Time
	CompletedAt time.Time
	done        chan struct{}
}

// Status returns the current task status.
func (t *Task) Status() TaskStatus {
	return TaskStatus(t.status.Load())
}

// IsDone returns true if the task has completed (success or failure).
func (t *Task) IsDone() bool {
	s := t.Status()
	return s == StatusCompleted || s == StatusFailed
}

// TaskManager manages a pool of async task workers.
type TaskManager struct {
	mu         sync.RWMutex
	tasks      map[int64]*Task
	nextID     int64
	taskCh     chan *Task
	maxWorkers int
	running    atomic.Bool
	wg         sync.WaitGroup
}

// NewTaskManager creates a new TaskManager with default worker count.
func NewTaskManager() *TaskManager {
	return &TaskManager{
		tasks:      make(map[int64]*Task),
		maxWorkers: runtime.NumCPU(),
	}
}

// SetMaxWorkers sets the worker pool size. Must be called before Start.
func (tm *TaskManager) SetMaxWorkers(n int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if n < 1 {
		n = 1
	}
	tm.maxWorkers = n
}

// Start launches the worker pool.
func (tm *TaskManager) Start() {
	if !tm.running.CompareAndSwap(false, true) {
		return
	}
	tm.taskCh = make(chan *Task, 256)
	for i := 0; i < tm.maxWorkers; i++ {
		tm.wg.Add(1)
		go tm.worker()
	}
}

func (tm *TaskManager) worker() {
	defer tm.wg.Done()
	for t := range tm.taskCh {
		t.status.Store(int32(StatusRunning))
		result, err := t.Func(t.Param)
		t.Result = result
		t.Err = err
		if err != nil {
			t.status.Store(int32(StatusFailed))
		} else {
			t.status.Store(int32(StatusCompleted))
		}
		t.CompletedAt = time.Now()
		close(t.done)
	}
}

// Submit queues a task for asynchronous execution.
func (tm *TaskManager) Submit(name string, fn TaskFunc, param interface{}) *Task {
	if !tm.running.Load() {
		tm.Start()
	}

	id := atomic.AddInt64(&tm.nextID, 1)
	t := &Task{
		ID:        id,
		Name:      name,
		Func:      fn,
		Param:     param,
		CreatedAt: time.Now(),
		done:      make(chan struct{}),
	}
	t.status.Store(int32(StatusPending))

	tm.mu.Lock()
	tm.tasks[id] = t
	tm.mu.Unlock()

	tm.taskCh <- t
	return t
}

// Wait blocks until the task completes and returns its result.
func (tm *TaskManager) Wait(t *Task) (interface{}, error) {
	if t == nil {
		return nil, errors.New("nil task")
	}
	<-t.done
	return t.Result, t.Err
}

// WaitWithTimeout blocks until the task completes or timeout expires.
func (tm *TaskManager) WaitWithTimeout(t *Task, timeout time.Duration) (interface{}, error) {
	if t == nil {
		return nil, errors.New("nil task")
	}
	select {
	case <-t.done:
		return t.Result, t.Err
	case <-time.After(timeout):
		return nil, errors.New("timeout waiting for task")
	}
}

// GetTask retrieves a task by ID.
func (tm *TaskManager) GetTask(id int64) *Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.tasks[id]
}

// ListTasks returns all known tasks.
func (tm *TaskManager) ListTasks() []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	result := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		result = append(result, t)
	}
	return result
}

// Count returns the total number of tasks.
func (tm *TaskManager) Count() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.tasks)
}

// ActiveCount returns the number of currently running tasks.
func (tm *TaskManager) ActiveCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	count := 0
	for _, t := range tm.tasks {
		if t.Status() == StatusRunning || t.Status() == StatusPending {
			count++
		}
	}
	return count
}

// Stop gracefully stops the worker pool, waiting for pending tasks.
func (tm *TaskManager) Stop() {
	if !tm.running.CompareAndSwap(true, false) {
		return
	}
	close(tm.taskCh)
	tm.wg.Wait()
}

// StopImmediately stops the worker pool without waiting.
func (tm *TaskManager) StopImmediately() {
	if !tm.running.CompareAndSwap(true, false) {
		return
	}
	close(tm.taskCh)
	tm.wg.Wait()
}

// --- Global default manager ---

var (
	defaultOnce sync.Once
	defaultTM   *TaskManager
)

// DefaultTaskManager returns the global default TaskManager.
func DefaultTaskManager() *TaskManager {
	defaultOnce.Do(func() {
		defaultTM = NewTaskManager()
		defaultTM.Start()
	})
	return defaultTM
}

// Init re-initializes the default TaskManager.
func Init() {
	defaultTM = NewTaskManager()
	defaultTM.Start()
	defaultOnce = sync.Once{}
}

// Submit submits a task to the default manager.
func Submit(name string, fn TaskFunc, param interface{}) *Task {
	return DefaultTaskManager().Submit(name, fn, param)
}

// Wait waits for a task on the default manager.
func Wait(t *Task) (interface{}, error) {
	return DefaultTaskManager().Wait(t)
}

// Stop stops the default manager.
func Stop() {
	DefaultTaskManager().Stop()
}
