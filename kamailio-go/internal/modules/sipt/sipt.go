// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * sipt module - ISUP/SIP translation helpers.
 * Port of the kamailio sipt module (src/modules/sipt).
 *
 * The original C module translates between ISUP (ISDN User Part, the
 * SS7 call-control protocol) and SIP, and builds/parses IAM (Initial
 * Address Message) bodies. This Go counterpart exposes the same
 * operations using a compact binary IAM representation:
 *
 *	[0]      message type (1 = IAM)
 *	[1]      called-number digit count
 *	[2..]    called-number digits (ASCII)
 *	[next]   calling-number digit count
 *	[next+1] calling-number digits (ASCII)
 *
 * It is safe for concurrent use.
 */

package sipt

import (
	"fmt"
	"strings"
	"sync"
)

// ISUP message type codes (subset).
const (
	MsgIAM = 0x01 // Initial Address Message
	MsgSAM = 0x02 // Subsequent Address Message
	MsgACM = 0x06 // Address Complete Message
	MsgANM = 0x09 // Answer Message
	MsgREL = 0x0c // Release
	MsgRLC = 0x0d // Release Complete
)

// SIPTModule translates between ISUP and SIP and handles IAM bodies.
// It is the Go counterpart of the kamailio sipt module.
type SIPTModule struct {
	mu sync.RWMutex
}

// New creates a SIPTModule.
func New() *SIPTModule {
	return &SIPTModule{}
}

// IsupToSip converts an ISUP message into a SIP message body. For an
// IAM the result is a SIP/SDP-like representation carrying the called
// and calling party numbers.
//
//	C: isup_to_sip()
func (m *SIPTModule) IsupToSip(isup []byte) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(isup) < 2 {
		return "", fmt.Errorf("sipt: isup too short")
	}
	if isup[0] != MsgIAM {
		return "", fmt.Errorf("sipt: unsupported message type 0x%02x", isup[0])
	}
	called, calling, err := m.parseIAM(isup)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Content-Type: application/sdp\r\n")
	fmt.Fprintf(&b, "From: <%s>\r\n", calling)
	fmt.Fprintf(&b, "To: <%s>\r\n", called)
	fmt.Fprintf(&b, "ISUP: IAM\r\n")
	return b.String(), nil
}

// SipToIsup converts a SIP message body back into an ISUP IAM byte
// slice, extracting the From/To numbers.
//
//	C: sip_to_isup()
func (m *SIPTModule) SipToIsup(sipBody string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calling := extractHeader(sipBody, "From")
	called := extractHeader(sipBody, "To")
	calling = stripAngle(calling)
	called = stripAngle(called)
	if called == "" && calling == "" {
		return nil, fmt.Errorf("sipt: no From/To in SIP body")
	}
	return m.buildIAM(called, calling)
}

// GetIAMCalledNumber extracts the called party number from an IAM body.
//
//	C: sipt_get_called_party()
func (m *SIPTModule) GetIAMCalledNumber(isup []byte) (string, error) {
	called, _, err := m.parseIAM(isup)
	return called, err
}

// GetIAMCallingNumber extracts the calling party number from an IAM body.
//
//	C: sipt_get_calling_party()
func (m *SIPTModule) GetIAMCallingNumber(isup []byte) (string, error) {
	_, calling, err := m.parseIAM(isup)
	return calling, err
}

// BuildIAM builds an IAM byte slice carrying the called and calling
// party numbers.
//
//	C: sipt_build_iam()
func (m *SIPTModule) BuildIAM(called, calling string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buildIAM(called, calling)
}

// --- internal helpers (no locking) ---

// buildIAM encodes called and calling into the compact IAM format.
func (m *SIPTModule) buildIAM(called, calling string) ([]byte, error) {
	calledDigits := digitsOnly(called)
	callingDigits := digitsOnly(calling)
	if calledDigits == "" {
		return nil, fmt.Errorf("sipt: empty called number")
	}
	out := make([]byte, 0, 2+len(calledDigits)+1+len(callingDigits))
	out = append(out, MsgIAM)
	out = append(out, byte(len(calledDigits)))
	out = append(out, calledDigits...)
	out = append(out, byte(len(callingDigits)))
	out = append(out, callingDigits...)
	return out, nil
}

// parseIAM decodes an IAM byte slice into called and calling numbers.
func (m *SIPTModule) parseIAM(isup []byte) (called, calling string, err error) {
	if len(isup) < 2 {
		return "", "", fmt.Errorf("sipt: isup too short")
	}
	if isup[0] != MsgIAM {
		return "", "", fmt.Errorf("sipt: not an IAM (0x%02x)", isup[0])
	}
	pos := 1
	calledLen := int(isup[pos])
	pos++
	if pos+calledLen > len(isup) {
		return "", "", fmt.Errorf("sipt: truncated called number")
	}
	called = string(isup[pos : pos+calledLen])
	pos += calledLen
	if pos >= len(isup) {
		return called, "", nil
	}
	callingLen := int(isup[pos])
	pos++
	if pos+callingLen > len(isup) {
		return called, "", nil
	}
	calling = string(isup[pos : pos+callingLen])
	return called, calling, nil
}

// extractHeader returns the value of the named header (case-insensitive)
// from a SIP-like body. Returns "" when absent.
func extractHeader(body, name string) string {
	lines := strings.Split(body, "\n")
	needle := strings.ToLower(name) + ":"
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trim), needle) {
			return strings.TrimSpace(trim[len(needle):])
		}
	}
	return ""
}

// stripAngle removes surrounding < > from a SIP URI.
func stripAngle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}

// digitsOnly strips everything except digits from s.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- package-level API ---

var defaultModule = New()

// DefaultSIPT returns the package-level default SIPTModule.
func DefaultSIPT() *SIPTModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
