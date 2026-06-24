// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - CallObj module tests.
 */

package call_obj

import (
	"sync"
	"testing"
)

func TestCreateGetDeleteCount(t *testing.T) {
	m := New()
	obj := m.Create("call-1")
	if obj == nil {
		t.Fatal("expected non-nil object")
	}
	if obj.CallID != "call-1" || obj.State != "init" {
		t.Fatalf("unexpected object: %+v", obj)
	}
	obj.FromURI = "sip:alice@example.com"
	obj.ToURI = "sip:bob@example.com"
	obj.State = "confirmed"
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1", m.Count())
	}
	got := m.Get("call-1")
	if got == nil || got.FromURI != "sip:alice@example.com" {
		t.Fatalf("Get = %+v", got)
	}
	if !m.Delete("call-1") {
		t.Fatal("expected Delete true")
	}
	if m.Get("call-1") != nil {
		t.Fatal("expected Get nil after Delete")
	}
	if m.Count() != 0 {
		t.Fatalf("Count = %d, want 0", m.Count())
	}
}

func TestCreateReplaces(t *testing.T) {
	m := New()
	m.Create("call-1")
	m.Create("call-1")
	if m.Count() != 1 {
		t.Fatalf("Count = %d, want 1 (replaced)", m.Count())
	}
}

func TestGetDeleteUnknown(t *testing.T) {
	m := New()
	if m.Get("missing") != nil {
		t.Fatal("expected Get nil for missing")
	}
	if m.Delete("missing") {
		t.Fatal("expected Delete false for missing")
	}
}

func TestCallObjectFields(t *testing.T) {
	m := New()
	obj := m.Create("call-2")
	obj.FromURI = "sip:from@x"
	obj.ToURI = "sip:to@x"
	obj.State = "ringing"
	got := m.Get("call-2")
	if got.FromURI != "sip:from@x" || got.ToURI != "sip:to@x" || got.State != "ringing" {
		t.Fatalf("unexpected fields: %+v", got)
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	obj := Create("gcall")
	if obj == nil {
		t.Fatal("expected non-nil global object")
	}
	if Get("gcall") == nil {
		t.Fatal("expected global Get non-nil")
	}
	if Count() < 1 {
		t.Fatalf("global Count = %d, want >=1", Count())
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Create("call")
			_ = m.Get("call")
			_ = m.Count()
		}()
	}
	wg.Wait()
}
