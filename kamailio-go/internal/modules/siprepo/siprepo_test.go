// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipRepo module tests.
 */
package siprepo

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var inviteBytes = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: cid-repo@10.0.0.1\r\n" +
	"CSeq: 1 INVITE\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func TestStoreRetrieveDelete(t *testing.T) {
	m := NewSipRepoModule()
	msg := mustParseMsg(t, inviteBytes)

	id := m.Store(msg)
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	if m.Count() != 1 {
		t.Errorf("Count = %d, want 1", m.Count())
	}
	got, err := m.Retrieve(id)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}
	if got != msg {
		t.Error("Retrieve returned a different message pointer")
	}
	if _, err := m.Retrieve("nope"); err == nil {
		t.Error("expected error for unknown id")
	}
	if !m.Delete(id) {
		t.Error("Delete returned false for existing id")
	}
	if m.Count() != 0 {
		t.Errorf("Count after Delete = %d, want 0", m.Count())
	}
	if m.Delete(id) {
		t.Error("Delete returned true for already-removed id")
	}
}

func TestListOrder(t *testing.T) {
	m := NewSipRepoModule()
	msg := mustParseMsg(t, inviteBytes)
	ids := []string{m.Store(msg), m.Store(msg), m.Store(msg)}
	if got := m.List(); len(got) != 3 {
		t.Fatalf("len(List) = %d, want 3", len(got))
	} else {
		for i, want := range ids {
			if got[i] != want {
				t.Errorf("List[%d] = %q, want %q", i, got[i], want)
			}
		}
	}
	// Deleting the middle id preserves the order of the rest.
	m.Delete(ids[1])
	got := m.List()
	if len(got) != 2 {
		t.Fatalf("len(List) after delete = %d, want 2", len(got))
	}
	if got[0] != ids[0] || got[1] != ids[2] {
		t.Errorf("List after delete = %v, want %q,%q", got, ids[0], ids[2])
	}
}

func TestStoreNil(t *testing.T) {
	m := NewSipRepoModule()
	if id := m.Store(nil); id != "" {
		t.Errorf("Store(nil) = %q, want empty", id)
	}
	if m.Count() != 0 {
		t.Errorf("Count after Store(nil) = %d, want 0", m.Count())
	}
}

func TestConcurrentSipRepo(t *testing.T) {
	m := NewSipRepoModule()
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				id := m.Store(msg)
				_, _ = m.Retrieve(id)
			}
		}()
	}
	wg.Wait()
	want := goroutines * perG
	if got := m.Count(); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
}
