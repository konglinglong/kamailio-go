package proxy

import (
	"context"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// stubAdaptor is a minimal CSCFAdaptor for testing dispatch.
type stubAdaptor struct {
	role     int
	register func(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	invite   func(ctx context.Context, msg *parser.SIPMsg) ResponseAction
	indialog func(ctx context.Context, msg *parser.SIPMsg) ResponseAction
}

func (s *stubAdaptor) Role() int { return s.role }
func (s *stubAdaptor) HandleRegister(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
	if s.register != nil {
		return s.register(ctx, msg)
	}
	return ResponseAction{Status: 0, StopRouting: false}
}
func (s *stubAdaptor) HandleInvite(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
	if s.invite != nil {
		return s.invite(ctx, msg)
	}
	return ResponseAction{Status: 0, StopRouting: false}
}
func (s *stubAdaptor) HandleInDialog(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
	if s.indialog != nil {
		return s.indialog(ctx, msg)
	}
	return ResponseAction{Status: 0, StopRouting: false}
}

func TestSetCSCFAdaptors_Attaches(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	pcore.SetCSCFAdaptors([]CSCFAdaptor{
		&stubAdaptor{role: RolePCSCF},
	})
	if got := len(pcore.cscfAdaptors); got != 1 {
		t.Fatalf("adaptors = %d, want 1", got)
	}
}

func TestApplyCSCFAction_RespondStops(t *testing.T) {
	// A Respond action (Status set, StopRouting true) must report handled.
	act := ResponseAction{Status: 401, Reason: "Unauthorized", StopRouting: true}
	if !applyCSCFAction(ResponseAction{}, act) {
		// applyCSCFAction returns true when the action is terminal
		// (Status != 0 or Target != "" or StopRouting).
		t.Fatalf("applyCSCFAction returned false for Respond")
	}
	_ = act
}

func TestApplyCSCFAction_ForwardStops(t *testing.T) {
	act := ResponseAction{Target: "sip:icscf.home.net:5060", StopRouting: true}
	if !applyCSCFAction(ResponseAction{}, act) {
		t.Fatalf("applyCSCFAction returned false for Forward")
	}
}

func TestApplyCSCFAction_EmptyContinues(t *testing.T) {
	// An empty action (Status 0, no Target, not StopRouting) means
	// "this adaptor declined; try the next".
	act := ResponseAction{}
	if applyCSCFAction(ResponseAction{}, act) {
		t.Fatalf("applyCSCFAction returned true for empty (declined) action")
	}
}

func TestDispatchRegister_RoutesToFirstAdaptor(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	called := 0
	a1 := &stubAdaptor{
		role: RolePCSCF,
		register: func(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
			called++
			return ResponseAction{Status: 401, Reason: "Unauthorized", StopRouting: true}
		},
	}
	a2 := &stubAdaptor{
		role: RoleICSCF,
		register: func(ctx context.Context, msg *parser.SIPMsg) ResponseAction {
			t.Fatal("second adaptor should not be called when first returns terminal")
			return ResponseAction{}
		},
	}
	pcore.SetCSCFAdaptors([]CSCFAdaptor{a1, a2})

	msg := mustBuildRegisterMsg(t, "sip:user@home.net")
	act := pcore.dispatchRegisterViaCSCF(msg, nil)
	if called != 1 {
		t.Fatalf("adaptor1 called %d times, want 1", called)
	}
	if act.Status != 401 {
		t.Fatalf("Status = %d, want 401", act.Status)
	}
}

func TestDispatchRegister_FallsBackWhenAllDecline(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	a1 := &stubAdaptor{role: RolePCSCF} // returns zero (decline)
	pcore.SetCSCFAdaptors([]CSCFAdaptor{a1})

	// Without a registrar attached, the fallback stub returns 200 (no auth).
	// All adaptors decline, so dispatch falls through to the existing path.
	msg := mustBuildRegisterMsg(t, "sip:user@home.net")
	act := pcore.dispatchRegister(msg, nil)
	if act.Status != 200 {
		t.Fatalf("fallback Status = %d, want 200", act.Status)
	}
}

func TestDispatchRegister_NoAdaptorsUsesExistingPath(t *testing.T) {
	pcore := NewProxyCore(&ProxyConfig{Realm: "test"})
	msg := mustBuildRegisterMsg(t, "sip:user@home.net")
	act := pcore.dispatchRegister(msg, nil)
	if act.Status != 200 {
		t.Fatalf("Status = %d, want 200 (no auth fallback)", act.Status)
	}
}

// mustBuildRegisterMsg builds a minimal REGISTER SIPMsg for tests using the
// real parser entry point parser.ParseMsg([]byte).
func mustBuildRegisterMsg(t *testing.T, uri string) *parser.SIPMsg {
	t.Helper()
	raw := "REGISTER sip:home.net SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 127.0.0.1:5060\r\n" +
		"From: <" + uri + ">;tag=abc\r\n" +
		"To: <" + uri + ">\r\n" +
		"Call-ID: test-cid\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <" + uri + ">\r\n" +
		"Content-Length: 0\r\n\r\n"
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		t.Fatalf("parse REGISTER: %v", err)
	}
	return msg
}
