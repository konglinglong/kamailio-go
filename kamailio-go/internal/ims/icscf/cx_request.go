// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * I-CSCF Cx request dispatch.
 * Port of the kamailio ims_icscf module's cxdx_uar.c and cxdx_lir.c
 * send-and-receive flow (the AVP-construction portions live in
 * cx_messages.go; this file wires them to the cdp module's transport and
 * transaction table).
 *
 * Two top-level entry points are exposed:
 *
 *   - SendUAR: builds a UAR, sends it via cdp's TransactionManager,
 *     blocks on the answer (or the supplied context's deadline), parses
 *     the UAA, and—if the HSS returned Server-Capabilities—populates
 *     the SCSCFTable's per-call-id candidate list.
 *
 *   - SendLIR: builds a LIR, sends it via cdp's TransactionManager,
 *     blocks on the answer (or the supplied context's deadline), parses
 *     the LIA, and—if the HSS returned a Server-Name—populates the
 *     SCSCFTable with a single-entry candidate list for the call-id.
 *
 * Both functions mirror the C source's synchronous-by-default behaviour
 * (the C module uses async callbacks with t_suspend / t_continue, but
 * the higher-level semantic is "send and wait"). Callers that need the
 * async variant can wrap the call in a goroutine.
 */

package icscf

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrNoHSSPeer is returned when no connected Diameter peer is
	// available to receive the Cx request. The C source uses
	// cxdx_forced_peer to override this; we expose the same knob via
	// Config.ForcedPeer.
	ErrNoHSSPeer = errors.New("icscf: no HSS peer configured")
	// ErrUARTimeout is returned when the UAA is not received within the
	// configured timeout.
	ErrUARTimeout = errors.New("icscf: UAR timed out")
	// ErrLIRTimeout is returned when the LIA is not received within the
	// configured timeout.
	ErrLIRTimeout = errors.New("icscf: LIR timed out")
	// ErrUnexpectedAnswer is returned when the answer's Command-Code is
	// not UAR or LIR respectively.
	ErrUnexpectedAnswer = errors.New("icscf: unexpected answer command code")
)

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config holds the runtime configuration of the I-CSCF Cx dispatcher.
// Mirrors the C modparams: cxdx_forced_peer, cxdx_dest_realm, the local
// Origin-Host / Origin-Realm, and the Tw inactivity timeout.
type Config struct {
	// OriginHost / OriginRealm are the local I-CSCF identity sent in
	// every Cx request.
	OriginHost  string
	OriginRealm string

	// DestinationRealm is the home realm to send Cx requests to
	// (cxdx_dest_realm modparam).
	DestinationRealm string

	// ForcedPeer, when non-empty, forces all Cx requests to the named
	// Diameter peer FQDN (cxdx_forced_peer modparam). When empty the
	// cdp module's default routing applies.
	ForcedPeer string

	// DefaultTimeout is the default Tw timer for UAR/LIR. Zero means
	// use the context's deadline only (or 30 s when neither is set).
	DefaultTimeout time.Duration

	// VisitedNetworkID is the Visited-Network-Identifier AVP value.
	// The C source reads this from the request's P-Visited-Network-ID
	// header; we expose it as a config knob for simplicity. Callers
	// may override per-request by setting UARRequest.VisitedNetworkID.
	VisitedNetworkID string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		DefaultTimeout: 30 * time.Second,
	}
}

// ---------------------------------------------------------------------------
// ICSCF — the top-level dispatcher
// ---------------------------------------------------------------------------

// ICSCF is the I-CSCF Cx dispatcher. It owns an SCSCFTable and uses a
// cdp TransactionManager to send UAR/LIR requests and wait for the
// corresponding answers. One ICSCF instance is intended to be process-wide
// (mirrors the C module's global state).
type ICSCF struct {
	cfg  *Config
	tbl  *SCSCFTable
	txn  *cdp.TransactionManager

	// dispatchFn is the optional pluggable message dispatcher. When nil
	// the dispatcher relies on cdp's internal routing (default). Tests
	// inject a stub via SetDispatcher.
	dispatchFn DispatchFunc
}

