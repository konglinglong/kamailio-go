// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Sanity module - SIP message sanity checks.
 * Port of the kamailio sanity module (src/modules/sanity).
 *
 * The sanity module validates well-formedness of a SIP message before
 * further processing: request-URI scheme/version, Via presence and
 * branch, CSeq consistency, From/To/Contact URI parsability and
 * Content-Length correctness.
 */

package sanity

import (
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// SanityCheck is a bitmask selecting which sanity checks to run.
type SanityCheck int

const (
	CheckURIs          SanityCheck = 1
	CheckVia           SanityCheck = 2
	CheckCSeq          SanityCheck = 4
	CheckRURI          SanityCheck = 8
	CheckFrom          SanityCheck = 16
	CheckTo            SanityCheck = 32
	CheckContact       SanityCheck = 64
	CheckContentLength SanityCheck = 128
	// CheckAll selects every available check.
	CheckAll SanityCheck = 0xFFFF
)

// SanityResult holds the outcome of a sanity Check run.
type SanityResult struct {
	Passed       bool
	FailedChecks []string
	Warnings     []string
}

// SanityModule performs sanity checks on SIP messages.
type SanityModule struct{}

// New creates a SanityModule.
func New() *SanityModule {
	return &SanityModule{}
}

// Check runs the requested sanity checks against msg and returns a result.
// CheckURIs is a shorthand that expands to the RURI, From, To and Contact
// URI checks.
func (s *SanityModule) Check(msg *parser.SIPMsg, checks int) *SanityResult {
	r := &SanityResult{Passed: true}

	if checks&int(CheckURIs) != 0 {
		checks |= int(CheckRURI | CheckFrom | CheckTo | CheckContact)
	}

	if checks&int(CheckRURI) != 0 && !s.CheckRURI(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "RURI")
	}
	if checks&int(CheckVia) != 0 && !s.CheckVia(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "Via")
	}
	if checks&int(CheckCSeq) != 0 && !s.CheckCSeq(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "CSeq")
	}
	if checks&int(CheckFrom) != 0 && !s.CheckFrom(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "From")
	}
	if checks&int(CheckTo) != 0 && !s.CheckTo(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "To")
	}
	if checks&int(CheckContact) != 0 && !s.CheckContact(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "Contact")
	}
	if checks&int(CheckContentLength) != 0 && !s.CheckContentLength(msg) {
		r.Passed = false
		r.FailedChecks = append(r.FailedChecks, "ContentLength")
	}
	return r
}

// CheckURI validates a SIP URI string. Returns true when the URI parses
// to a recognised scheme (sip, sips, tel, ...).
func (s *SanityModule) CheckURI(uri string) bool {
	if uri == "" {
		return false
	}
	u, err := parser.ParseURI(uri)
	if err != nil {
		return false
	}
	return u.Type != parser.ErrorURIT
}

// CheckVia validates the top Via header: it must be present with a host
// and a branch parameter (RFC 3261 compliance).
func (s *SanityModule) CheckVia(msg *parser.SIPMsg) bool {
	if msg == nil || msg.Via1 == nil {
		return false
	}
	vb := msg.Via1
	if vb.Host.IsEmpty() {
		return false
	}
	if vb.Branch == nil || vb.Branch.Value.IsEmpty() {
		return false
	}
	return true
}

// CheckCSeq validates the CSeq header: it must be present, carry a valid
// number and method, and (for requests) the method must match the
// request-line method.
func (s *SanityModule) CheckCSeq(msg *parser.SIPMsg) bool {
	if msg == nil || msg.CSeq == nil {
		return false
	}
	cb, err := parser.ParseCSeq(msg.CSeq.Body)
	if err != nil {
		return false
	}
	if cb.Method.IsEmpty() {
		return false
	}
	if msg.IsRequest() && cb.MethodValue != msg.Method() {
		return false
	}
	return true
}

// CheckRURI validates the request URI: it must be parseable, use a
// supported scheme, and the SIP version must be 2.0.
func (s *SanityModule) CheckRURI(msg *parser.SIPMsg) bool {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return false
	}
	uriStr := msg.FirstLine.Req.URI.String()
	if uriStr == "" {
		return false
	}
	u, err := parser.ParseURI(uriStr)
	if err != nil {
		return false
	}
	if u.Type == parser.ErrorURIT {
		return false
	}
	if !strings.Contains(msg.FirstLine.Req.Version.String(), "2.0") {
		return false
	}
	return true
}

// CheckFrom validates that the From header URI is parseable.
func (s *SanityModule) CheckFrom(msg *parser.SIPMsg) bool {
	if msg == nil || msg.From == nil {
		return false
	}
	tb, err := parser.ParseToBody(msg.From.Body)
	if err != nil {
		return false
	}
	return tb.URI != nil
}

// CheckTo validates that the To header URI is parseable.
func (s *SanityModule) CheckTo(msg *parser.SIPMsg) bool {
	if msg == nil || msg.To == nil {
		return false
	}
	tb, err := parser.ParseToBody(msg.To.Body)
	if err != nil {
		return false
	}
	return tb.URI != nil
}

// CheckContact validates the Contact header URI is parseable when the
// header is present. A missing Contact header is considered acceptable.
func (s *SanityModule) CheckContact(msg *parser.SIPMsg) bool {
	if msg == nil || msg.Contact == nil {
		return true
	}
	cb, err := parser.ParseContact(msg.Contact.Body)
	if err != nil {
		return false
	}
	return cb.URI != nil
}

// CheckContentLength validates that the Content-Length header, when
// present, matches the actual message body length. A missing
// Content-Length header is considered acceptable.
func (s *SanityModule) CheckContentLength(msg *parser.SIPMsg) bool {
	if msg == nil || msg.ContentLength == nil {
		return true
	}
	cl, err := parser.ParseContentLength(msg.ContentLength)
	if err != nil {
		return false
	}
	bodyLen := 0
	if b, ok := msg.Body.([]byte); ok {
		bodyLen = len(b)
	}
	return cl == bodyLen
}

// --- package-level API ---

var defaultModule = New()

// DefaultSanity returns the default set of sanity checks (all checks).
func DefaultSanity() int {
	return int(CheckAll)
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}

// Check runs sanity checks using the default module.
func Check(msg *parser.SIPMsg, checks int) *SanityResult {
	return defaultModule.Check(msg, checks)
}
