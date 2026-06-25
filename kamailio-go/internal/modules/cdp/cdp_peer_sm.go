// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Diameter peer state machine.
 * Port of the kamailio cdp module's peerstatemachine.c.
 *
 * The state machine drives the Diameter peer lifecycle: connection
 * establishment, the Capabilities-Exchange handshake (CER/CEA), the
 * established-state exchange of DWR/DWA, and the DPR/DPA shutdown dance.
 *
 * Mirrors the 8-state, 25-event state machine described in RFC 6733 §5.3
 * ("Peer State Machine"). The state machine is *pure* — it transitions
 * state and produces events for the transport layer to act on, but does
 * not itself perform I/O. This keeps the unit tests fast (no sockets) and
 * allows the transport layer to be swapped without touching the SM.
 *
 * It is safe for concurrent use.
 */

package cdp

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Peer states (RFC 6733 §5.3 / cdp peerstatemachine.c "cdp_peer_state_t")
// ---------------------------------------------------------------------------

// PeerState is the lifecycle state of a Diameter peer.
type PeerState int

const (
	// StateClosed: no connection has been established. Initial state.
	StateClosed PeerState = iota
	// StateWaitConnAck: a TCP connection has been initiated and we are
	// waiting for the transport layer to acknowledge it.
	StateWaitConnAck
	// StateWaitICEA: we sent a CER (Capabilities-Exchange-Request) and are
	// waiting for the corresponding CEA.
	StateWaitICEA
	// StateWaitConnAckOpen: a connection was accepted and we are waiting
	// for the transport to acknowledge it before sending our CER.
	StateWaitConnAckOpen
	// StateROpen: the connection is open and we are the responder side
	// (i.e. the remote peer initiated the connection and sent the CER).
	StateROpen
	// StateIWaitCEA: alternate spelling used by some implementations for
	// the I side; provided for compatibility.
	StateIWaitCEA
	// StateClosing: a DPR has been sent and we are waiting for the DPA
	// before tearing down the connection.
	StateClosing
	// StateWaitReturns: the connection is being torn down and we are
	// waiting for outstanding transactions to return.
	StateWaitReturns
	// StateClosedIdle is the idle state used between failed connection
	// attempts (not part of RFC 6733; mirrors Kamailio's INTERNAL state).
	StateClosedIdle
)

