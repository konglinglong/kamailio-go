// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipCapture module tests - capturing raw SIP packets.
 */
package sipcapture

import (
	"bytes"
	"sync"
	"testing"
)

func TestCapture(t *testing.T) {
	m := NewSipCaptureModule()
	payload := []byte("INVITE sip:bob@example.com SIP/2.0\r\n\r\n")

	entry := m.Capture("UDP", "10.0.0.1", 5060, "10.0.0.2", 5060, payload)
	if entry == nil {
		t.Fatal("expected non-nil capture entry")
	}
	if entry.Proto != "UDP" {
		t.Errorf("Proto = %q, want UDP", entry.Proto)
	}
	if entry.SrcIP != "10.0.0.1" || entry.SrcPort != 5060 {
		t.Errorf("Src = %s:%d, want 10.0.0.1:5060", entry.SrcIP, entry.SrcPort)
	}
	if entry.DstIP != "10.0.0.2" || entry.DstPort != 5060 {
		t.Errorf("Dst = %s:%d, want 10.0.0.2:5060", entry.DstIP, entry.DstPort)
	}
	if !bytes.Equal(entry.Payload, payload) {
		t.Errorf("Payload mismatch")
	}
	if entry.ID <= 0 {
		t.Errorf("ID = %d, want > 0", entry.ID)
	}
	if entry.Time.IsZero() {
		t.Error("expected non-zero Time")
	}
}

func TestCaptureCopiesPayload(t *testing.T) {
	m := NewSipCaptureModule()
	payload := []byte("DATA")
	entry := m.Capture("TCP", "1.1.1.1", 1, "2.2.2.2", 2, payload)
	// Mutate the caller's slice; the stored entry must be unaffected.
	payload[0] = 'X'
	if entry.Payload[0] != 'D' {
		t.Errorf("stored payload was mutated: %q", string(entry.Payload))
	}
}

func TestGetEntryAndCorrelationID(t *testing.T) {
	m := NewSipCaptureModule()
	e1 := m.Capture("UDP", "a", 1, "b", 2, []byte("p1"))
	e2 := m.Capture("UDP", "c", 3, "d", 4, []byte("p2"))

	if got := m.GetEntry(e1.ID); got != e1 {
		t.Errorf("GetEntry(e1.ID) returned %p, want %p", got, e1)
	}
	if got := m.GetEntry(e2.ID); got != e2 {
		t.Errorf("GetEntry(e2.ID) returned %p, want %p", got, e2)
	}
	if got := m.GetEntry(99999); got != nil {
		t.Errorf("GetEntry(unknown) returned %v, want nil", got)
	}

	if !m.SetCorrelationID(e1.ID, "corr-1") {
		t.Error("SetCorrelationID returned false for existing entry")
	}
	if m.SetCorrelationID(99999, "corr-x") {
		t.Error("SetCorrelationID returned true for unknown entry")
	}
	if !m.SetCorrelationID(e2.ID, "corr-1") {
		t.Error("SetCorrelationID returned false for existing entry")
	}

	entries := m.GetByCorrelationID("corr-1")
	if len(entries) != 2 {
		t.Fatalf("len(GetByCorrelationID) = %d, want 2", len(entries))
	}
	if entries[0] != e1 || entries[1] != e2 {
		t.Error("correlation entries not in insertion order")
	}
	if got := m.GetByCorrelationID("none"); len(got) != 0 {
		t.Errorf("GetByCorrelationID(none) = %d, want 0", len(got))
	}
}

func TestCountListClear(t *testing.T) {
	m := NewSipCaptureModule()
	if m.Count() != 0 {
		t.Errorf("Count = %d, want 0", m.Count())
	}
	m.Capture("UDP", "a", 1, "b", 2, []byte("p1"))
	m.Capture("UDP", "c", 3, "d", 4, []byte("p2"))
	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
	if got := m.List(); len(got) != 2 {
		t.Errorf("len(List) = %d, want 2", len(got))
	}
	m.Clear()
	if m.Count() != 0 {
		t.Errorf("Count after Clear = %d, want 0", m.Count())
	}
	if got := m.List(); len(got) != 0 {
		t.Errorf("len(List) after Clear = %d, want 0", len(got))
	}
}

func TestSetEnabled(t *testing.T) {
	m := NewSipCaptureModule()
	if !m.IsEnabled() {
		t.Error("expected enabled by default")
	}
	m.SetEnabled(false)
	if m.IsEnabled() {
		t.Error("expected disabled after SetEnabled(false)")
	}
	if entry := m.Capture("UDP", "a", 1, "b", 2, []byte("p")); entry != nil {
		t.Errorf("Capture when disabled returned %v, want nil", entry)
	}
	if m.Count() != 0 {
		t.Errorf("Count when disabled = %d, want 0", m.Count())
	}
	m.SetEnabled(true)
	if entry := m.Capture("UDP", "a", 1, "b", 2, []byte("p")); entry == nil {
		t.Error("Capture when re-enabled returned nil")
	}
}

func TestFormat(t *testing.T) {
	m := NewSipCaptureModule()
	entry := m.Capture("UDP", "10.0.0.1", 5060, "10.0.0.2", 5060, []byte("hello"))
	s := entry.Format()
	if s != "UDP 10.0.0.1:5060 -> 10.0.0.2:5060 (5 bytes)" {
		t.Errorf("Format = %q", s)
	}
	var nilEntry *CaptureEntry
	if got := nilEntry.Format(); got != "<nil>" {
		t.Errorf("nil Format = %q, want <nil>", got)
	}
}

func TestConcurrentCapture(t *testing.T) {
	m := NewSipCaptureModule()
	const goroutines = 50
	const perG = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Capture("UDP", "a", 1, "b", 2, []byte("p"))
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if got := int64(m.Count()); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
}

func TestDefaultSipCaptureAndInit(t *testing.T) {
	Init()
	d1 := DefaultSipCapture()
	d2 := DefaultSipCapture()
	if d1 != d2 {
		t.Error("DefaultSipCapture returned different instances")
	}
	d1.Clear()
	d1.Capture("UDP", "a", 1, "b", 2, []byte("p"))
	if d2.Count() != 1 {
		t.Errorf("Count after capture via default = %d, want 1", d2.Count())
	}
	Init()
	if DefaultSipCapture().Count() != 0 {
		t.Error("expected reset after Init()")
	}
}
