// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipP module tests.
 */
package sipp

import (
	"sync"
	"testing"
)

func TestStartStopAndIsRunning(t *testing.T) {
	m := NewSippModule()
	if m.IsRunning() {
		t.Fatal("expected not running initially")
	}
	if err := m.StartScenario("uac"); err != nil {
		t.Fatalf("StartScenario failed: %v", err)
	}
	if !m.IsRunning() {
		t.Error("expected running after StartScenario")
	}
	// Starting the same scenario twice fails.
	if err := m.StartScenario("uac"); err == nil {
		t.Error("expected error starting already-running scenario")
	}
	m.StopScenario("uac")
	if m.IsRunning() {
		t.Error("expected not running after StopScenario")
	}
	// Stopping an unknown scenario is a no-op.
	m.StopScenario("ghost")
}

func TestStartScenarioValidation(t *testing.T) {
	m := NewSippModule()
	if err := m.StartScenario(""); err == nil {
		t.Error("expected error for empty scenario name")
	}
}

func TestGetStatsAndRecordCall(t *testing.T) {
	m := NewSippModule()
	if s := m.GetStats("uas"); s != nil {
		t.Errorf("GetStats for unknown scenario = %v, want nil", s)
	}
	if err := m.StartScenario("uas"); err != nil {
		t.Fatalf("StartScenario failed: %v", err)
	}
	s := m.GetStats("uas")
	if s == nil {
		t.Fatal("expected non-nil stats")
	}
	if !s.Running {
		t.Error("expected Running=true")
	}
	m.RecordCall("uas", true)
	m.RecordCall("uas", true)
	m.RecordCall("uas", false)
	s = m.GetStats("uas")
	if s.Calls != 3 {
		t.Errorf("Calls = %d, want 3", s.Calls)
	}
	if s.SuccessfulCalls != 2 {
		t.Errorf("SuccessfulCalls = %d, want 2", s.SuccessfulCalls)
	}
	if s.FailedCalls != 1 {
		t.Errorf("FailedCalls = %d, want 1", s.FailedCalls)
	}
	// RecordCall on unknown scenario is a no-op.
	m.RecordCall("ghost", true)
	if s.Calls != 3 {
		t.Errorf("Calls after ghost record = %d, want 3", s.Calls)
	}
}

func TestConcurrentSipp(t *testing.T) {
	m := NewSippModule()
	var wg sync.WaitGroup
	const goroutines = 20
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(g int) {
			defer wg.Done()
			name := "scn-" + itoa(g)
			_ = m.StartScenario(name)
			m.RecordCall(name, true)
			m.StopScenario(name)
		}(i)
	}
	wg.Wait()
	if m.IsRunning() {
		t.Error("expected not running after all stopped")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
