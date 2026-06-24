// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * UAC module - User Agent Client helpers.
 * Port of the kamailio uac module (src/modules/uac).
 *
 * The UAC module provides functions for:
 *   - building and sending UAC requests (t_uac_send equivalent),
 *   - replacing and restoring the From / To headers of in-dialog
 *     requests (uac_replace_from / uac_replace_to),
 *   - performing UAC authentication by computing a digest response
 *     and inserting an Authorization header (uac_auth),
 *   - detecting in-dialog requests.
 *
 * It is safe for concurrent use.
 */

package uac

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// DefaultMaxForwards is the default Max-Forwards value placed on
// generated UAC requests, matching the C uac default.
const DefaultMaxForwards = 70

// UACModule implements User Agent Client helpers.
type UACModule struct {
	mu sync.Mutex

	// fromBackup / toBackup store the original From/To header bodies
	// keyed by message pointer so they can be restored by RestoreFrom /
	// RestoreTo. This mirrors the C module's lump-based backup.
	fromBackup map[*parser.SIPMsg]string
	toBackup   map[*parser.SIPMsg]string

	// requestsSent counts the number of requests built by SendRequest.
	requestsSent atomic.Int64
}

// New creates a UACModule configured with the C module defaults.
func New() *UACModule {
	return &UACModule{
		fromBackup: make(map[*parser.SIPMsg]string),
		toBackup:   make(map[*parser.SIPMsg]string),
	}
}

// SendRequest builds a UAC request from the supplied parameters and
// returns its on-wire serialization. The generated message carries the
// mandatory From, To, Call-ID, CSeq, Max-Forwards and Content-Length
// headers, followed by the caller-supplied headers and the body.
//
//	C: t_uac_send() / t_uac_start()
func (m *UACModule) SendRequest(method, ruri, fromURI, toURI string, headers []string, body string) ([]byte, error) {
	if method == "" {
		return nil, fmt.Errorf("uac: empty method")
	}
	if ruri == "" {
		return nil, fmt.Errorf("uac: empty request URI")
	}
	m.requestsSent.Add(1)

	var sb strings.Builder
	// Request line.
	sb.WriteString(method)
	sb.WriteByte(' ')
	sb.WriteString(ruri)
	sb.WriteString(" SIP/2.0\r\n")

	// From header (with a generated tag).
	fromTag := generateTag()
	sb.WriteString("From: <")
	sb.WriteString(fromURI)
	sb.WriteString(">;tag=")
	sb.WriteString(fromTag)
	sb.WriteString("\r\n")

	// To header.
	sb.WriteString("To: <")
	sb.WriteString(toURI)
	sb.WriteString(">\r\n")

	// Call-ID.
	callID := generateCallID()
	sb.WriteString("Call-ID: ")
	sb.WriteString(callID)
	sb.WriteString("\r\n")

	// CSeq.
	sb.WriteString("CSeq: 1 ")
	sb.WriteString(method)
	sb.WriteString("\r\n")

	// Max-Forwards.
	sb.WriteString("Max-Forwards: ")
	sb.WriteString(fmt.Sprintf("%d", DefaultMaxForwards))
	sb.WriteString("\r\n")

	// Caller-supplied headers.
	for _, h := range headers {
		sb.WriteString(h)
		sb.WriteString("\r\n")
	}

	// Content-Length and body.
	sb.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))
	sb.WriteString("\r\n")
	sb.WriteString(body)

	return []byte(sb.String()), nil
}

// ReplaceFrom replaces the From header of msg with one built from display
// and uri, preserving the original tag. The original From body is stored
// so it can be restored by RestoreFrom. Returns 0 on success or -1 when
// msg has no From header.
//
//	C: uac_replace_from()
func (m *UACModule) ReplaceFrom(msg *parser.SIPMsg, display, uri string) int {
	if msg == nil || msg.From == nil {
		return -1
	}
	orig := msg.From.Body.String()
	tag := extractTag(msg.From.Body)

	var sb strings.Builder
	if display != "" {
		sb.WriteString(display)
		sb.WriteByte(' ')
	}
	sb.WriteString("<")
	sb.WriteString(uri)
	sb.WriteString(">")
	if tag != "" {
		sb.WriteString(";tag=")
		sb.WriteString(tag)
	}

	m.mu.Lock()
	if m.fromBackup == nil {
		m.fromBackup = make(map[*parser.SIPMsg]string)
	}
	m.fromBackup[msg] = orig
	m.mu.Unlock()

	msg.From.Body = str.Mk(sb.String())
	return 0
}

