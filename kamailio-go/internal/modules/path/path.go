// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Path module - Path header handling for intermediate proxies.
 * Port of the kamailio path module (src/modules/path).
 *
 * The Path header (RFC 3327) is used by intermediate proxies in front of
 * a registrar to record the path a REGISTER took, so that subsequent
 * requests can be routed back along the same path. This module provides
 * functions for inserting a Path header (optionally carrying a
 * "received" parameter that records the source address of the
 * registration) and for evaluating Path headers on subsequent requests.
 */

package path

import (
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// DefaultReceivedName is the default name of the received parameter,
// matching the C path_received_name = str_init("received").
const DefaultReceivedName = "received"

// PathModule implements Path header processing.
//
// It is the Go counterpart of the kamailio path module. It is safe for
// concurrent use: the configuration is read-only after New and the
// process-wide singleton is guarded by a mutex.
type PathModule struct {
	mu sync.RWMutex

	// UseReceived mirrors path_use_received: when set, the received-param
	// of the current Route uri is used as the destination URI.
	UseReceived bool
	// ReceivedFormat mirrors path_received_format (0 = IP, 1 = IP:port).
	ReceivedFormat int
	// EnableR2 mirrors path_enable_r2: insert a ;r2=on parameter.
	EnableR2 bool
	// SocknameMode mirrors path_sockname_mode.
	SocknameMode int
	// ReceivedName mirrors path_received_name (default "received").
	ReceivedName string
}

// New creates a PathModule configured with the C module defaults.
func New() *PathModule {
	return &PathModule{ReceivedName: DefaultReceivedName}
}

// AddPathHeader adds a Path header carrying the supplied URI to msg.
// The URI is wrapped in angle brackets when it is not already. Returns
// 0 on success or -1 when msg is nil or the URI is empty.
//
//	C: add_path() / prepend_path()
func (m *PathModule) AddPathHeader(msg *parser.SIPMsg, uri string) int {
	if msg == nil || uri == "" {
		return -1
	}
	value := strings.TrimSpace(uri)
	if !strings.HasPrefix(value, "<") {
		value = "<" + value + ">"
	}
	hdr := msg.AddHeader("Path", value)
	if msg.Path == nil {
		msg.Path = hdr
	}
	return 0
}

// AddPathReceived adds a Path header carrying a "received" parameter
// derived from the top Via header of msg. The received value is taken
// from the Via "received" parameter when present, otherwise from the
// Via sent-by host. Returns 0 on success or -1 when msg is nil.
//
//	C: add_path_received() / prepend_path(... PATH_PARAM_RECEIVED ...)
func (m *PathModule) AddPathReceived(msg *parser.SIPMsg) int {
	if msg == nil {
		return -1
	}
	received := m.extractReceived(msg)
	name := m.receivedParam()
	value := "<sip:proxy@localhost>;" + name + "=" + received
	hdr := msg.AddHeader("Path", value)
	if msg.Path == nil {
		msg.Path = hdr
	}
	return 0
}

// ProcessPath extracts and returns the URI of every Path header present
// in msg, in the order they appear. Returns an empty (non-nil) slice
// when there are no Path headers or msg is nil.
//
//	C: path_rr_callback() / use_received evaluation
func (m *PathModule) ProcessPath(msg *parser.SIPMsg) []string {
	uris := []string{}
	if msg == nil {
		return uris
	}
	for _, h := range msg.GetAllHeadersByType(parser.HdrPath) {
		rrb, err := parser.ParseRoute(h.Body)
		if err != nil || rrb == nil {
			// Fall back to the raw body when the header cannot be parsed
			// as a route list (keeps ProcessPath lossless).
			uris = append(uris, strings.TrimSpace(h.Body.String()))
			continue
		}
		for r := rrb.FirstURL; r != nil; r = r.Next {
			uris = append(uris, r.String())
		}
	}
	return uris
}

// HandlePath reports whether msg carries at least one Path header.
//
//	C: presence check used before processing Path headers
func (m *PathModule) HandlePath(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	return msg.GetHeaderByType(parser.HdrPath) != nil
}

// SetPathParams appends the given parameters (e.g. "ftag=abc;r2=on")
// to the last Path header of msg. Returns 0 on success or -1 when
// there is no Path header, the params are empty, or msg is nil.
//
//	C: add_rr_param() analogue for Path headers
func (m *PathModule) SetPathParams(msg *parser.SIPMsg, params string) int {
	if msg == nil || params == "" {
		return -1
	}
	paths := msg.GetAllHeadersByType(parser.HdrPath)
	if len(paths) == 0 {
		return -1
	}
	last := paths[len(paths)-1]
	body := last.Body.String()
	if body == "" {
		last.Body = str.Mk(params)
		return 0
	}
	last.Body = str.Mk(body + ";" + params)
	return 0
}

// extractReceived returns the received value to record for msg: the
// Via "received" parameter when present, otherwise the Via sent-by host,
// otherwise an empty string.
func (m *PathModule) extractReceived(msg *parser.SIPMsg) string {
	via := msg.HdrVia1
	if via == nil {
		via = msg.GetHeaderByType(parser.HdrVia)
	}
	if via == nil {
		return ""
	}
	vb, err := parser.ParseVia(via.Body)
	if err != nil || vb == nil {
		return ""
	}
	if vb.Received != nil && vb.Received.Value.Len > 0 {
		return vb.Received.Value.String()
	}
	if vb.Host.Len > 0 {
		return vb.Host.String()
	}
	return ""
}

// receivedParam returns the configured received parameter name.
func (m *PathModule) receivedParam() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.ReceivedName == "" {
		return DefaultReceivedName
	}
	return m.ReceivedName
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultPM *PathModule
)

// DefaultPath returns the process-wide PathModule, creating it on first use.
func DefaultPath() *PathModule {
	defaultMu.RLock()
	m := defaultPM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPM == nil {
		defaultPM = New()
	}
	return defaultPM
}

// Init (re)initialises the process-wide PathModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPM = New()
}

// AddPathHeader is the package-level wrapper around DefaultPath().AddPathHeader.
func AddPathHeader(msg *parser.SIPMsg, uri string) int {
	return DefaultPath().AddPathHeader(msg, uri)
}

// AddPathReceived is the package-level wrapper around DefaultPath().AddPathReceived.
func AddPathReceived(msg *parser.SIPMsg) int {
	return DefaultPath().AddPathReceived(msg)
}

// ProcessPath is the package-level wrapper around DefaultPath().ProcessPath.
func ProcessPath(msg *parser.SIPMsg) []string {
	return DefaultPath().ProcessPath(msg)
}

// HandlePath is the package-level wrapper around DefaultPath().HandlePath.
func HandlePath(msg *parser.SIPMsg) bool {
	return DefaultPath().HandlePath(msg)
}

// SetPathParams is the package-level wrapper around DefaultPath().SetPathParams.
func SetPathParams(msg *parser.SIPMsg, params string) int {
	return DefaultPath().SetPathParams(msg, params)
}
