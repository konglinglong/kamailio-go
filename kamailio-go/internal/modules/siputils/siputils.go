// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SIP utilities module - matching Kamailio modules/siputils.
 *
 * Provides helpers for inspecting SIP messages and URIs: To-tag detection,
 * URI scheme/user/host/parameter extraction, E.164 validation, URI and
 * Call-ID comparison, first-hop detection and "is this host mine" checks.
 */

package siputils

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// SipUtilsModule implements the siputils module functionality.
// C: struct module siputils
type SipUtilsModule struct {
	mu    sync.RWMutex
	hosts map[string]bool // lower-cased host names we consider "ours"
}

// NewSipUtilsModule creates a new SipUtilsModule pre-populated with the
// loopback host names and the machine's hostname.
func NewSipUtilsModule() *SipUtilsModule {
	s := &SipUtilsModule{hosts: make(map[string]bool)}
	s.AddMyHost("localhost")
	s.AddMyHost("127.0.0.1")
	s.AddMyHost("::1")
	if hn, err := os.Hostname(); err == nil && hn != "" {
		s.AddMyHost(hn)
	}
	return s
}

// AddMyHost registers an additional host name that IsMyHost / IsMyURI will
// treat as belonging to us. Matching is case-insensitive.
func (s *SipUtilsModule) AddMyHost(host string) {
	if s == nil {
		return
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host] = true
}

// HasToTag returns true if the To header of msg carries a tag parameter.
// C: has_totag()
func (s *SipUtilsModule) HasToTag(msg *parser.SIPMsg) bool {
	if msg == nil || msg.To == nil {
		return false
	}
	tb, err := parser.ParseToBody(msg.To.Body)
	if err != nil || tb == nil {
		return false
	}
	return tb.HasTag()
}

// HasSipUri returns true if uri uses the sip: or sips: scheme.
// C: is_uri() / is_sip()
func (s *SipUtilsModule) HasSipUri(uri string) bool {
	u, err := parser.ParseURI(uri)
	if err != nil {
		return false
	}
	return u.Type == parser.SIPURIT || u.Type == parser.SIPSURIT
}

// HasTelUri returns true if uri uses the tel: or tels: scheme.
// C: is_tel_number() variant
func (s *SipUtilsModule) HasTelUri(uri string) bool {
	u, err := parser.ParseURI(uri)
	if err != nil {
		return false
	}
	return u.Type == parser.TELURIT || u.Type == parser.TELSURIT
}

