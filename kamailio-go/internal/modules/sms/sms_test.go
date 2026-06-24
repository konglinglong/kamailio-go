// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the sms module - send/receive and PDU (de)coding.
 */
package sms

import (
	"sync"
	"testing"
	"time"
)

func TestSendReceive(t *testing.T) {
	m := New()
	if err := m.Send("alice", "bob", "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if m.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", m.PendingCount())
	}
	select {
	case msg := <-m.Receive():
		if msg.From != "alice" || msg.To != "bob" || msg.Body != "hello" {
			t.Errorf("received = %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("Receive timed out")
	}
	if m.PendingCount() != 0 {
		t.Errorf("PendingCount after read = %d, want 0", m.PendingCount())
	}
}

func TestEncodeDecodePDU(t *testing.T) {
	m := New()
	orig := &SMSMessage{
		From:     "alice",
		To:       "bob",
		Body:     "hello world",
		Coding:   0,
		Validity: 300 * time.Second,
	}
	pdu, err := m.EncodePDU(orig)
	if err != nil {
		t.Fatalf("EncodePDU: %v", err)
	}
	decoded, err := m.DecodePDU(pdu)
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	if decoded.From != orig.From {
		t.Errorf("From = %q, want %q", decoded.From, orig.From)
	}
	if decoded.To != orig.To {
		t.Errorf("To = %q, want %q", decoded.To, orig.To)
	}
	if decoded.Body != orig.Body {
		t.Errorf("Body = %q, want %q", decoded.Body, orig.Body)
	}
	if decoded.Coding != orig.Coding {
		t.Errorf("Coding = %d", decoded.Coding)
	}
	if decoded.Validity != orig.Validity {
		t.Errorf("Validity = %v", decoded.Validity)
	}
}

func TestEncodeDecodeEmptyBody(t *testing.T) {
	m := New()
	orig := &SMSMessage{From: "a", To: "b", Body: ""}
	pdu, err := m.EncodePDU(orig)
	if err != nil {
		t.Fatalf("EncodePDU: %v", err)
	}
	decoded, err := m.DecodePDU(pdu)
	if err != nil {
		t.Fatalf("DecodePDU: %v", err)
	}
	if decoded.Body != "" {
		t.Errorf("Body = %q, want empty", decoded.Body)
	}
}

func TestDecodePDUErrors(t *testing.T) {
	m := New()
	if _, err := m.DecodePDU(nil); err == nil {
		t.Error("DecodePDU(nil) should error")
	}
	if _, err := m.DecodePDU([]byte{0, 0, 0}); err == nil {
		t.Error("DecodePDU short should error")
	}
	if _, err := m.EncodePDU(nil); err == nil {
		t.Error("EncodePDU(nil) should error")
	}
}

func TestInitClearsInbox(t *testing.T) {
	m := New()
	_ = m.Send("a", "b", "c")
	if m.PendingCount() != 1 {
		t.Fatalf("PendingCount = %d, want 1", m.PendingCount())
	}
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.PendingCount() != 0 {
		t.Errorf("PendingCount after Init = %d, want 0", m.PendingCount())
	}
}

func TestDefaultAndInit(t *testing.T) {
	Init()
	d1 := DefaultSMS()
	d2 := DefaultSMS()
	if d1 != d2 {
		t.Error("DefaultSMS should return same instance")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Send("a", "b", "c")
			pdu, _ := m.EncodePDU(&SMSMessage{From: "a", To: "b", Body: "x"})
			_, _ = m.DecodePDU(pdu)
			_ = m.PendingCount()
		}()
	}
	wg.Wait()
	// Drain inbox.
	for {
		select {
		case <-m.Receive():
		default:
			return
		}
	}
}