// String returns a human-readable state name.
func (s PeerState) String() string {
	switch s {
	case StateClosed:
		return "Closed"
	case StateWaitConnAck:
		return "Wait-Conn-Ack"
	case StateWaitICEA:
		return "Wait-I-CEA"
	case StateWaitConnAckOpen:
		return "Wait-Conn-Ack-Open"
	case StateROpen:
		return "R-Open"
	case StateIWaitCEA:
		return "I-Wait-CEA"
	case StateClosing:
		return "Closing"
	case StateWaitReturns:
		return "Wait-Returns"
	case StateClosedIdle:
		return "Closed-Idle"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ---------------------------------------------------------------------------
// Peer events (cdp peerstatemachine.c — cdp_peer_event_t)
// ---------------------------------------------------------------------------

// PeerEvent is an event that triggers a state transition.
type PeerEvent int

const (
	EvStart PeerEvent = iota
	EvStop
	EvTimeout
	EvConnectRequest // transport layer: outgoing connect requested
	EvConnectAccepted // outgoing connect succeeded
	EvConnectRejected // outgoing connect failed
	EvConnectionAccepted // incoming connect accepted
	EvConnectionFailure // connection broke
	EvConnectionTimeout // inactivity / watchdog timeout
	EvCERReceived // CER received from peer
	EvCERTimeout // CER not received within Tw timer
	EvCEAReceived // CEA received (answer to our CER)
	EvCEANotAcceptable // CEA received but rejected capabilities
	EvCEAUnknownPeer // CEA received from unknown peer
	EvDPRReceived // DPR received
	EvDPAReceived // DPA received (answer to our DPR)
	EvDWRReceived // DWR received
	EvDWAReceived // DWA received (answer to our DWR)
	EvDWARetry // DWA not received, retry DWR
	EvDWRFinal // DWA not received after final retry
	EvSendMessage // app queued a message to send
	EvReceiveMessage // peer sent a Diameter message
	EvProcessMessage // app asked to process a received message
	EvCleanup // forced cleanup (e.g. module shutdown)
	EvDiscarded // message was discarded
)

// String returns a human-readable event name.
func (e PeerEvent) String() string {
	switch e {
	case EvStart:
		return "Start"
	case EvStop:
		return "Stop"
	case EvTimeout:
		return "Timeout"
	case EvConnectRequest:
		return "ConnectRequest"
	case EvConnectAccepted:
		return "ConnectAccepted"
	case EvConnectRejected:
		return "ConnectRejected"
	case EvConnectionAccepted:
		return "ConnectionAccepted"
	case EvConnectionFailure:
		return "ConnectionFailure"
	case EvConnectionTimeout:
		return "ConnectionTimeout"
	case EvCERReceived:
		return "CERReceived"
	case EvCERTimeout:
		return "CERTimeout"
	case EvCEAReceived:
		return "CEAReceived"
	case EvCEANotAcceptable:
		return "CEANotAcceptable"
	case EvCEAUnknownPeer:
		return "CEAUnknownPeer"
	case EvDPRReceived:
		return "DPRReceived"
	case EvDPAReceived:
		return "DPAReceived"
	case EvDWRReceived:
		return "DWRReceived"
	case EvDWAReceived:
		return "DWAReceived"
	case EvDWARetry:
		return "DWARetry"
	case EvDWRFinal:
		return "DWRFinal"
	case EvSendMessage:
		return "SendMessage"
	case EvReceiveMessage:
		return "ReceiveMessage"
	case EvProcessMessage:
		return "ProcessMessage"
	case EvCleanup:
		return "Cleanup"
	case EvDiscarded:
		return "Discarded"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// ---------------------------------------------------------------------------
// Actions — the side effects that the transport layer must perform after
// a state transition. The state machine returns an Action, not a function,
// so that the transport can perform them synchronously or asynchronously.
// ---------------------------------------------------------------------------

// PeerAction is the action the transport layer should perform after
// processing an event.
type PeerAction int

const (
	ActionNone PeerAction = iota
	// ActionConnect initiates an outgoing TCP connection.
	ActionConnect
	// ActionDisconnect tears down the existing TCP connection.
	ActionDisconnect
	// ActionSendCER sends a Capabilities-Exchange-Request.
	ActionSendCER
	// ActionSendCEA sends a Capabilities-Exchange-Answer (response to a
	// received CER).
	ActionSendCEA
	// ActionSendDPR sends a Disconnect-Peer-Request.
	ActionSendDPR
	// ActionSendDPA sends a Disconnect-Peer-Answer.
	ActionSendDPA
	// ActionSendDWA sends a Device-Watchdog-Answer.
	ActionSendDWA
	// ActionSendDWR sends a Device-Watchdog-Request.
	ActionSendDWR
	// ActionReject rejects the connection (typically by closing it
	// without sending any Diameter message).
	ActionReject
	// ActionRetry waits for the Tw timer and retries the last request.
	ActionRetry
	// ActionCleanup removes the peer from the peer table.
	ActionCleanup
	// ActionDropMessage discards the message that triggered the event.
	ActionDropMessage
)

// String returns a human-readable action name.
func (a PeerAction) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionConnect:
		return "connect"
	case ActionDisconnect:
		return "disconnect"
	case ActionSendCER:
		return "send-cer"
	case ActionSendCEA:
		return "send-cea"
	case ActionSendDPR:
		return "send-dpr"
	case ActionSendDPA:
		return "send-dpa"
	case ActionSendDWA:
		return "send-dwa"
	case ActionSendDWR:
		return "send-dwr"
	case ActionReject:
		return "reject"
	case ActionRetry:
		return "retry"
	case ActionCleanup:
		return "cleanup"
	case ActionDropMessage:
		return "drop-message"
	default:
		return fmt.Sprintf("unknown(%d)", int(a))
	}
}

// Transition captures the result of a state machine transition.
type Transition struct {
	From   PeerState
	Event  PeerEvent
	To     PeerState
	Action PeerAction
}

// String returns a human-readable transition.
func (t Transition) String() string {
	return fmt.Sprintf("%s --%s--> %s [%s]", t.From, t.Event, t.To, t.Action)
}

// ErrIllegalTransition is returned when an event is not valid for the
// current state. The state machine leaves the state unchanged.
var ErrIllegalTransition = errors.New("cdp: illegal peer state transition")

// ---------------------------------------------------------------------------
// Peer (stateful)
// ---------------------------------------------------------------------------

// Peer is a Diameter peer with a full state machine. It extends the
// minimal DiameterPeer struct (used by the simple stub API) with the
// runtime state needed to drive the protocol.
type Peer struct {
	mu sync.Mutex

	// Identity (mirrors DiameterPeer).
	Host  string
	Realm string
	IP    string
	Port  int

	// State machine.
	state    PeerState
	lastErr  error
	createdAt time.Time
	updatedAt time.Time

	// Capabilities advertised / received during the CER/CEA exchange.
	applications []uint32
	supportedVendors []uint32

	// Watchdog / Tw timer bookkeeping.
	dwrRetries int
	lastActivity time.Time

	// Pending DPR cause (for outgoing DPR).
	pendingDPRCause uint32
}

// NewPeer creates a Peer in the StateClosed state.
func NewPeer(host, realm, ip string, port int) *Peer {
	now := time.Now()
	return &Peer{
		Host:       host,
		Realm:      realm,
		IP:         ip,
		Port:       port,
		state:      StateClosed,
		createdAt:  now,
		updatedAt:  now,
		lastActivity: now,
	}
}

// State returns the current peer state.
func (p *Peer) State() PeerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// LastError returns the last error recorded during a state transition
// (e.g. the reason for a transition to StateClosed).
func (p *Peer) LastError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

// Applications returns a snapshot of the application ids the peer has
// advertised during the last CER/CEA exchange.
func (p *Peer) Applications() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]uint32, len(p.applications))
	copy(out, p.applications)
	return out
}