// RestoreFrom restores the original From header previously saved by
// ReplaceFrom. Returns 0 on success or -1 when there is no backup.
//
//	C: uac_restore_from()
func (m *UACModule) RestoreFrom(msg *parser.SIPMsg) int {
	if msg == nil {
		return -1
	}
	m.mu.Lock()
	orig, ok := m.fromBackup[msg]
	if ok {
		delete(m.fromBackup, msg)
	}
	m.mu.Unlock()
	if !ok {
		return -1
	}
	if msg.From != nil {
		msg.From.Body = str.Mk(orig)
	}
	return 0
}

// ReplaceTo replaces the To header of msg with one built from display
// and uri, preserving the original tag. Returns 0 on success or -1 when
// msg has no To header.
//
//	C: uac_replace_to()
func (m *UACModule) ReplaceTo(msg *parser.SIPMsg, display, uri string) int {
	if msg == nil || msg.To == nil {
		return -1
	}
	orig := msg.To.Body.String()
	tag := extractTag(msg.To.Body)

	var sb strings.Builder
	if display != "" {
		sb.WriteString(display)
		sb.WriteByte(' ')
	}
	sb.WriteString("<")
	sb.WriteString(uri)
	sb.WriteString(">")
	if tag != "" {
		sb.WriteString(";tag=")
		sb.WriteString(tag)
	}

	m.mu.Lock()
	if m.toBackup == nil {
		m.toBackup = make(map[*parser.SIPMsg]string)
	}
	m.toBackup[msg] = orig
	m.mu.Unlock()

	msg.To.Body = str.Mk(sb.String())
	return 0
}

// RestoreTo restores the original To header previously saved by
// ReplaceTo. Returns 0 on success or -1 when there is no backup.
//
//	C: uac_restore_to()
func (m *UACModule) RestoreTo(msg *parser.SIPMsg) int {
	if msg == nil {
		return -1
	}
	m.mu.Lock()
	orig, ok := m.toBackup[msg]
	if ok {
		delete(m.toBackup, msg)
	}
	m.mu.Unlock()
	if !ok {
		return -1
	}
	if msg.To != nil {
		msg.To.Body = str.Mk(orig)
	}
	return 0
}

// Auth computes a digest response for the given credentials and inserts
// an Authorization header into msg. The method and request URI are taken
// from the message's request line. Returns 0 on success or -1 when msg
// is nil or has no request line.
//
//	C: uac_auth()
func (m *UACModule) Auth(msg *parser.SIPMsg, realm, user, password string) int {
	if msg == nil {
		return -1
	}
	method, uri := requestMethodAndURI(msg)
	if method == "" {
		return -1
	}

	nonce := m.lookupNonce(msg)
	if nonce == "" {
		nonce = generateNonce()
	}
	uriValue := uri
	if uriValue == "" {
		uriValue = "sip:localhost"
	}

	ha1 := md5hex(fmt.Sprintf("%s:%s:%s", user, realm, password))
	ha2 := md5hex(fmt.Sprintf("%s:%s", method, uriValue))
	response := md5hex(fmt.Sprintf("%s:%s:%s", ha1, nonce, ha2))

	value := fmt.Sprintf(
		`Digest username="%s",realm="%s",nonce="%s",uri="%s",response="%s",algorithm=MD5`,
		user, realm, nonce, uriValue, response,
	)
	// Create the header with the explicit Authorization type, since the
	// parser's name-based lookup does not map "Authorization" yet.
	hdr := &parser.HdrField{
		Name: str.Mk("Authorization"),
		Body: str.Mk(value),
		Type: parser.HdrAuthorization,
	}
	msg.Headers = append(msg.Headers, hdr)
	msg.LastHeader = hdr
	return 0
}

