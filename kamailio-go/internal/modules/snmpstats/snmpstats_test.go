// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SNMPStats module tests.
 */
package snmpstats

import (
	"sync"
	"testing"
)

func TestRegisterAndIncCounter(t *testing.T) {
	m := NewSNMPStatsModule()
	m.RegisterCounter("calls")
	if got := m.GetCounter("calls"); got != 0 {
		t.Errorf("GetCounter(calls) = %d, want 0", got)
	}
	m.IncCounter("calls")
	m.IncCounter("calls")
	m.IncCounter("calls")
	if got := m.GetCounter("calls"); got != 3 {
		t.Errorf("GetCounter(calls) = %d, want 3", got)
	}
	// IncCounter on an unregistered counter auto-registers it.
	m.IncCounter("errors")
	if got := m.GetCounter("errors"); got != 1 {
		t.Errorf("GetCounter(errors) = %d, want 1", got)
	}
	// Unknown counter returns 0.
	if got := m.GetCounter("nope"); got != 0 {
		t.Errorf("GetCounter(nope) = %d, want 0", got)
	}
	// Re-registering is a no-op (value preserved).
	m.IncCounter("calls")
	m.RegisterCounter("calls")
	if got := m.GetCounter("calls"); got != 4 {
		t.Errorf("GetCounter(calls) after re-register = %d, want 4", got)
	}
}

func TestStartStop(t *testing.T) {
	m := NewSNMPStatsModule()
	// Start without an address fails.
	if err := m.Start(); err == nil {
		t.Error("expected error starting without address")
	}
	if m.IsRunning() {
		t.Error("expected not running after failed start")
	}
	m.Init("127.0.0.1:161")
	if m.Addr() != "127.0.0.1:161" {
		t.Errorf("Addr = %q, want 127.0.0.1:161", m.Addr())
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !m.IsRunning() {
		t.Error("expected running after Start")
	}
	// Starting twice fails.
	if err := m.Start(); err == nil {
		t.Error("expected error starting twice")
	}
	m.Stop()
	if m.IsRunning() {
		t.Error("expected not running after Stop")
	}
}

func TestConcurrentCounters(t *testing.T) {
	m := NewSNMPStatsModule()
	m.RegisterCounter("reqs")
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 50
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.IncCounter("reqs")
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if got := m.GetCounter("reqs"); got != want {
		t.Errorf("GetCounter(reqs) = %d, want %d", got, want)
	}
}