// SetApplications replaces the peer's advertised applications (used when
// processing a received CER/CEA).
func (p *Peer) SetApplications(apps []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.applications = append([]uint32(nil), apps...)
}

// ---------------------------------------------------------------------------
// State machine processing (cdp peerstatemachine.c — sm_process)
// ---------------------------------------------------------------------------

// ProcessEvent applies event to the peer's state machine and returns the
// transition that was performed. Returns ErrIllegalTransition when the
// event is not valid for the current state — the state is left unchanged.
//
// The transport layer is responsible for performing the returned Action.
//
//	C: sm_process()
func (p *Peer) ProcessEvent(event PeerEvent) (Transition, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	from := p.state
	to, action, err := p.transition(from, event)
	if err != nil {
		return Transition{}, err
	}

	p.state = to
	p.updatedAt = time.Now()
	if event == EvConnectionFailure || event == EvConnectRejected ||
		event == EvDWRFinal || event == EvCEAUnknownPeer ||
		event == EvCERTimeout || event == EvConnectionTimeout {
		p.lastErr = fmt.Errorf("event %s in state %s", event, from)
	}

	// Track DWR retry count: reset on receipt of DWA, increment on retry.
	switch event {
	case EvDWAReceived:
		p.dwrRetries = 0
	case EvDWARetry:
		p.dwrRetries++
	}

	return Transition{From: from, Event: event, To: to, Action: action}, nil
}