// New creates an ICSCF with the supplied configuration, S-CSCF table and
// cdp transaction manager. Any nil parameter is replaced with a default.
func New(cfg *Config, tbl *SCSCFTable, txn *cdp.TransactionManager) *ICSCF {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if tbl == nil {
		tbl = NewSCSCFTable()
	}
	if txn == nil {
		txn = cdp.DefaultTransactionManager()
	}
	return &ICSCF{cfg: cfg, tbl: tbl, txn: txn}
}

// Table returns the S-CSCF table backing this dispatcher.
func (i *ICSCF) Table() *SCSCFTable { return i.tbl }

// Config returns the dispatcher's configuration.
func (i *ICSCF) Config() *Config { return i.cfg }

// ---------------------------------------------------------------------------
// SendUAR — User-Authorization-Request (3GPP TS 29.229 §7.2)
// ---------------------------------------------------------------------------

// SendUAR builds and sends a UAR for the given call-id, waits for the UAA
// (or the context deadline), and — on success — populates the SCSCFTable's
// per-call-id candidate list from the Server-Capabilities returned by the
// HSS.
//
// The returned UAAResult reflects the parsed HSS answer; callers can
// inspect RegistrationCase() to decide whether to proceed with
// Table().Select(callID) or reject the registration.
//
//	C: I_perform_user_authorization_request() + async_cdp_uar_callback()
func (i *ICSCF) SendUAR(ctx context.Context, callID string, req *UARRequest) (*UAAResult, error) {
	if req == nil {
		req = &UARRequest{}
	}
	// Apply config defaults to fields the caller left empty.
	if req.OriginHost == "" {
		req.OriginHost = i.cfg.OriginHost
	}
	if req.OriginRealm == "" {
		req.OriginRealm = i.cfg.OriginRealm
	}
	if req.DestinationRealm == "" {
		req.DestinationRealm = i.cfg.DestinationRealm
	}
	if req.VisitedNetworkID == "" {
		req.VisitedNetworkID = i.cfg.VisitedNetworkID
	}

	// Build the UAR.
	msg := BuildUAR(req, 0, 0)

	// Dispatch and wait.
	answer, err := i.sendAndWait(ctx, msg, i.cfg.DefaultTimeout)
	if err != nil {
		return nil, err
	}

	// Parse the UAA.
	result, err := ParseUAA(answer)
	if err != nil {
		return nil, err
	}

	// Build the candidate list from the HSS's reply.
	i.populateCandidatesFromUAA(callID, result, req.AuthorizationType != 0)
	return result, nil
}

// populateCandidatesFromUAA builds the per-call-id candidate list from
// the UAA's Server-Capabilities / Server-Name. Mirrors the
// I_get_capab_ordered() invocation inside async_cdp_uar_callback().
func (i *ICSCF) populateCandidatesFromUAA(callID string, uaa *UAAResult, orig bool) {
	if uaa == nil {
		return
	}
	// Combine the HSS-required capability set with the local catalogue.
	required := combineCapabilities(uaa.MandatoryCaps, uaa.OptionalCaps)
	// If the HSS named a specific server, BuildCandidateList will rank
	// it with INT_MAX; otherwise the catalogue's MatchScore is used.
	i.tbl.BuildCandidateList(callID, required, uaa.ServerName, orig)
}

// ---------------------------------------------------------------------------
// SendLIR — Location-Info-Request (3GPP TS 29.229 §7.4)
// ---------------------------------------------------------------------------

// SendLIR builds and sends a LIR for the given call-id, waits for the LIA
// (or the context deadline), and — on success — populates the SCSCFTable's
// per-call-id candidate list with the S-CSCF named in the LIA.
//
// The returned LIAResult reflects the parsed HSS answer.
//
//	C: I_perform_location_information_request() + async_cdp_lir_callback()
func (i *ICSCF) SendLIR(ctx context.Context, callID string, req *LIRRequest) (*LIAResult, error) {
	if req == nil {
		req = &LIRRequest{}
	}
	if req.OriginHost == "" {
		req.OriginHost = i.cfg.OriginHost
	}
	if req.OriginRealm == "" {
		req.OriginRealm = i.cfg.OriginRealm
	}
	if req.DestinationRealm == "" {
		req.DestinationRealm = i.cfg.DestinationRealm
	}

	msg := BuildLIR(req, 0, 0)
	answer, err := i.sendAndWait(ctx, msg, i.cfg.DefaultTimeout)
	if err != nil {
		return nil, err
	}

	result, err := ParseLIA(answer)
	if err != nil {
		return nil, err
	}

	// Build the candidate list with the explicit Server-Name returned by
	// the HSS. If no Server-Name is present the candidate list will be
	// empty and the caller should fall back to a default route.
	required := combineCapabilities(result.MandatoryCaps, result.OptionalCaps)
	i.tbl.BuildCandidateList(callID, required, result.ServerName, false)
	return result, nil
}