// IsUriUserE164 returns true if the user part of uri is a valid E.164 number:
// an optional leading '+' followed by 1 to 15 digits.
// C: is_uri_user_e164()
func (s *SipUtilsModule) IsUriUserE164(uri string) bool {
	user := s.GetUriUser(uri)
	if user == "" {
		return false
	}
	digits := user
	if digits[0] == '+' {
		digits = digits[1:]
	}
	if len(digits) == 0 || len(digits) > 15 {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// IsFirstHop returns true if msg is a request with no Route headers, i.e. the
// request is destined directly to us (we are the first hop).
// C: is_first_hop()
func (s *SipUtilsModule) IsFirstHop(msg *parser.SIPMsg) bool {
	if msg == nil || !msg.IsRequest() {
		return false
	}
	return len(msg.GetAllHeadersByType(parser.HdrRoute)) == 0
}

// IsMyHost returns true if host is one of the host names registered as ours.
// Matching is case-insensitive.
// C: is_my_host() / check_self()
func (s *SipUtilsModule) IsMyHost(host string) bool {
	if s == nil || host == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hosts[strings.ToLower(strings.TrimSpace(host))]
}

// IsMyURI returns true if the host part of uri is one of ours.
// C: is_my_uri() / check_self()
func (s *SipUtilsModule) IsMyURI(uri string) bool {
	u, err := parser.ParseURI(uri)
	if err != nil {
		return false
	}
	return s.IsMyHost(u.Host.String())
}

// GetMsgID returns a message identifier built from the Call-ID and the CSeq
// number. Two messages belonging to the same dialog leg share the same id.
// C: get_msg_id() / ki_get_msg_id()
func (s *SipUtilsModule) GetMsgID(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	callID := ""
	if msg.CallID != nil {
		callID = strings.TrimSpace(msg.CallID.Body.String())
	}
	cseq := ""
	if msg.CSeq != nil {
		if cb, err := parser.ParseCSeq(msg.CSeq.Body); err == nil && cb != nil {
			cseq = strconv.FormatUint(uint64(cb.Number), 10)
		}
	}
	return callID + "-" + cseq
}

// CompareURI compares two SIP URIs for equality. The user part is compared
// case-sensitively, the host case-insensitively, and the port numerically.
// C: cmp_uri()
func (s *SipUtilsModule) CompareURI(uri1, uri2 string) bool {
	u1, err1 := parser.ParseURI(uri1)
	u2, err2 := parser.ParseURI(uri2)
	if err1 != nil || err2 != nil {
		// Fall back to a plain string comparison for unparseable URIs.
		return uri1 == uri2
	}
	if u1.Type != u2.Type {
		return false
	}
	if u1.User.String() != u2.User.String() {
		return false
	}
	if !strings.EqualFold(u1.Host.String(), u2.Host.String()) {
		return false
	}
	if u1.PortNo != u2.PortNo {
		return false
	}
	return true
}

// CompareCallID returns true if msg1 and msg2 carry the same Call-ID value.
// C: cmp_callid() / ki_cmp_callid()
func (s *SipUtilsModule) CompareCallID(msg1, msg2 *parser.SIPMsg) bool {
	if msg1 == nil || msg2 == nil {
		return false
	}
	var c1, c2 string
	if msg1.CallID != nil {
		c1 = strings.TrimSpace(msg1.CallID.Body.String())
	}
	if msg2.CallID != nil {
		c2 = strings.TrimSpace(msg2.CallID.Body.String())
	}
	return c1 != "" && c1 == c2
}

// GetUriUser returns the user part of uri, or the empty string if absent.
// C: get_uri_user() (via parse_uri)
func (s *SipUtilsModule) GetUriUser(uri string) string {
	u, err := parser.ParseURI(uri)
	if err != nil {
		return ""
	}
	return u.User.String()
}

// GetUriHost returns the host part of uri, or the empty string if absent.
// C: get_uri_host() (via parse_uri)
func (s *SipUtilsModule) GetUriHost(uri string) string {
	u, err := parser.ParseURI(uri)
	if err != nil {
		return ""
	}
	return u.Host.String()
}

// GetUriParam returns the value of the named URI parameter, or the empty
// string if the parameter is absent (or is a value-less flag).
// C: get_uri_param()
func (s *SipUtilsModule) GetUriParam(uri, paramName string) string {
	if paramName == "" {
		return ""
	}
	u, err := parser.ParseURI(uri)
	if err != nil || u.Params.Len == 0 {
		return ""
	}
	for _, p := range strings.Split(u.Params.String(), ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if strings.EqualFold(kv[0], paramName) {
			if len(kv) == 2 {
				return kv[1]
			}
			return ""
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Package-level default instance and global functions
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.Mutex
	defaultSipUtils = NewSipUtilsModule()
)

// DefaultSipUtils returns the package-level default SipUtilsModule instance.
func DefaultSipUtils() *SipUtilsModule {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipUtils == nil {
		defaultSipUtils = NewSipUtilsModule()
	}
	return defaultSipUtils
}

// Init initialises the siputils module (resets the default instance).
// C: mod_init()
func Init() error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipUtils = NewSipUtilsModule()
	return nil
}

// HasToTag is the package-level wrapper around the default instance.
func HasToTag(msg *parser.SIPMsg) bool {
	return DefaultSipUtils().HasToTag(msg)
}

// IsMyHost is the package-level wrapper around the default instance.
func IsMyHost(host string) bool {
	return DefaultSipUtils().IsMyHost(host)
}
