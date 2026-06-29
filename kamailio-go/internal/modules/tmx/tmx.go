// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TMX module - TM extensions, matching Kamailio modules/tmx (tmx_mod.c).
 *
 * The TMX module exposes transaction-state introspection helpers that the
 * C module surfaces through pseudo-variables ($T_reply_code, $T_reply_reason,
 * $T_branch_idx, ...) and the t_is_*_route() script functions. These helpers
 * answer questions such as "am I running inside a failure route?" or "what
 * was the last reply code on the picked branch?".
 *
 * In the C module the answers come from the global route_type variable and
 * the per-message tm_ctx_t (branch_index) plus the current transaction cell
 * (t_gett()). Here we keep an equivalent route context on the TMXModule and
 * let callers (or tests) drive it through the Set* helpers, mirroring how
 * Kamailio's core sets route_type / tm_ctx before dispatching a route block.
 */

package tmx

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// RouteType mirrors Kamailio's route_type bitmap (core/route.h).
// C: REQUEST_ROUTE, FAILURE_ROUTE, BRANCH_ROUTE, ONREPLY_ROUTE, ...
type RouteType uint32

const (
	// RouteUndefined is the zero route type (no route context set).
	RouteUndefined RouteType = 0
	// RouteRequest matches C REQUEST_ROUTE (1 << 0).
	RouteRequest RouteType = 1 << 0
	// RouteFailure matches C FAILURE_ROUTE (1 << 1).
	RouteFailure RouteType = 1 << 1
	// RouteOnReply matches C ONREPLY_ROUTE (TM_ONREPLY_ROUTE | CORE_ONREPLY_ROUTE).
	RouteOnReply RouteType = 1 << 2
	// RouteBranch matches C BRANCH_ROUTE (1 << 3).
	RouteBranch RouteType = 1 << 3
	// RouteLocal matches C LOCAL_ROUTE (1 << 6).
	RouteLocal RouteType = 1 << 6
	// RouteBranchFailure matches C BRANCH_FAILURE_ROUTE (1 << 8).
	RouteBranchFailure RouteType = 1 << 8
)

// BranchUndefined mirrors C T_BR_UNDEFINED (-1), returned when no branch
// index is available for the current route context.
const BranchUndefined = -1

// TMXModule implements the TMX (TM extensions) module functionality.
// C: struct tm_binds _tmx_tmb + global route_type / tm_ctx_t state.
type TMXModule struct {
	mu          sync.RWMutex
	routeType   RouteType
	branchIndex int
	branchCount int
	status      int  // last UAS reply status (C: t->uas.status)
	isLocal     bool // C: T_IS_LOCAL_FLAG on the transaction
}

// NewTMXModule creates a new TMXModule with an empty route context.
func NewTMXModule() *TMXModule {
	return &TMXModule{
		branchIndex: BranchUndefined,
	}
}

// ---------------------------------------------------------------------------
// Route context setters (mirror Kamailio core setting route_type / tm_ctx)
// ---------------------------------------------------------------------------

// SetRouteType sets the current route type, mirroring Kamailio's
// set_route_type(). It is used by the core before dispatching a route block.
func (m *TMXModule) SetRouteType(rt RouteType) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routeType = rt
}

// RouteType returns the current route type.
func (m *TMXModule) RouteType() RouteType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routeType
}

// SetBranchIndex records the active branch index, mirroring the C
// tm_ctx_t.branch_index field set by the TM core before entering a
// branch / onreply route.
func (m *TMXModule) SetBranchIndex(idx int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.branchIndex = idx
}

// SetBranchCount records the total number of outgoing branches, mirroring
// C cell.nr_of_outgoings.
func (m *TMXModule) SetBranchCount(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.branchCount = n
}

// SetStatus records the last UAS reply status code, mirroring C
// cell.uas.status.
func (m *TMXModule) SetStatus(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = code
}

// SetLocal records whether the current transaction is locally generated,
// mirroring the C T_IS_LOCAL_FLAG transaction flag.
func (m *TMXModule) SetLocal(v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isLocal = v
}

// ---------------------------------------------------------------------------
// Route-type predicates (C: t_is_failure_route / t_is_branch_route / ...)
// ---------------------------------------------------------------------------

// TIsFailureRoute returns true if the current route is a failure route.
// C: t_is_failure_route() -> route_type == FAILURE_ROUTE.
func (m *TMXModule) TIsFailureRoute() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routeType == RouteFailure
}

// TIsBranchRoute returns true if the current route is a branch route.
// C: t_is_branch_route() -> route_type == BRANCH_ROUTE.
func (m *TMXModule) TIsBranchRoute() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routeType == RouteBranch
}

// TIsRequestRoute returns true if the current route is a request route.
// C: t_is_request_route() -> route_type == REQUEST_ROUTE.
func (m *TMXModule) TIsRequestRoute() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routeType == RouteRequest
}

// TIsReplyRoute returns true if the current route is an onreply route.
// C: t_is_reply_route() -> route_type & ONREPLY_ROUTE.
func (m *TMXModule) TIsReplyRoute() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.routeType&RouteOnReply != 0
}

