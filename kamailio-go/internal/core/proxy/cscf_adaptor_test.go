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