// transition returns the target state and action for the (state, event)
// pair. Mirrors the state-transition table in cdp peerstatemachine.c.
//
// The table below follows RFC 6733 §5.3. Rows marked "—" are illegal
// (return ErrIllegalTransition); the state is left unchanged. Where the
// transport must perform an action, the corresponding PeerAction is returned.
func (p *Peer) transition(from PeerState, event PeerEvent) (PeerState, PeerAction, error) {
	switch from {
	case StateClosed:
		switch event {
		case EvStart:
			return StateWaitConnAck, ActionConnect, nil
		case EvConnectionAccepted:
			// Incoming connection from a peer we did not initiate.
			return StateWaitConnAckOpen, ActionNone, nil
		case EvStop, EvCleanup, EvConnectionFailure:
			return StateClosed, ActionNone, nil
		}

	case StateWaitConnAck:
		switch event {
		case EvConnectAccepted:
			return StateWaitICEA, ActionSendCER, nil
		case EvConnectRejected:
			return StateClosed, ActionNone, nil
		case EvConnectionAccepted:
			// Incoming connection while we were attempting to connect.
			return StateWaitConnAckOpen, ActionNone, nil
		case EvStop:
			return StateClosed, ActionDisconnect, nil
		case EvTimeout:
			return StateClosed, ActionDisconnect, nil
		case EvCleanup:
			return StateClosed, ActionCleanup, nil
		}

	case StateWaitICEA:
		switch event {
		case EvCEAReceived:
			return StateROpen, ActionNone, nil
		case EvCEANotAcceptable:
			return StateClosed, ActionDisconnect, nil
		case EvCEAUnknownPeer:
			return StateClosed, ActionDisconnect, nil
		case EvCERTimeout:
			return StateClosed, ActionDisconnect, nil
		case EvConnectionFailure:
			return StateClosed, ActionNone, nil
		case EvStop:
			return StateClosed, ActionDisconnect, nil
		case EvCleanup:
			return StateClosed, ActionCleanup, nil
		case EvSendMessage, EvProcessMessage:
			// Queue messages while we wait for the handshake.
			return StateWaitICEA, ActionDropMessage, nil
		}

	case StateWaitConnAckOpen:
		switch event {
		case EvConnectAccepted:
			// We accepted an incoming connection; the responder side will
			// receive the CER from the peer.
			return StateWaitConnAckOpen, ActionNone, nil
		case EvCERReceived:
			return StateROpen, ActionSendCEA, nil
		case EvConnectionFailure:
			return StateClosed, ActionNone, nil
		case EvStop:
			return StateClosed, ActionDisconnect, nil
		case EvTimeout:
			return StateClosed, ActionDisconnect, nil
		case EvCleanup:
			return StateClosed, ActionCleanup, nil
		}

	case StateROpen:
		switch event {
		case EvDWRReceived:
			return StateROpen, ActionSendDWA, nil
		case EvDPRReceived:
			return StateClosed, ActionSendDPA, nil
		case EvSendMessage, EvReceiveMessage, EvProcessMessage:
			p.lastActivity = time.Now()
			return StateROpen, ActionNone, nil
		case EvConnectionTimeout:
			return StateROpen, ActionSendDWR, nil
		case EvDWARetry:
			return StateROpen, ActionSendDWR, nil
		case EvDWRFinal:
			return StateClosed, ActionDisconnect, nil
		case EvConnectionFailure:
			return StateClosed, ActionNone, nil
		case EvStop:
			return StateClosing, ActionSendDPR, nil
		case EvCleanup:
			return StateClosed, ActionCleanup, nil
		case EvDWAReceived:
			// Watchdog reply resets the inactivity timer.
			p.lastActivity = time.Now()
			return StateROpen, ActionNone, nil
		}

	case StateIWaitCEA:
		// Aliased behaviour of WaitICEA on the I side; kept separate to
		// match the C enum.
		switch event {
		case EvCEAReceived:
			return StateROpen, ActionNone, nil
		case EvCEANotAcceptable, EvCEAUnknownPeer:
			return StateClosed, ActionDisconnect, nil
		case EvCERTimeout:
			return StateClosed, ActionDisconnect, nil
		case EvConnectionFailure:
			return StateClosed, ActionNone, nil
		case EvStop, EvCleanup:
			return StateClosed, ActionDisconnect, nil
		}

	case StateClosing:
		switch event {
		case EvDPAReceived:
			return StateClosed, ActionDisconnect, nil
		case EvDWRReceived:
			// A peer may send a DWR while we are closing; reply with DWA.
			return StateClosing, ActionSendDWA, nil
		case EvDPRReceived:
			// Both sides initiated a DPR simultaneously; reply with DPA.
			return StateClosed, ActionSendDPA, nil
		case EvTimeout:
			return StateClosed, ActionDisconnect, nil
		case EvConnectionFailure:
			return StateClosed, ActionNone, nil
		case EvSendMessage, EvProcessMessage:
			return StateClosing, ActionDropMessage, nil
		case EvCleanup:
			return StateClosed, ActionCleanup, nil
		}

	case StateWaitReturns:
		switch event {
		case EvTimeout:
			return StateClosed, ActionCleanup, nil
		case EvCleanup:
			return StateClosed, ActionCleanup, nil
		case EvConnectionFailure:
			return StateClosed, ActionNone, nil
		}

	case StateClosedIdle:
		switch event {
		case EvStart:
			return StateWaitConnAck, ActionConnect, nil
		case EvStop, EvCleanup:
			return StateClosedIdle, ActionNone, nil
		}
	}

	return from, ActionNone, ErrIllegalTransition
}

// ---------------------------------------------------------------------------
// Convenience helpers
// ---------------------------------------------------------------------------

// IsOpen reports whether the peer is in a state that allows Diameter
// messages to be exchanged (StateROpen).
func (p *Peer) IsOpen() bool {
	return p.State() == StateROpen
}

// IsClosed reports whether the peer is in one of the closed states.
func (p *Peer) IsClosed() bool {
	s := p.State()
	return s == StateClosed || s == StateClosedIdle
}

// IsConnecting reports whether the peer is in the process of establishing
// a connection (any state between StateClosed and StateROpen).
func (p *Peer) IsConnecting() bool {
	s := p.State()
	switch s {
	case StateWaitConnAck, StateWaitICEA, StateWaitConnAckOpen, StateIWaitCEA:
		return true
	}
	return false
}

// DWRRetries returns the current number of unanswered DWR retries.
func (p *Peer) DWRRetries() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dwrRetries
}

// LastActivity returns the timestamp of the last activity on this peer.
func (p *Peer) LastActivity() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastActivity
}

// ResetDWRRetries resets the watchdog retry counter. Useful when the
// transport layer confirms activity out-of-band (e.g. a non-watchdog
// message has been received).
func (p *Peer) ResetDWRRetries() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dwrRetries = 0
	p.lastActivity = time.Now()
}

// SetDPRCause stores the Disconnect-Cause to be sent in the next DPR.
func (p *Peer) SetDPRCause(cause uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pendingDPRCause = cause
}

// DPRCause returns the pending Disconnect-Cause, or 0 when none is set.
func (p *Peer) DPRCause() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pendingDPRCause
}