// ---------------------------------------------------------------------------
// Transaction introspection (C: $T_reply_code, $T_reply_reason, ...)
// ---------------------------------------------------------------------------

// TReplyCode returns the reply code associated with the current transaction
// context. In an onreply route the code is taken from the reply's status
// line (C: msg->first_line.u.reply.statuscode); otherwise the last UAS
// status recorded on the transaction is returned (C: t->uas.status).
// C: pv_get_tm_reply_code().
func (m *TMXModule) TReplyCode(msg *parser.SIPMsg) int {
	m.mu.RLock()
	rt := m.routeType
	status := m.status
	m.mu.RUnlock()

	// Onreply routes use the reply's own status code.
	if rt&RouteOnReply != 0 && msg != nil && msg.IsReply() {
		return int(msg.StatusCode())
	}
	if msg != nil && msg.IsReply() {
		// A reply outside an explicit onreply route still carries its code.
		return int(msg.StatusCode())
	}
	return status
}

// TReplyReason returns the reply reason phrase for the current transaction
// context. In an onreply route the reason is taken from the reply's status
// line; otherwise the empty string is returned (the C module returns the
// stored reply reason from the picked branch, which we do not track).
// C: pv_get_tm_reply_reason().
func (m *TMXModule) TReplyReason(msg *parser.SIPMsg) string {
	if msg == nil || !msg.IsReply() {
		return ""
	}
	if msg.FirstLine == nil || msg.FirstLine.Reply == nil {
		return ""
	}
	return msg.FirstLine.Reply.Reason.String()
}

// TRequestMethod returns the method of the transaction's request, taken
// from the request line of msg when it is a request. Returns the empty
// string for replies or nil messages.
// C: $T_req_method / pv_get_tm_request_method().
func (m *TMXModule) TRequestMethod(msg *parser.SIPMsg) string {
	if msg == nil || !msg.IsRequest() {
		return ""
	}
	if msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	return msg.FirstLine.Req.Method.String()
}

// TBranchIndex returns the active branch index, mirroring C
// tmx_get_branch_idx(). Returns BranchUndefined when no branch index is
// available (e.g. outside a branch / failure / onreply route).
func (m *TMXModule) TBranchIndex() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.branchIndex
}

// TBranchCount returns the total number of outgoing branches, mirroring C
// cell.nr_of_outgoings.
func (m *TMXModule) TBranchCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.branchCount
}

// TIsLocal returns true if the current transaction was locally generated
// (C: T_IS_LOCAL_FLAG). When msg is non-nil and carries no transaction
// context, the recorded flag is returned.
// C: $T_req_local / T_IS_LOCAL_FLAG.
func (m *TMXModule) TIsLocal(msg *parser.SIPMsg) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isLocal
}

// TGetStatus returns the last UAS reply status code recorded on the
// transaction, mirroring C cell.uas.status.
func (m *TMXModule) TGetStatus() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu   sync.Mutex
	defaultTMX  *TMXModule
	defaultOnce sync.Once
)

// DefaultTMX returns the package-level default TMXModule instance.
func DefaultTMX() *TMXModule {
	defaultOnce.Do(func() {
		defaultTMX = NewTMXModule()
	})
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTMX == nil {
		defaultTMX = NewTMXModule()
	}
	return defaultTMX
}

// Init initialises the TMX module (resets the default instance and route
// context). Mirrors C mod_init().
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTMX = NewTMXModule()
	return nil
}

// routeTypeName maps a RouteType to its canonical Kamailio name for the
// debug helpers below.
func routeTypeName(rt RouteType) string {
	switch rt {
	case RouteRequest:
		return "REQUEST_ROUTE"
	case RouteFailure:
		return "FAILURE_ROUTE"
	case RouteBranch:
		return "BRANCH_ROUTE"
	case RouteOnReply:
		return "ONREPLY_ROUTE"
	case RouteLocal:
		return "LOCAL_ROUTE"
	case RouteBranchFailure:
		return "BRANCH_FAILURE_ROUTE"
	default:
		return "UNDEFINED"
	}
}

// RouteTypeName returns the canonical Kamailio name of the current route
// type, or "UNDEFINED" when no route context is set. Useful for logging.
func (m *TMXModule) RouteTypeName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return routeTypeName(m.routeType)
}

// String returns a debug representation of the current TMX route context.
func (m *TMXModule) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var sb strings.Builder
	sb.WriteString("tmx{route=")
	sb.WriteString(routeTypeName(m.routeType))
	sb.WriteString(", branch=")
	if m.branchIndex == BranchUndefined {
		sb.WriteString("undef")
	} else {
		// branchIndex is small; format manually to avoid pulling fmt.
		sb.WriteString(itoa(m.branchIndex))
	}
	sb.WriteString(", count=")
	sb.WriteString(itoa(m.branchCount))
	sb.WriteString(", status=")
	sb.WriteString(itoa(m.status))
	sb.WriteString(", local=")
	if m.isLocal {
		sb.WriteString("true")
	} else {
		sb.WriteString("false")
	}
	sb.WriteString("}")
	return sb.String()
}

// itoa formats a non-negative int without importing fmt (kept local to
// avoid pulling fmt into the hot path).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
