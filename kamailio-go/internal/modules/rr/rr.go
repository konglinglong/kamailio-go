// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Record-Route (rr) module - Go equivalent of Kamailio's C rr module.
 *
 * Responsibilities (RFC 3261 Section 20.30 / 16):
 *   - Insert Record-Route headers so the proxy stays in the signalling path
 *   - Detect loose routing (Route header carrying the "lr" parameter)
 *   - Remove Record-Route headers from a message
 *   - Append parameters (e.g. ftag) to an existing Record-Route header
 *   - Check for the presence of a given route parameter
 *   - Determine the direction of a in-dialog request (upstream/downstream)
 *
 * The C module lives in src/modules/rr/{rr_mod.c,record.c,loose.c,rr_cb.c}.
 */

package rr

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// ---------------------------------------------------------------------------
// Parameters
// ---------------------------------------------------------------------------

// RecordRouteParams describes how a Record-Route header is built.
// It mirrors the options exposed by the C record_route() wrapper plus the
// module parameters (add_username, enable_full_lr, ...).
type RecordRouteParams struct {
	// Username is the user part of the Record-Route URI. Empty means no
	// user part (the default, matching add_username=0).
	Username string
	// Domain is the host part of the Record-Route URI. When empty it
	// defaults to "localhost".
	Domain string
	// Port is the port part (as a string). Empty means no port.
	Port string
	// Transport is the transport protocol ("udp", "tcp", "tls", ...).
	// When set it is emitted as a ;transport=<value> URI parameter.
	Transport string
	// LR is the loose-route flag. When true a ;lr parameter is appended,
	// advertising this proxy as a loose router (RFC 3261 §16.4).
	LR bool
	// CustomParams is an opaque string of extra parameters appended verbatim
	// (e.g. "ftag=abc;r2=on"). It is split on ';' by the parser.
	CustomParams string
}

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// RRStats holds the module counters. The fields are atomic so they can be
// updated concurrently with -race safety.
type RRStats struct {
	RecordRoutesAdded   atomic.Int64
	LooseRoutesDetected atomic.Int64
}

// ---------------------------------------------------------------------------
// Module
// ---------------------------------------------------------------------------

// RRModule is the Record-Route processor. It is safe for concurrent use:
// configuration is immutable after NewRRModule and the counters are atomic.
type RRModule struct {
	stats RRStats

	// Configuration mirrors the C module parameters (rr_mod.c).
	appendFromTag  bool // append_fromtag: append ;ftag on RR (default on)
	enableDoubleRR bool // enable_double_rr: insert two RR headers (default on)
	enableFullLR   bool // enable_full_lr: emit ;lr=on instead of ;lr
	addUsername    bool // add_username: force a user part (default off)
}

// NewRRModule returns a new RRModule configured with the C module defaults.
func NewRRModule() *RRModule {
	return &RRModule{
		appendFromTag:  true,
		enableDoubleRR: true,
		enableFullLR:   false,
		addUsername:     false,
	}
}

// buildRRValue renders the Record-Route header body from params.
//
// The URI parameters (lr, transport, custom) are emitted *after* the closing
// '>' as rr-params, matching the form used elsewhere in the codebase
// ("<sip:host>;lr") and what the parser's HasLR() helper detects.
func buildRRValue(p *RecordRouteParams) string {
	if p == nil {
		p = &RecordRouteParams{Domain: "localhost", LR: true}
	}

	var sb strings.Builder
	sb.WriteString("<sip:")
	if p.Username != "" {
		sb.WriteString(p.Username)
		sb.WriteByte('@')
	}
	domain := p.Domain
	if domain == "" {
		domain = "localhost"
	}
	sb.WriteString(domain)
	if p.Port != "" {
		sb.WriteByte(':')
		sb.WriteString(p.Port)
	}
	sb.WriteByte('>')

	var params []string
	if p.LR {
		params = append(params, "lr")
	}
	if p.Transport != "" {
		params = append(params, "transport="+p.Transport)
	}
	if p.CustomParams != "" {
		params = append(params, p.CustomParams)
	}
	if len(params) > 0 {
		sb.WriteByte(';')
		sb.WriteString(strings.Join(params, ";"))
	}
	return sb.String()
}

