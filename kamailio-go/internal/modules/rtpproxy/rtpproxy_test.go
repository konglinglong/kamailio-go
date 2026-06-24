// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - rtpproxy module tests.
 */

package rtpproxy

import (
	"strings"
	"sync"
	"testing"
)

func TestOfferAnswerDelete(t *testing.T) {
	m := New()
	if !m.Ping() {
		t.Fatal("new module should be alive")
	}
	off, err := m.Offer("call-1")
	if err != nil {
		t.Fatalf("Offer error: %v", err)
	}
	if !strings.HasPrefix(off, "rtpproxy:call-1:") {
		t.Errorf("Offer returned %q", off)
	}
	if m.SessionCount() != 1 {
		t.Errorf("SessionCount = %d, want 1", m.SessionCount())
	}
	ans, err := m.Answer("call-1")
	if err != nil {
		t.Fatalf("Answer error: %v", err)
	}
	if !strings.HasPrefix(ans, "rtpproxy:call-1:") {
		t.Errorf("Answer returned %q", ans)
	}
	// Answer refreshes, not duplicates.
	if m.SessionCount() != 1 {
		t.Errorf("SessionCount after Answer = %d, want 1", m.SessionCount())
	}
	if err := m.Delete("call-1"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if m.SessionCount() != 0 {
		t.Errorf("SessionCount after Delete = %d, want 0", m.SessionCount())
	}
}

func TestErrors(t *testing.T) {
	m := New()
	if _, err := m.Offer(""); err == nil {
		t.Errorf("Offer with empty callID should error")
	}
	if _, err := m.Answer(""); err == nil {
		t.Errorf("Answer with empty callID should error")
	}
	if err := m.Delete(""); err == nil {
		t.Errorf("Delete with empty callID should error")
	}
	m.SetAlive(false)
	if m.Ping() {
		t.Errorf("Ping should be false after SetAlive(false)")
	}
	if _, err := m.Offer("call"); err == nil {
		t.Errorf("Offer should error when not alive")
	}
}

func TestMultipleSessions(t *testing.T) {
	m := New()
	for i := 0; i < 5; i++ {
		if _, err := m.Offer("call"); err != nil {
			t.Fatal(err)
		}
	}
	if m.SessionCount() != 1 {
		t.Errorf("repeated Offer should keep one session, got %d", m.SessionCount())
	}
	m.Offer("a")
	m.Offer("b")
	if m.SessionCount() != 3 {
		t.Errorf("SessionCount = %d, want 3", m.SessionCount())
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	a := DefaultRTPProxy()
	b := DefaultRTPProxy()
	if a != b {
		t.Fatal("DefaultRTPProxy should return the same instance")
	}
	a.Offer("call")
	Init()
	c := DefaultRTPProxy()
	if c == a {
		t.Fatal("Init should reset the default instance")
	}
	if c.SessionCount() != 0 {
		t.Errorf("reset default should have no sessions")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	calls := []string{"c1", "c2", "c3", "c4", "c5"}
	for _, c := range calls {
		wg.Add(1)
		c := c
		go func() {
			defer wg.Done()
			_, _ = m.Offer(c)
			_, _ = m.Answer(c)
			_ = m.SessionCount()
			_ = m.Ping()
			_ = m.Delete(c)
		}()
	}
	wg.Wait()
}
