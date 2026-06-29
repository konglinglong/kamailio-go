// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SEAS module tests - SIP Express Application Server interface.
 */
package seas

import (
	"errors"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

var inviteBytes = []byte("INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 10.0.0.1;branch=z9hG4bK776\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: cid-seas@10.0.0.1\r\n" +
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

func TestRegisterAndIsASRegistered(t *testing.T) {
	m := NewSEASModule()
	if m.IsASRegistered("as1") {
		t.Fatal("expected as1 not registered initially")
	}
	m.RegisterAS("as1", "10.0.0.10:5060")
	if !m.IsASRegistered("as1") {
		t.Error("expected as1 registered after RegisterAS")
	}
	// Re-register updates without error.
	m.RegisterAS("as1", "10.0.0.11:5060")
	if !m.IsASRegistered("as1") {
		t.Error("expected as1 still registered after re-register")
	}
	m.UnregisterAS("as1")
	if m.IsASRegistered("as1") {
		t.Error("expected as1 unregistered after UnregisterAS")
	}
}

func TestSendToAS(t *testing.T) {
	m := NewSEASModule()
	msg := mustParseMsg(t, inviteBytes)

	// Sending to an unregistered AS fails.
	if err := m.SendToAS("ghost", msg); err == nil {
		t.Error("expected error sending to unregistered AS")
	}
	// nil message fails.
	if err := m.SendToAS("as1", nil); err == nil {
		t.Error("expected error sending nil message")
	}

	m.RegisterAS("as1", "10.0.0.10:5060")
	if err := m.SendToAS("as1", msg); err != nil {
		t.Errorf("SendToAS failed: %v", err)
	}
	if got := m.SentCount(); got != 1 {
		t.Errorf("SentCount = %d, want 1", got)
	}
}

func TestSendToASTransportError(t *testing.T) {
	m := NewSEASModule()
	msg := mustParseMsg(t, inviteBytes)
	m.RegisterAS("as1", "10.0.0.10:5060")

	boom := errors.New("transport down")
	m.SetTransport(func(name, addr string, _ *parser.SIPMsg) error {
		if name != "as1" || addr != "10.0.0.10:5060" {
			return errors.New("bad args")
		}
		return boom
	})
	if err := m.SendToAS("as1", msg); err != boom {
		t.Errorf("SendToAS err = %v, want %v", err, boom)
	}
	// Failed dispatch must not be recorded as sent.
	if got := m.SentCount(); got != 0 {
		t.Errorf("SentCount after failure = %d, want 0", got)
	}
	// nil transport restores default.
	m.SetTransport(nil)
	if err := m.SendToAS("as1", msg); err != nil {
		t.Errorf("SendToAS after default transport failed: %v", err)
	}
}

func TestConcurrentSEAS(t *testing.T) {
	m := NewSEASModule()
	msg := mustParseMsg(t, inviteBytes)
	m.RegisterAS("as1", "10.0.0.10:5060")
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				_ = m.SendToAS("as1", msg)
				_ = m.IsASRegistered("as1")
			}
		}()
	}
	wg.Wait()
	want := goroutines * perG
	if got := m.SentCount(); got != want {
		t.Errorf("SentCount = %d, want %d", got, want)
	}
}
