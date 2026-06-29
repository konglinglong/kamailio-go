// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Diameter peer state machine.
 */

package cdp

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPeerInitialState(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	if p.State() != StateClosed {
		t.Errorf("initial state = %s, want Closed", p.State())
	}
	if p.IsOpen() {
		t.Errorf("IsOpen() = true, want false")
	}
	if !p.IsClosed() {
		t.Errorf("IsClosed() = false, want true")
	}
	if p.IsConnecting() {
		t.Errorf("IsConnecting() = true, want false")
	}
}

func TestPeerStateString(t *testing.T) {
	cases := []struct {
		state PeerState
		name  string
	}{
		{StateClosed, "Closed"},
		{StateWaitConnAck, "Wait-Conn-Ack"},
		{StateWaitICEA, "Wait-I-CEA"},
		{StateWaitConnAckOpen, "Wait-Conn-Ack-Open"},
		{StateROpen, "R-Open"},
		{StateIWaitCEA, "I-Wait-CEA"},
		{StateClosing, "Closing"},
		{StateWaitReturns, "Wait-Returns"},
		{StateClosedIdle, "Closed-Idle"},
	}
	for _, c := range cases {
		if got := c.state.String(); got != c.name {
			t.Errorf("State(%d).String() = %q, want %q", int(c.state), got, c.name)
		}
	}
	if PeerState(99).String() == "Closed" {
		t.Errorf("unknown state should not say Closed")
	}
}

func TestPeerEventString(t *testing.T) {
	cases := []struct {
		event PeerEvent
		name  string
	}{
		{EvStart, "Start"},
		{EvStop, "Stop"},
		{EvTimeout, "Timeout"},
		{EvConnectRequest, "ConnectRequest"},
		{EvConnectAccepted, "ConnectAccepted"},
		{EvConnectRejected, "ConnectRejected"},
		{EvConnectionAccepted, "ConnectionAccepted"},
		{EvConnectionFailure, "ConnectionFailure"},
		{EvCERReceived, "CERReceived"},
		{EvCEAReceived, "CEAReceived"},
		{EvDPRReceived, "DPRReceived"},
		{EvDPAReceived, "DPAReceived"},
		{EvDWRReceived, "DWRReceived"},
		{EvDWAReceived, "DWAReceived"},
	}
	for _, c := range cases {
		if got := c.event.String(); got != c.name {
			t.Errorf("Event(%d).String() = %q, want %q", int(c.event), got, c.name)
		}
	}
	if PeerEvent(99).String() == "Start" {
		t.Errorf("unknown event should not say Start")
	}
}

func TestPeerActionString(t *testing.T) {
	cases := []struct {
		action PeerAction
		name   string
	}{
		{ActionNone, "none"},
		{ActionConnect, "connect"},
		{ActionDisconnect, "disconnect"},
		{ActionSendCER, "send-cer"},
		{ActionSendCEA, "send-cea"},
		{ActionSendDPR, "send-dpr"},
		{ActionSendDPA, "send-dpa"},
		{ActionSendDWA, "send-dwa"},
		{ActionSendDWR, "send-dwr"},
		{ActionReject, "reject"},
		{ActionRetry, "retry"},
		{ActionCleanup, "cleanup"},
		{ActionDropMessage, "drop-message"},
	}
	for _, c := range cases {
		if got := c.action.String(); got != c.name {
			t.Errorf("Action(%d).String() = %q, want %q", int(c.action), got, c.name)
		}
	}
}

