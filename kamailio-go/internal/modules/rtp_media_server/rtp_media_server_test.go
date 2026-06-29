// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - RTPMediaServer module tests.
 */

package rtp_media_server

import (
	"sync"
	"testing"
)

func TestCreateDestroyCount(t *testing.T) {
	m := New()
	id := m.CreateSession("call-1")
	if id == "" {
		t.Fatal("expected non-empty session id")
	}
	if m.SessionCount() != 1 {
		t.Fatalf("SessionCount = %d, want 1", m.SessionCount())
	}
	m.DestroySession(id)
	if m.SessionCount() != 0 {
		t.Fatalf("SessionCount = %d, want 0", m.SessionCount())
	}
}

func TestPlayRecordMedia(t *testing.T) {
	m := New()
	id := m.CreateSession("call-1")
	if err := m.PlayMedia(id, "prompt.wav"); err != nil {
		t.Fatalf("PlayMedia: %v", err)
	}
	s := m.GetSession(id)
	if s == nil || !s.Playing || s.File != "prompt.wav" {
		t.Fatalf("unexpected session: %+v", s)
	}
	if err := m.RecordMedia(id, "rec.wav"); err != nil {
		t.Fatalf("RecordMedia: %v", err)
	}
	s = m.GetSession(id)
	if s == nil || !s.Recording || s.Playing || s.File != "rec.wav" {
		t.Fatalf("unexpected session after record: %+v", s)
	}
}

func TestPlayRecordUnknownSession(t *testing.T) {
	m := New()
	if err := m.PlayMedia("nope", "f.wav"); err == nil {
		t.Fatal("expected error for unknown session")
	}
	if err := m.RecordMedia("nope", "f.wav"); err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestDestroyUnknown(t *testing.T) {
	m := New()
	m.DestroySession("nope") // should not panic
	if m.SessionCount() != 0 {
		t.Fatal("expected 0 sessions")
	}
}

func TestMultipleSessions(t *testing.T) {
	m := New()
	id1 := m.CreateSession("call-1")
	id2 := m.CreateSession("call-2")
	if id1 == id2 {
		t.Fatal("expected distinct session ids")
	}
	if m.SessionCount() != 2 {
		t.Fatalf("SessionCount = %d, want 2", m.SessionCount())
	}
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	m := DefaultRTPMediaServer()
	id := m.CreateSession("gcall")
	if m.SessionCount() != 1 {
		t.Fatalf("SessionCount = %d, want 1", m.SessionCount())
	}
	m.DestroySession(id)
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := m.CreateSession("c")
			_ = m.PlayMedia(id, "f")
			_ = m.SessionCount()
		}()
	}
	wg.Wait()
	if m.SessionCount() != 20 {
		t.Fatalf("SessionCount = %d, want 20", m.SessionCount())
	}
}