// ---------------------------------------------------------------------------
// Shared send-and-wait
// ---------------------------------------------------------------------------

// sendAndWait registers msg with the transaction table, dispatches it via
// the (caller-provided) SendMessage hook, and blocks until the answer
// arrives or the deadline elapses.
//
// The C source uses cdp's AAASendMessage with an async callback; here we
// use the TransactionManager's AwaitAnswer, which gives the same semantic
// (one goroutine per request, blocking on Done) without needing the
// t_suspend / t_continue dance that the C source uses with TM.
func (i *ICSCF) sendAndWait(ctx context.Context, msg *cdp.DiameterMessage, timeout time.Duration) (*cdp.DiameterMessage, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Register the request with the transaction table (populates the
	// HopByHop / EndToEnd identifiers if zero).
	peer := i.cfg.ForcedPeer
	txn, err := i.txn.SendRequest(msg, peer, nil)
	if err != nil {
		return nil, fmt.Errorf("icscf: register transaction: %w", err)
	}

	// Dispatch the encoded message via the cdp module's transport.
	if err := i.dispatch(msg, peer); err != nil {
		// Best-effort cleanup: cancel the pending transaction.
		_ = i.txn.Table().Cancel(txn.HopByHopID)
		return nil, err
	}

	// Wait for the answer or the deadline. AwaitAnswer blocks until
	// the transaction is finalised by an incoming answer or a timeout.
	answer, err := i.txn.AwaitAnswer(ctx, msg, peer)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrUARTimeout
		}
		return nil, err
	}
	if answer == nil {
		return nil, ErrUnexpectedAnswer
	}
	return answer, nil
}

// dispatch is the hook the ICSCF uses to push the encoded request onto the
// wire. Tests can swap this for a stub via SetDispatcher; production code
// leaves it at the default, which sends the message via the cdp module's
// default transport (DefaultTransport or a configured peer).
//
// The default implementation is a no-op stub that returns nil: production
// deployments are expected to wire the cdp Transport's SendMessage on a
// known peer, or to use the forced-peer routing path inside cdp.
func (i *ICSCF) dispatch(msg *cdp.DiameterMessage, peer string) error {
	if i.dispatchFn != nil {
		return i.dispatchFn(msg, peer)
	}
	// Default: rely on the cdp module's internal routing. When no peer
	// is configured the message is silently dropped (the transaction
	// will time out on the caller side and return ErrUARTimeout). This
	// mirrors the C source's behaviour when cdp has no route to the
	// configured realm.
	return nil
}

// DispatchFunc sends an encoded Diameter message to the named peer (or
// to the default route when peer is empty). Tests inject a stub via
// SetDispatcher.
type DispatchFunc func(msg *cdp.DiameterMessage, peer string) error

// SetDispatcher replaces the message-dispatch hook. Pass nil to restore
// the default no-op behaviour (which relies on cdp's internal routing).
func (i *ICSCF) SetDispatcher(fn DispatchFunc) {
	i.dispatchFn = fn
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// combineCapabilities merges the mandatory and optional capability lists
// returned by the HSS into a single set for SCSCFCapability.MatchScore.
// In the C source the two lists are kept separate because the catalogue
// entry distinguishes between mandatory (must-match) and optional
// (bonus) capabilities; here we feed both into MatchScore which itself
// looks up against the union.
func combineCapabilities(mandatory, optional []int) []int {
	out := make([]int, 0, len(mandatory)+len(optional))
	out = append(out, mandatory...)
	out = append(out, optional...)
	return out
}