// TestPeerStateMachineHappyPath walks through the typical lifecycle of a
// peer that initiates a connection (the "I" side of RFC 6733 §5.3):
//   Closed --Start--> WaitConnAck --ConnectAccepted--> WaitICEA --CEAReceived--> ROpen --Stop--> Closing --DPAReceived--> Closed
func TestPeerStateMachineHappyPath(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)

	// Closed --Start--> WaitConnAck, action=Connect.
	tr, err := p.ProcessEvent(EvStart)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if tr.To != StateWaitConnAck {
		t.Errorf("Start: To = %s, want WaitConnAck", tr.To)
	}
	if tr.Action != ActionConnect {
		t.Errorf("Start: Action = %s, want Connect", tr.Action)
	}

	// WaitConnAck --ConnectAccepted--> WaitICEA, action=SendCER.
	tr, err = p.ProcessEvent(EvConnectAccepted)
	if err != nil {
		t.Fatalf("ConnectAccepted: %v", err)
	}
	if tr.To != StateWaitICEA {
		t.Errorf("ConnectAccepted: To = %s, want WaitICEA", tr.To)
	}
	if tr.Action != ActionSendCER {
		t.Errorf("ConnectAccepted: Action = %s, want SendCER", tr.Action)
	}
	if !p.IsConnecting() {
		t.Errorf("IsConnecting() = false after ConnectAccepted")
	}

	// WaitICEA --CEAReceived--> ROpen, action=None.
	tr, err = p.ProcessEvent(EvCEAReceived)
	if err != nil {
		t.Fatalf("CEAReceived: %v", err)
	}
	if tr.To != StateROpen {
		t.Errorf("CEAReceived: To = %s, want ROpen", tr.To)
	}
	if tr.Action != ActionNone {
		t.Errorf("CEAReceived: Action = %s, want None", tr.Action)
	}
	if !p.IsOpen() {
		t.Errorf("IsOpen() = false after CEAReceived")
	}

	// ROpen --DWRReceived--> ROpen, action=SendDWA.
	tr, err = p.ProcessEvent(EvDWRReceived)
	if err != nil {
		t.Fatalf("DWRReceived: %v", err)
	}
	if tr.To != StateROpen {
		t.Errorf("DWRReceived: To = %s, want ROpen", tr.To)
	}
	if tr.Action != ActionSendDWA {
		t.Errorf("DWRReceived: Action = %s, want SendDWA", tr.Action)
	}

	// ROpen --DWAReceived--> ROpen, action=None (resets activity).
	tr, err = p.ProcessEvent(EvDWAReceived)
	if err != nil {
		t.Fatalf("DWAReceived: %v", err)
	}
	if tr.To != StateROpen {
		t.Errorf("DWAReceived: To = %s, want ROpen", tr.To)
	}

	// ROpen --Stop--> Closing, action=SendDPR.
	tr, err = p.ProcessEvent(EvStop)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if tr.To != StateClosing {
		t.Errorf("Stop: To = %s, want Closing", tr.To)
	}
	if tr.Action != ActionSendDPR {
		t.Errorf("Stop: Action = %s, want SendDPR", tr.Action)
	}

	// Closing --DPAReceived--> Closed, action=Disconnect.
	tr, err = p.ProcessEvent(EvDPAReceived)
	if err != nil {
		t.Fatalf("DPAReceived: %v", err)
	}
	if tr.To != StateClosed {
		t.Errorf("DPAReceived: To = %s, want Closed", tr.To)
	}
	if tr.Action != ActionDisconnect {
		t.Errorf("DPAReceived: Action = %s, want Disconnect", tr.Action)
	}
	if !p.IsClosed() {
		t.Errorf("IsClosed() = false after DPAReceived")
	}
}

// TestPeerResponderPath exercises the responder side ("R" side of
// RFC 6733 §5.3): an incoming connection is accepted, the peer sends a
// CER, we reply with a CEA and enter ROpen.
func TestPeerResponderPath(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)

	tr, err := p.ProcessEvent(EvConnectionAccepted)
	if err != nil {
		t.Fatalf("ConnectionAccepted: %v", err)
	}
	if tr.To != StateWaitConnAckOpen {
		t.Errorf("ConnectionAccepted: To = %s, want WaitConnAckOpen", tr.To)
	}

	tr, err = p.ProcessEvent(EvCERReceived)
	if err != nil {
		t.Fatalf("CERReceived: %v", err)
	}
	if tr.To != StateROpen {
		t.Errorf("CERReceived: To = %s, want ROpen", tr.To)
	}
	if tr.Action != ActionSendCEA {
		t.Errorf("CERReceived: Action = %s, want SendCEA", tr.Action)
	}
}

