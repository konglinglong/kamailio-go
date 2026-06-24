// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SystemdOps module tests.
 */
package systemdops

import (
	"strings"
	"sync"
	"testing"
)

func TestNotifyAndStatus(t *testing.T) {
	m := NewSystemdOpsModule()
	if m.IsReady() {
		t.Fatal("expected not ready initially")
	}
	if got := m.GetStatus(); got != "" {
		t.Errorf("GetStatus = %q, want empty", got)
	}
	m.Notify("starting up")
	if !m.IsReady() {
		t.Error("expected ready after Notify")
	}
	if got := m.GetStatus(); got != "starting up" {
		t.Errorf("GetStatus = %q, want 'starting up'", got)
	}
	if m.NotifyCount() != 1 {
		t.Errorf("NotifyCount = %d, want 1", m.NotifyCount())
	}
	m.SetStatus("running")
	if got := m.GetStatus(); got != "running" {
		t.Errorf("GetStatus after SetStatus = %q, want 'running'", got)
	}
	// SetStatus does not bump the notify counter.
	if m.NotifyCount() != 1 {
		t.Errorf("NotifyCount after SetStatus = %d, want 1", m.NotifyCount())
	}
}

func TestWatchdog(t *testing.T) {
	m := NewSystemdOpsModule()
	if m.WatchdogCount() != 0 {
		t.Fatalf("WatchdogCount = %d, want 0", m.WatchdogCount())
	}
	m.Watchdog()
	m.Watchdog()
	m.Watchdog()
	if got := m.WatchdogCount(); got != 3 {
		t.Errorf("WatchdogCount = %d, want 3", got)
	}
}

func TestNotifyMessage(t *testing.T) {
	msg := NotifyMessage("idle")
	if !strings.Contains(msg, "READY=1") {
		t.Errorf("message missing READY=1: %q", msg)
	}
	if !strings.Contains(msg, "STATUS=idle") {
		t.Errorf("message missing STATUS=idle: %q", msg)
	}
}

func TestConcurrentSystemdOps(t *testing.T) {
	m := NewSystemdOpsModule()
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 50
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Notify("s")
				m.Watchdog()
				_ = m.GetStatus()
			}
		}(i)
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if got := m.NotifyCount(); got != want {
		t.Errorf("NotifyCount = %d, want %d", got, want)
	}
	if got := m.WatchdogCount(); got != want {
		t.Errorf("WatchdogCount = %d, want %d", got, want)
	}
}
