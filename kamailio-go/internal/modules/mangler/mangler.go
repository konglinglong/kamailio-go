// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * mangler - SIP header / SDP mangling.
 *
 * Rewrites Contact header hostnames and SDP connection-line addresses so
 * that private topology is hidden from downstream peers. Mirrors the
 * kamailio mangler module.
 */

package mangler

import (
	"regexp"
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// MangleIP is the address substituted into mangled headers/SDP.
const MangleIP = "10.0.0.1"

var (
	contactHostRE = regexp.MustCompile(`@[^;>\s]+`)
	sdpConnRE     = regexp.MustCompile(`c=IN IP4 [0-9.]+`)
)

// ManglerModule rewrites Contact and SDP addresses.
type ManglerModule struct{}

// New returns a new ManglerModule.
func New() *ManglerModule { return &ManglerModule{} }

// MangleContact rewrites the host portion of every Contact header to
// MangleIP and returns the number of headers modified.
func (m *ManglerModule) MangleContact(msg *parser.SIPMsg) int {
	if m == nil || msg == nil {
		return 0
	}
	count := 0
	for _, h := range msg.Headers {
		if h == nil || h.Type != parser.HdrContact {
			continue
		}
		body := h.Body.String()
		if !strings.Contains(body, "@") {
			continue
		}
		newBody := contactHostRE.ReplaceAllString(body, "@"+MangleIP)
		if newBody != body {
			h.Body = str.Mk(newBody)
			count++
		}
	}
	return count
}

// MangleSDP rewrites every SDP "c=IN IP4 <addr>" line in the message body
// to use MangleIP and returns the number of lines replaced.
func (m *ManglerModule) MangleSDP(msg *parser.SIPMsg) int {
	if m == nil || msg == nil {
		return 0
	}
	body := bodyString(msg)
	if body == "" {
		return 0
	}
	matches := sdpConnRE.FindAllString(body, -1)
	if len(matches) == 0 {
		return 0
	}
	newBody := sdpConnRE.ReplaceAllString(body, "c=IN IP4 "+MangleIP)
	setBody(msg, newBody)
	return len(matches)
}

// UnmangleContact restores Contact headers by replacing MangleIP with the
// placeholder "0.0.0.0". Returns the number of headers restored. In a real
// deployment the original address would be recovered from a lookup table;
// here we expose the inverse operation for round-trip testing.
func (m *ManglerModule) UnmangleContact(msg *parser.SIPMsg) int {
	if m == nil || msg == nil {
		return 0
	}
	count := 0
	for _, h := range msg.Headers {
		if h == nil || h.Type != parser.HdrContact {
			continue
		}
		body := h.Body.String()
		if !strings.Contains(body, "@"+MangleIP) {
			continue
		}
		newBody := strings.Replace(body, "@"+MangleIP, "@0.0.0.0", 1)
		h.Body = str.Mk(newBody)
		count++
	}
	return count
}

// bodyString returns the message body as a string.
func bodyString(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if b, ok := msg.Body.([]byte); ok {
		return string(b)
	}
	if s, ok := msg.Body.(string); ok {
		return s
	}
	return ""
}

// setBody updates the message body from a string.
func setBody(msg *parser.SIPMsg, s string) {
	if msg == nil {
		return
	}
	msg.Body = []byte(s)
}
