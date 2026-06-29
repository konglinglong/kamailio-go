// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - qos module tests.
 */

package qos

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

const qosInvite = "INVITE sip:bob@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP host;branch=z9hG4bK1\r\n" +
	"From: <sip:alice@example.com>;tag=1\r\n" +
	"To: <sip:bob@example.com>\r\n" +
	"Call-ID: qos-call-1@host\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

func parseMsg(t *testing.T) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg([]byte(qosInvite))
	if err != nil {
		t.Fatalf("ParseMsg: %v", err)
	}
	return msg
}

func TestSetAndGetQoS(t *testing.T) {
	m := New()
	msg := parseMsg(t)
	if v := m.SetQoS(msg, "downstream", 64000); v != 64000 {
		t.Errorf("SetQoS downstream = %d, want 64000", v)
	}
	m.SetQoS(msg, "upstream", 32000)
	down, up := m.GetQoS(msg)
	if down != 64000 || up != 32000 {
		t.Errorf("GetQoS = (%d,%d), want (64000,32000)", down, up)
	}
}

func TestRemoveQoS(t *testing.T) {
	m := New()
	msg := parseMsg(t)
	m.SetQoS(msg, "downstream", 1000)
	if n := m.RemoveQoS(msg); n != 1 {
		t.Errorf("RemoveQoS = %d, want 1", n)
	}
	if n := m.RemoveQoS(msg); n != 0 {
		t.Errorf("RemoveQoS twice = %d, want 0", n)
	}
	down, up := m.GetQoS(msg)
	if down != 0 || up != 0 {
		t.Errorf("GetQoS after remove = (%d,%d), want (0,0)", down, up)
	}
}

func TestNilAndNoCallID(t *testing.T) {
	m := New()
	if m.SetQoS(nil, "downstream", 100) != 0 {
		t.Error("SetQoS(nil) != 0")
	}
	if down, up := m.GetQoS(nil); down != 0 || up != 0 {
		t.Errorf("GetQoS(nil) = (%d,%d), want (0,0)", down, up)
	}
	if m.RemoveQoS(nil) != 0 {
		t.Error("RemoveQoS(nil) != 0")
	}
	// Message without Call-ID.
	noCID := &parser.SIPMsg{}
	if m.SetQoS(noCID, "downstream", 100) != 0 {
		t.Error("SetQoS(no Call-ID) != 0")
	}
}
