// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for asynctask package
 */

package asynctask

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSubmit(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	task := tm.Submit("test", func(param interface{}) (interface{}, error) {
		return param, nil
	}, "hello")

	result, err := tm.Wait(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}
	if task.Status() != StatusCompleted {
		t.Errorf("expected StatusCompleted, got %d", task.Status())
	}
}

func TestWait(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	task := tm.Submit("compute", func(param interface{}) (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		return 42, nil
	}, nil)

	result, err := tm.Wait(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestWaitWithTimeout(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	task := tm.Submit("slow", func(param interface{}) (interface{}, error) {
		time.Sleep(200 * time.Millisecond)
		return "done", nil
	}, nil)

	// Short timeout should expire
	_, err := tm.WaitWithTimeout(task, 50*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}

	// Wait for completion
	result, err := tm.WaitWithTimeout(task, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("expected 'done', got %v", result)
	}
}

func TestGetTask(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	task := tm.Submit("lookup", func(param interface{}) (interface{}, error) {
		return "ok", nil
	}, nil)
	tm.Wait(task)

	got := tm.GetTask(task.ID)
	if got == nil {
		t.Fatal("GetTask returned nil")
	}
	if got.ID != task.ID {
		t.Errorf("expected ID %d, got %d", task.ID, got.ID)
	}
}

func TestListTasks(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	for i := 0; i < 5; i++ {
		t := tm.Submit("task", func(param interface{}) (interface{}, error) {
			return nil, nil
		}, nil)
		tm.Wait(t)
	}

	tasks := tm.ListTasks()
	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}
}

func TestCount(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	for i := 0; i < 3; i++ {
		t := tm.Submit("count-test", func(param interface{}) (interface{}, error) {
			return nil, nil
		}, nil)
		tm.Wait(t)
	}

	if tm.Count() != 3 {
		t.Errorf("expected count 3, got %d", tm.Count())
	}
}

func TestActiveCount(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	// Submit a slow task
	task := tm.Submit("slow", func(param interface{}) (interface{}, error) {
		time.Sleep(100 * time.Millisecond)
		return nil, nil
	}, nil)

	// Should be active while running
	time.Sleep(20 * time.Millisecond)
	if tm.ActiveCount() < 1 {
		t.Error("expected at least 1 active task")
	}

	tm.Wait(task)
	if tm.ActiveCount() != 0 {
		t.Errorf("expected 0 active tasks, got %d", tm.ActiveCount())
	}
}

func TestSetMaxWorkers(t *testing.T) {
	tm := NewTaskManager()
	tm.SetMaxWorkers(4)
	tm.Start()
	defer tm.Stop()

	for i := 0; i < 10; i++ {
		t := tm.Submit("worker-test", func(param interface{}) (interface{}, error) {
			return nil, nil
		}, nil)
		tm.Wait(t)
	}

	if tm.Count() != 10 {
		t.Errorf("expected 10 tasks, got %d", tm.Count())
	}
}

func TestStop(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()

	for i := 0; i < 5; i++ {
		t := tm.Submit("stop-test", func(param interface{}) (interface{}, error) {
			return nil, nil
		}, nil)
		tm.Wait(t)
	}

	tm.Stop()
	// Should not panic
}

func TestTaskError(t *testing.T) {
	tm := NewTaskManager()
	tm.Start()
	defer tm.Stop()

	task := tm.Submit("error-test", func(param interface{}) (interface{}, error) {
		return nil, errors.New("task failed")
	}, nil)

	_, err := tm.Wait(task)
	if err == nil {
		t.Error("expected error")
	}
	if task.Status() != StatusFailed {
		t.Errorf("expected StatusFailed, got %d", task.Status())
	}
}

func TestConcurrentSubmit(t *testing.T) {
	tm := NewTaskManager()
	tm.SetMaxWorkers(8)
	tm.Start()
	defer tm.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			task := tm.Submit("concurrent", func(param interface{}) (interface{}, error) {
				return n, nil
			}, n)
			tm.Wait(task)
		}(i)
	}
	wg.Wait()

	if tm.Count() != 100 {
		t.Errorf("expected 100 tasks, got %d", tm.Count())
	}
}

func TestGlobalFunctions(t *testing.T) {
	task := Submit("global", func(param interface{}) (interface{}, error) {
		return "global-result", nil
	}, nil)

	result, err := Wait(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "global-result" {
		t.Errorf("expected 'global-result', got %v", result)
	}
}