// RecordRoute adds a Record-Route header built from params to msg and
// returns the header value that was inserted.
//
//	C: w_record_route() -> record_route()
func (m *RRModule) RecordRoute(msg *parser.SIPMsg, params *RecordRouteParams) string {
	if msg == nil {
		return ""
	}
	if params == nil {
		params = &RecordRouteParams{Domain: "localhost", LR: true}
	}
	value := buildRRValue(params)
	hdr := msg.AddHeader("Record-Route", value)
	if msg.RecordRoute == nil {
		msg.RecordRoute = hdr
	}
	m.stats.RecordRoutesAdded.Add(1)
	return value
}

// RecordRoutePreset adds a Record-Route header using the supplied URI
// verbatim (the caller takes responsibility for the URI form). The URI is
// wrapped in angle brackets when it is not already. Returns the header value.
//
//	C: w_record_route_preset() -> record_route_preset()
func (m *RRModule) RecordRoutePreset(msg *parser.SIPMsg, uri string) string {
	if msg == nil || uri == "" {
		return ""
	}
	value := strings.TrimSpace(uri)
	if !strings.HasPrefix(value, "<") {
		value = "<" + value + ">"
	}
	hdr := msg.AddHeader("Record-Route", value)
	if msg.RecordRoute == nil {
		msg.RecordRoute = hdr
	}
	m.stats.RecordRoutesAdded.Add(1)
	return value
}

// LooseRoute inspects the first Route header and reports whether the request
// is loose-routed (the Route URI carries the "lr" parameter). It returns the
// reconstructed URI of the first Route header (or "" when there is none).
//
//	C: w_loose_route() -> loose_route()
func (m *RRModule) LooseRoute(msg *parser.SIPMsg) (bool, string) {
	if msg == nil {
		return false, ""
	}
	routeHdr := msg.GetHeaderByType(parser.HdrRoute)
	if routeHdr == nil {
		return false, ""
	}
	rrb, err := parser.ParseRoute(routeHdr.Body)
	if err != nil || rrb == nil || rrb.FirstURL == nil {
		return false, ""
	}
	first := rrb.FirstURL
	isLoose := first.HasLR()
	if isLoose {
		m.stats.LooseRoutesDetected.Add(1)
	}
	return isLoose, first.String()
}

// RemoveRecordRoute removes every Record-Route header from msg and returns
// the number of headers that were removed.
//
//	C: w_remove_record_route()
func (m *RRModule) RemoveRecordRoute(msg *parser.SIPMsg) int {
	if msg == nil {
		return 0
	}
	count := msg.CountHeadersByType(parser.HdrRecordRoute)
	if count > 0 {
		msg.RemoveHeadersByType(parser.HdrRecordRoute)
	}
	return count
}

// AddRRParam appends a parameter to the last Record-Route header of msg.
// Returns 0 on success or -1 when there is no Record-Route header to amend.
//
//	C: w_add_rr_param() -> add_rr_param()
func (m *RRModule) AddRRParam(msg *parser.SIPMsg, param string) int {
	if msg == nil || param == "" {
		return -1
	}
	rrs := msg.GetAllHeadersByType(parser.HdrRecordRoute)
	if len(rrs) == 0 {
		return -1
	}
	last := rrs[len(rrs)-1]
	body := last.Body.String()
	if body == "" {
		last.Body = str.Mk(param)
		return 0
	}
	last.Body = str.Mk(body + ";" + param)
	return 0
}