// TestPeerConnectRejected exercises the failure path where an outbound
// connect fails: WaitConnAck --ConnectRejected--> Closed.
func TestPeerConnectRejected(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	if _, err := p.ProcessEvent(EvStart); err != nil {
		t.Fatalf("Start: %v", err)
	}

	tr, err := p.ProcessEvent(EvConnectRejected)
	if err != nil {
		t.Fatalf("ConnectRejected: %v", err)
	}
	if tr.To != StateClosed {
		t.Errorf("ConnectRejected: To = %s, want Closed", tr.To)
	}
	if p.LastError() == nil {
		t.Errorf("LastError should be set after ConnectRejected")
	}
}

// TestPeerIllegalTransition verifies that an event which is invalid for
// the current state returns ErrIllegalTransition and leaves the state
// unchanged.
func TestPeerIllegalTransition(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	// In StateClosed, EvCEAReceived is invalid.
	_, err := p.ProcessEvent(EvCEAReceived)
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("CEAReceived in Closed: err = %v, want ErrIllegalTransition", err)
	}
	if p.State() != StateClosed {
		t.Errorf("state changed after illegal transition: %s", p.State())
	}
}

// TestPeerDWRRetries verifies that the watchdog retry counter increments
// on EvDWARetry and resets on EvDWAReceived.
func TestPeerDWRRetries(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	// Drive to ROpen.
	for _, ev := range []PeerEvent{EvStart, EvConnectAccepted, EvCEAReceived} {
		if _, err := p.ProcessEvent(ev); err != nil {
			t.Fatalf("event %s: %v", ev, err)
		}
	}

	// Two unanswered DWR retries.
	if _, err := p.ProcessEvent(EvDWARetry); err != nil {
		t.Fatalf("DWARetry: %v", err)
	}
	if _, err := p.ProcessEvent(EvDWARetry); err != nil {
		t.Fatalf("DWARetry: %v", err)
	}
	if got := p.DWRRetries(); got != 2 {
		t.Errorf("DWRRetries = %d, want 2", got)
	}

	// A DWA resets the counter.
	if _, err := p.ProcessEvent(EvDWAReceived); err != nil {
		t.Fatalf("DWAReceived: %v", err)
	}
	if got := p.DWRRetries(); got != 0 {
		t.Errorf("DWRRetries after DWA = %d, want 0", got)
	}
}

// TestPeerDWRFinal exercises the watchdog final-retry path: ROpen
// --DWRFinal--> Closed with action=Disconnect.
func TestPeerDWRFinal(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	for _, ev := range []PeerEvent{EvStart, EvConnectAccepted, EvCEAReceived} {
		if _, err := p.ProcessEvent(ev); err != nil {
			t.Fatalf("event %s: %v", ev, err)
		}
	}

	tr, err := p.ProcessEvent(EvDWRFinal)
	if err != nil {
		t.Fatalf("DWRFinal: %v", err)
	}
	if tr.To != StateClosed {
		t.Errorf("DWRFinal: To = %s, want Closed", tr.To)
	}
	if tr.Action != ActionDisconnect {
		t.Errorf("DWRFinal: Action = %s, want Disconnect", tr.Action)
	}
}

// TestPeerDPRWhileOpen exercises the graceful-shutdown path from the
// responder side: ROpen --DPRReceived--> Closed with action=SendDPA.
func TestPeerDPRWhileOpen(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	for _, ev := range []PeerEvent{EvStart, EvConnectAccepted, EvCEAReceived} {
		if _, err := p.ProcessEvent(ev); err != nil {
			t.Fatalf("event %s: %v", ev, err)
		}
	}

	tr, err := p.ProcessEvent(EvDPRReceived)
	if err != nil {
		t.Fatalf("DPRReceived: %v", err)
	}
	if tr.To != StateClosed {
		t.Errorf("DPRReceived: To = %s, want Closed", tr.To)
	}
	if tr.Action != ActionSendDPA {
		t.Errorf("DPRReceived: Action = %s, want SendDPA", tr.Action)
	}
}

