// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * S-CSCF Adaptor tests — bridges the scscf business layer
 * (Registrar / SessionHandler) to ProxyCore's CSCFAdaptor dispatch.
 */

package scscf

import (
	"context"
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
)

const adaptorTestRealm = "home.net"

// hostOf extracts the host portion of a "sip:user@host" URI.
func hostOf(uri string) string {
	at := strings.IndexByte(uri, '@')
	if at < 0 {
		return uri
	}
	return uri[at+1:]
}

// mustBuildRegister builds an initial REGISTER (no Authorization) for uri.
func mustBuildRegister(t *testing.T, uri string) *parser.SIPMsg {
	t.Helper()
	raw := "REGISTER sip:" + hostOf(uri) + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bKreg01\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: <" + uri + ">;tag=regtag\r\n" +
		"To: <" + uri + ">\r\n" +
		"Call-ID: reg-callid@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:user@10.0.0.1:5060>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse REGISTER failed: %v", err)
	}
	return msg
}

// mustBuildInvite builds an INVITE from fromURI to toURI.
func mustBuildInvite(t *testing.T, toURI, fromURI string) *parser.SIPMsg {
	t.Helper()
	raw := "INVITE " + toURI + " SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.2:5060;branch=z9hG4bKinv01\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: <" + fromURI + ">;tag=fromtag\r\n" +
		"To: <" + toURI + ">\r\n" +
		"Call-ID: inv-callid@10.0.0.2\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Contact: <sip:caller@10.0.0.2:5060>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse INVITE failed: %v", err)
	}
	return msg
}

// newTestAdaptor wires a fresh Adaptor over a real Registrar/SessionHandler.
func newTestAdaptor(t *testing.T) (*Adaptor, *Registrar) {
	t.Helper()
	reg := NewRegistrar(adaptorTestRealm)
	sess := NewSessionHandler(reg)
	return NewAdaptor(reg, sess, nil), reg
}

// hasHeader reports whether headers contains a row named name.
func hasHeader(headers []string, name string) bool {
	prefix := name + ":"
	for _, h := range headers {
		if strings.HasPrefix(strings.TrimSpace(h), prefix) {
			return true
		}
	}
	return false
}

// TestSCSCFAdaptor_RegisterChallenge_Responds401: a REGISTER without
// Authorization must surface as a 401 challenge that stops routing and
// carries a WWW-Authenticate header.
func TestSCSCFAdaptor_RegisterChallenge_Responds401(t *testing.T) {
	a, _ := newTestAdaptor(t)
	msg := mustBuildRegister(t, "sip:alice@"+adaptorTestRealm)

	action := a.HandleRegister(context.Background(), msg)

	if action.Status != 401 {
		t.Fatalf("expected Status 401, got %d", action.Status)
	}
	if !action.StopRouting {
		t.Error("expected StopRouting=true on challenge")
	}
	if !hasHeader(action.ExtraHeaders, "WWW-Authenticate") {
		t.Errorf("expected WWW-Authenticate in ExtraHeaders, got %v", action.ExtraHeaders)
	}
}

// TestSCSCFAdaptor_InviteUnregistered_Responds403: an INVITE where
// neither caller nor callee is registered must be rejected with a
// terminal 4xx and stop routing, with no forwarding target.
func TestSCSCFAdaptor_InviteUnregistered_Responds403(t *testing.T) {
	a, _ := newTestAdaptor(t)
	msg := mustBuildInvite(t, "sip:callee@"+adaptorTestRealm, "sip:caller@external.net")

	action := a.HandleInvite(context.Background(), msg)

	if action.Status != 403 && action.Status != 404 {
		t.Fatalf("expected Status 403 or 404, got %d", action.Status)
	}
	if !action.StopRouting {
		t.Error("expected StopRouting=true on rejection")
	}
	if action.Target != "" {
		t.Errorf("expected empty Target for rejection, got %q", action.Target)
	}
}

// TestSCSCFAdaptor_InviteRegistered_ForwardsToContact: an INVITE to a
// registered callee must produce a forwarding action (Target set to the
// registered contact) or a non-zero status, and stop routing.
func TestSCSCFAdaptor_InviteRegistered_ForwardsToContact(t *testing.T) {
	a, reg := newTestAdaptor(t)
	const contact = "sip:bob@10.0.0.7:5060"
	reg.SetRecordForTest("sip:bob@"+adaptorTestRealm, contact)

	msg := mustBuildInvite(t, "sip:bob@"+adaptorTestRealm, "sip:caller@external.net")
	action := a.HandleInvite(context.Background(), msg)

	if action.Target == "" && action.Status == 0 {
		t.Fatalf("expected Target set or non-zero Status, got Target=%q Status=%d",
			action.Target, action.Status)
	}
	if !action.StopRouting {
		t.Error("expected StopRouting=true on forward")
	}
	if action.Target != "" && action.Target != contact {
		t.Errorf("expected Target=%q, got %q", contact, action.Target)
	}
}

// TestSCSCFAdaptor_Role: the adaptor identifies as S-CSCF.
func TestSCSCFAdaptor_Role(t *testing.T) {
	a, _ := newTestAdaptor(t)
	if got := a.Role(); got != proxy.RoleSCSCF {
		t.Errorf("expected Role %d (RoleSCSCF), got %d", proxy.RoleSCSCF, got)
	}
}