// ReqInDialog reports whether msg is an in-dialog request, i.e. both
// the From and To headers carry a tag parameter.
//
//	C: dlg_req_in_dialog() analogue
func (m *UACModule) ReqInDialog(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	if msg.From == nil || msg.To == nil {
		return false
	}
	return extractTag(msg.From.Body) != "" && extractTag(msg.To.Body) != ""
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// lookupNonce scans msg for a WWW-Authenticate or Proxy-Authenticate
// challenge and returns its nonce value, or the empty string.
func (m *UACModule) lookupNonce(msg *parser.SIPMsg) string {
	for _, name := range []string{"WWW-Authenticate", "Proxy-Authenticate"} {
		for _, h := range msg.Headers {
			if strings.EqualFold(h.Name.String(), name) {
				return extractParam(h.Body.String(), "nonce")
			}
		}
	}
	return ""
}

// requestMethodAndURI returns the method name and request URI string
// from msg's request line.
func requestMethodAndURI(msg *parser.SIPMsg) (string, string) {
	if msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return "", ""
	}
	req := msg.FirstLine.Req
	method := req.Method.String()
	if method == "" {
		method = parser.MethodName(req.MethodValue)
	}
	return method, req.URI.String()
}

// extractTag scans a From/To header body and returns the value of the
// "tag" parameter, or the empty string if not present.
func extractTag(body str.Str) string {
	s := body.String()
	if s == "" {
		return ""
	}
	return extractParam(s, "tag")
}

// extractParam extracts the value of a name=value parameter from a
// header body (looking for ;name= or leading name=).
func extractParam(s, name string) string {
	lower := strings.ToLower(s)
	lname := strings.ToLower(name)
	idx := strings.Index(lower, ";"+lname+"=")
	if idx < 0 {
		// tolerate a leading "name=" (no semicolon)
		if strings.HasPrefix(lower, lname+"=") {
			rest := s[len(name)+1:]
			if semi := strings.IndexByte(rest, ';'); semi >= 0 {
				return strings.TrimSpace(rest[:semi])
			}
			return strings.TrimSpace(rest)
		}
		return ""
	}
	rest := s[idx+len(";"+name+"="):]
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		return strings.TrimSpace(rest[:semi])
	}
	return strings.TrimSpace(rest)
}

// md5hex returns the hex-encoded MD5 digest of s.
func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// generateTag returns a short random tag string.
func generateTag() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("t%d", fallbackID())
	}
	return hex.EncodeToString(buf)
}

// generateCallID returns a random Call-ID.
func generateCallID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("callid%d@kamailio-go", fallbackID())
	}
	return hex.EncodeToString(buf) + "@kamailio-go"
}

// generateNonce returns a random nonce.
func generateNonce() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("nonce%d", fallbackID())
	}
	return hex.EncodeToString(buf)
}

// fallbackID is a monotonic counter used only when crypto/rand fails, which
// in practice never happens on a working system.
var fallbackCounter atomic.Uint64

func fallbackID() uint64 {
	return fallbackCounter.Add(1)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu  sync.RWMutex
	defaultUAC *UACModule
)

// DefaultUAC returns the process-wide UACModule, creating it on first use.
func DefaultUAC() *UACModule {
	defaultMu.RLock()
	m := defaultUAC
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultUAC == nil {
		defaultUAC = New()
	}
	return defaultUAC
}

// Init (re)initialises the process-wide UACModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultUAC = New()
}

// SendRequest is the package-level wrapper around DefaultUAC().SendRequest.
func SendRequest(method, ruri, fromURI, toURI string, headers []string, body string) ([]byte, error) {
	return DefaultUAC().SendRequest(method, ruri, fromURI, toURI, headers, body)
}

// ReplaceFrom is the package-level wrapper around DefaultUAC().ReplaceFrom.
func ReplaceFrom(msg *parser.SIPMsg, display, uri string) int {
	return DefaultUAC().ReplaceFrom(msg, display, uri)
}

// RestoreFrom is the package-level wrapper around DefaultUAC().RestoreFrom.
func RestoreFrom(msg *parser.SIPMsg) int {
	return DefaultUAC().RestoreFrom(msg)
}

// ReplaceTo is the package-level wrapper around DefaultUAC().ReplaceTo.
func ReplaceTo(msg *parser.SIPMsg, display, uri string) int {
	return DefaultUAC().ReplaceTo(msg, display, uri)
}

// RestoreTo is the package-level wrapper around DefaultUAC().RestoreTo.
func RestoreTo(msg *parser.SIPMsg) int {
	return DefaultUAC().RestoreTo(msg)
}

// Auth is the package-level wrapper around DefaultUAC().Auth.
func Auth(msg *parser.SIPMsg, realm, user, password string) int {
	return DefaultUAC().Auth(msg, realm, user, password)
}

// ReqInDialog is the package-level wrapper around DefaultUAC().ReqInDialog.
func ReqInDialog(msg *parser.SIPMsg) bool {
	return DefaultUAC().ReqInDialog(msg)
}
