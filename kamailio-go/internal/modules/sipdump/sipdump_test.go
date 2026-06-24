// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipDump module tests.
 */
package sipdump

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var inviteBytes = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: cid-dump@10.0.0.1\r\n" +
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

func TestDumpDisabledByDefault(t *testing.T) {
	m := NewSipDumpModule()
	if m.IsEnabled() {
		t.Fatal("expected disabled by default")
	}
	msg := mustParseMsg(t, inviteBytes)
	m.Dump(msg, "in")
	if got := m.Count(); got != 0 {
		t.Errorf("Count when disabled = %d, want 0", got)
	}
}

func TestDumpAndEntries(t *testing.T) {
	m := NewSipDumpModule()
	m.SetEnabled(true)
	msg := mustParseMsg(t, inviteBytes)

	m.Dump(msg, "in")
	m.Dump(msg, "out")

	entries := m.GetEntries()
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Direction != "in" || entries[1].Direction != "out" {
		t.Errorf("directions = %q/%q", entries[0].Direction, entries[1].Direction)
	}
	if entries[0].Payload == "" {
		t.Error("expected non-empty payload")
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	// nil message is a no-op.
	m.Dump(nil, "in")
	if got := m.Count(); got != 2 {
		t.Errorf("Count after nil dump = %d, want 2", got)
	}
}

func TestClear(t *testing.T) {
	m := NewSipDumpModule()
	m.SetEnabled(true)
	msg := mustParseMsg(t, inviteBytes)
	m.Dump(msg, "in")
	m.Dump(msg, "out")
	if m.Count() != 2 {
		t.Fatalf("Count = %d, want 2 before clear", m.Count())
	}
	m.Clear()
	if m.Count() != 0 {
		t.Errorf("Count after Clear = %d, want 0", m.Count())
	}
	if got := m.GetEntries(); len(got) != 0 {
		t.Errorf("len(GetEntries) after Clear = %d, want 0", len(got))
	}
}

func TestConcurrentDump(t *testing.T) {
	m := NewSipDumpModule()
	m.SetEnabled(true)
	msg := mustParseMsg(t, inviteBytes)
	var wg sync.WaitGroup
	const goroutines = 25
	const perG = 20
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Dump(msg, "in")
			}
		}()
	}
	wg.Wait()
	want := goroutines * perG
	if got := m.Count(); got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}
}