// CheckRouteParam reports whether the given parameter is present on any
// Route or Record-Route header. The param may be a bare name ("lr") or a
// name=value pair ("ftag=abc"); matching is case-insensitive.
//
//	C: w_check_route_param() -> check_route_param()
func (m *RRModule) CheckRouteParam(msg *parser.SIPMsg, param string) bool {
	if msg == nil || param == "" {
		return false
	}
	param = strings.TrimSpace(param)
	var wantName, wantValue string
	if eq := strings.Index(param, "="); eq != -1 {
		wantName = strings.TrimSpace(param[:eq])
		wantValue = strings.TrimSpace(param[eq+1:])
	} else {
		wantName = param
	}

	match := func(body str.Str) bool {
		rrb, err := parser.ParseRoute(body)
		if err != nil || rrb == nil {
			return false
		}
		for r := rrb.FirstURL; r != nil; r = r.Next {
			for p := r.ParamList; p != nil; p = p.Next {
				if !strings.EqualFold(p.Name.String(), wantName) {
					continue
				}
				if wantValue == "" {
					return true
				}
				if strings.EqualFold(p.Value.String(), wantValue) {
					return true
				}
			}
		}
		return false
	}

	for _, h := range msg.GetAllHeadersByType(parser.HdrRoute) {
		if match(h.Body) {
			return true
		}
	}
	for _, h := range msg.GetAllHeadersByType(parser.HdrRecordRoute) {
		if match(h.Body) {
			return true
		}
	}
	return false
}

// IsDirection reports whether an in-dialog request flows in the requested
// direction relative to the dialog-creating request. Direction is one of
// "upstream" or "downstream" and is derived by comparing the From tag of the
// current request with the "ftag" parameter stored on the Record-Route
// header (which records the From tag of the original request).
//
//	C: w_is_direction() -> is_direction()
func (m *RRModule) IsDirection(msg *parser.SIPMsg, direction string) bool {
	if msg == nil {
		return false
	}
	dir := strings.ToLower(strings.TrimSpace(direction))

	var fromTag string
	if msg.From != nil {
		if fb, err := parser.ParseFromBody(msg.From.Body); err == nil && fb != nil {
			fromTag = fb.GetTag()
		}
	}

	var ftag string
	if rr := msg.GetHeaderByType(parser.HdrRecordRoute); rr != nil {
		if rrb, err := parser.ParseRecordRoute(rr.Body); err == nil && rrb != nil {
			for r := rrb.FirstURL; r != nil; r = r.Next {
				for p := r.ParamList; p != nil; p = p.Next {
					if strings.EqualFold(p.Name.String(), "ftag") {
						ftag = p.Value.String()
						break
					}
				}
				if ftag != "" {
					break
				}
			}
		}
	}

	if ftag == "" {
		return false
	}

	switch dir {
	case "downstream":
		// Same direction as the dialog-creating request.
		return fromTag != "" && fromTag == ftag
	case "upstream":
		// Reverse direction: the From tag differs from the stored ftag.
		return fromTag != "" && fromTag != ftag
	default:
		return false
	}
}

// Stats returns a pointer to the module's statistics. The returned pointer
// aliases the internal counters, which are read atomically; callers must
// only read from it.
func (m *RRModule) Stats() *RRStats {
	return &m.stats
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultRR *RRModule
)

// DefaultRR returns the process-wide RRModule, creating it on first use.
func DefaultRR() *RRModule {
	defaultMu.RLock()
	m := defaultRR
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRR == nil {
		defaultRR = NewRRModule()
	}
	return defaultRR
}

// Init (re)initialises the process-wide RRModule to a fresh state, mirroring
// Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRR = NewRRModule()
}

// RecordRoute is the package-level wrapper around DefaultRR().RecordRoute.
func RecordRoute(msg *parser.SIPMsg, params *RecordRouteParams) string {
	return DefaultRR().RecordRoute(msg, params)
}

// LooseRoute is the package-level wrapper around DefaultRR().LooseRoute.
func LooseRoute(msg *parser.SIPMsg) (bool, string) {
	return DefaultRR().LooseRoute(msg)
}