// TestPeerMessageInHandshake verifies that messages queued during the
// CER/CEA handshake are dropped (action=DropMessage) rather than sent.
func TestPeerMessageInHandshake(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	if _, err := p.ProcessEvent(EvStart); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := p.ProcessEvent(EvConnectAccepted); err != nil {
		t.Fatalf("ConnectAccepted: %v", err)
	}
	// Now in WaitICEA.
	tr, err := p.ProcessEvent(EvSendMessage)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if tr.Action != ActionDropMessage {
		t.Errorf("SendMessage: Action = %s, want DropMessage", tr.Action)
	}
	if tr.To != StateWaitICEA {
		t.Errorf("SendMessage: To = %s, want WaitICEA", tr.To)
	}
}

// TestPeerConcurrentTransitions hammers ProcessEvent from many goroutines.
// The state machine must remain consistent; some events will be rejected
// as illegal but must never panic.
func TestPeerConcurrentTransitions(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	events := []PeerEvent{
		EvStart, EvConnectAccepted, EvCEAReceived,
		EvDWRReceived, EvDWAReceived, EvStop, EvDPAReceived,
		EvConnectionFailure, EvCleanup,
	}
	var wg sync.WaitGroup
	const goroutines = 100
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			ev := events[seed%len(events)]
			_, _ = p.ProcessEvent(ev)
			_ = p.State()
			_ = p.IsOpen()
			_ = p.IsClosed()
		}(i)
	}
	wg.Wait()
}

// TestPeerDPRCause verifies the DPR cause accessor.
func TestPeerDPRCause(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	if got := p.DPRCause(); got != 0 {
		t.Errorf("DPRCause initial = %d, want 0", got)
	}
	p.SetDPRCause(DisconnectCauseRebooting)
	if got := p.DPRCause(); got != DisconnectCauseRebooting {
		t.Errorf("DPRCause after set = %d, want %d", got, DisconnectCauseRebooting)
	}
}

// TestPeerLastActivity verifies the LastActivity accessor returns a
// non-zero time and is updated by EvDWAReceived.
func TestPeerLastActivity(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	t0 := p.LastActivity()
	if t0.IsZero() {
		t.Fatalf("LastActivity is zero")
	}
	time.Sleep(time.Millisecond)
	for _, ev := range []PeerEvent{EvStart, EvConnectAccepted, EvCEAReceived} {
		_, _ = p.ProcessEvent(ev)
	}
	_, _ = p.ProcessEvent(EvDWAReceived)
	t1 := p.LastActivity()
	if !t1.After(t0) {
		t.Errorf("LastActivity not updated by DWA: %v -> %v", t0, t1)
	}
}

// TestPeerResetDWRRetries verifies the manual reset path.
func TestPeerResetDWRRetries(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	for _, ev := range []PeerEvent{EvStart, EvConnectAccepted, EvCEAReceived, EvDWARetry, EvDWARetry} {
		_, _ = p.ProcessEvent(ev)
	}
	if p.DWRRetries() != 2 {
		t.Errorf("DWRRetries = %d, want 2", p.DWRRetries())
	}
	p.ResetDWRRetries()
	if p.DWRRetries() != 0 {
		t.Errorf("DWRRetries after reset = %d, want 0", p.DWRRetries())
	}
}

// TestPeerApplications verifies the SetApplications/Applications round trip.
func TestPeerApplications(t *testing.T) {
	p := NewPeer("h1", "r1", "10.0.0.1", 3868)
	apps := []uint32{AppDiameterSIP, AppDiameterCreditControl}
	p.SetApplications(apps)
	got := p.Applications()
	if len(got) != 2 {
		t.Fatalf("Applications len = %d, want 2", len(got))
	}
	// Mutating the returned slice must not affect the peer's copy.
	got[0] = 999999
	again := p.Applications()
	if again[0] == 999999 {
		t.Errorf("Applications returned a live reference to internal state")
	}
}

// TestPeerTransitionString verifies the Transition.String() formatter.
func TestPeerTransitionString(t *testing.T) {
	tr := Transition{From: StateClosed, Event: EvStart, To: StateWaitConnAck, Action: ActionConnect}
	s := tr.String()
	if s == "" {
		t.Errorf("Transition.String() returned empty")
	}
}
