// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * nathelper module - NAT traversal helpers, matching C nathelper.c.
 *
 * Extends internal/core/nat with the higher-level operations provided by
 * the Kamailio nathelper module:
 *   - fix_nated_sdp        -> FixNatedSDP
 *   - add_contact_alias    -> AddContactAlias
 *   - handle_ruri_alias    -> HandleRURIAlias
 *   - is_rfc1918           -> IsRFC1918
 *   - nat_uac_test         -> NatUacTest
 *   - fix_nated_contact    -> FixNatedContact (delegates to core/nat)
 *   - natping              -> NatPing
 *   - add rport/received   -> AddRportAlias
 *
 * All exported state is guarded by a sync.RWMutex so the module is safe for
 * concurrent use.
 */

package nathelper

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/nat"
	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// NATUAC test bitmask values (matching the task specification).
const (
	NatUacTestContact1918 = 1 << 0 // 1: Contact header IP is RFC1918
	NatUacTestViaRPort    = 1 << 1 // 2: Via rport parameter present
	NatUacTestViaReceived = 1 << 2 // 4: Via received parameter present
	NatUacTestSDP1918     = 1 << 3 // 8: SDP connection IP is RFC1918
	NatUacTestDomainCmp   = 1 << 4 // 16: Contact host is a domain, not an IP
)

// Default proto digits used in the ;alias= parameter (matching Kamailio
// sip_protos enum: UDP=1, TCP=2, TLS=3, SCTP=4, WS=5, WSS=6).
const (
	aliasProtoUDP = "1"
)

// NathelperModule extends the core NAT helper with the higher-level
// operations of the Kamailio nathelper module. It is safe for concurrent
// use.
type NathelperModule struct {
	mu sync.RWMutex
	// configuration
	natpingInterval int    // NAT ping interval in seconds
	natpingMethod   string // NAT ping method
	natpingThOld    int    // ageing threshold
	sippingFrom     string // SIP ping From header
	sippingMethod   string // SIP ping method
	forceSocket     string // forced sending socket
	// state
	targets map[string]*NATTarget
}

// NATTarget records a single NAT keepalive destination.
type NATTarget struct {
	URI      string
	Socket   string
	LastPing time.Time
	Failures int
	Active   bool
}

// New creates a NathelperModule with default configuration and empty state.
func New() *NathelperModule {
	return &NathelperModule{
		natpingInterval: 60,
		natpingMethod:   "OPTIONS",
		sippingMethod:   "OPTIONS",
		targets:         make(map[string]*NATTarget),
	}
}

// SetNatpingInterval configures the NAT ping interval (seconds).
func (m *NathelperModule) SetNatpingInterval(seconds int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.natpingInterval = seconds
}

// SetNatpingMethod configures the NAT ping method.
func (m *NathelperModule) SetNatpingMethod(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.natpingMethod = method
}

// SetForceSocket configures the forced sending socket.
func (m *NathelperModule) SetForceSocket(socket string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forceSocket = socket
}

// IsRFC1918 reports whether ip belongs to one of the RFC1918 private
// address ranges: 10.0.0.0/8, 172.16.0.0/12 or 192.168.0.0/16.
//
//	C: is_rfc1918() / is1918addr()
func (m *NathelperModule) IsRFC1918(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	if parsed.To4() == nil {
		return false
	}
	return parsed.IsPrivate()
}

// isRFC1918IP is the net.IP variant used internally by the bitmask tests.
func (m *NathelperModule) isRFC1918IP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.IsPrivate()
	}
	return false
}

// FixNatedSDP rewrites the connection (c=) IP addresses in the SDP body to
// ip. When port is greater than zero the media (m=) line port is also
// rewritten. Returns an error when the message has no body.
//
//	C: fix_nated_sdp()
func (m *NathelperModule) FixNatedSDP(msg *parser.SIPMsg, ip string, port int) error {
	if msg == nil {
		return fmt.Errorf("nathelper: nil message")
	}
	body := msgBodyBytes(msg)
	if len(body) == 0 {
		return fmt.Errorf("nathelper: no message body")
	}
	rewritten := rewriteSDP(string(body), ip, port)
	msg.Body = []byte(rewritten)
	return nil
}

// AddContactAlias appends a ";alias=ip~port~proto" parameter to the Contact
// URI so that the receiving side can reconstruct the source address. The
// proto defaults to UDP (digit "1").
//
//	C: add_contact_alias()
func (m *NathelperModule) AddContactAlias(msg *parser.SIPMsg, aliasIP string, aliasPort int) error {
	if msg == nil {
		return fmt.Errorf("nathelper: nil message")
	}
	if msg.Contact == nil {
		return fmt.Errorf("nathelper: no Contact header")
	}
	body := msg.Contact.Body.String()
	if body == "" {
		return fmt.Errorf("nathelper: empty Contact body")
	}
	alias := fmt.Sprintf(";alias=%s~%d~%s", aliasIP, aliasPort, aliasProtoUDP)
	newBody := insertAliasParam(body, alias)
	msg.Contact.Body = str.Mk(newBody)
	return nil
}

// insertAliasParam inserts the alias parameter into a Contact URI body,
// placing it before the closing '>' when the URI is bracketed, or at the
// end otherwise.
func insertAliasParam(body, alias string) string {
	if idx := strings.LastIndex(body, ">"); idx >= 0 {
		return body[:idx] + alias + body[idx:]
	}
	return body + alias
}

// HandleRURIAlias extracts the alias parameter from the Request-URI, removes
// it from the URI and returns the decoded (ip, port, proto) tuple. proto is
// returned as a transport name ("udp", "tcp", ...). Returns an error when
// no alias parameter is present or the value is malformed.
//
//	C: handle_ruri_alias()
func (m *NathelperModule) HandleRURIAlias(msg *parser.SIPMsg) (string, int, string, error) {
	if msg == nil {
		return "", 0, "", fmt.Errorf("nathelper: nil message")
	}
	uri := ruriString(msg)
	if uri == "" {
		return "", 0, "", fmt.Errorf("nathelper: no Request-URI")
	}
	aliasVal, cleaned, ok := extractAliasParam(uri)
	if !ok {
		return "", 0, "", fmt.Errorf("nathelper: no alias param in RURI")
	}
	ip, port, proto, err := parseAliasValue(aliasVal)
	if err != nil {
		return "", 0, "", err
	}
	setRURIString(msg, cleaned)
	return ip, port, proto, nil
}

// NatUacTest evaluates the bitmask tests against the message and returns
// true if any of the requested tests hold:
//   - 1:  Contact header IP is RFC1918
//   - 2:  Via rport parameter present
//   - 4:  Via received parameter present
//   - 8:  SDP connection IP is RFC1918
//   - 16: Contact host is a domain (not an IP address)
//
//	C: nat_uac_test()
func (m *NathelperModule) NatUacTest(msg *parser.SIPMsg, tests int) bool {
	if msg == nil || tests == 0 {
		return false
	}
	if tests&NatUacTestContact1918 != 0 {
		if ip := contactIP(msg); ip != nil && m.isRFC1918IP(ip) {
			return true
		}
	}
	if tests&NatUacTestViaRPort != 0 {
		if msg.Via1 != nil && msg.Via1.RPort != nil && msg.Via1.RPort.Value.Len > 0 {
			return true
		}
	}
	if tests&NatUacTestViaReceived != 0 {
		if msg.Via1 != nil && msg.Via1.Received != nil && msg.Via1.Received.Value.Len > 0 {
			return true
		}
	}
	if tests&NatUacTestSDP1918 != 0 {
		if ip := sdpIP(msg); ip != nil && m.isRFC1918IP(ip) {
			return true
		}
	}
	if tests&NatUacTestDomainCmp != 0 {
		if host := contactHost(msg); host != "" && net.ParseIP(host) == nil {
			return true
		}
	}
	return false
}

// FixNatedContact rewrites the Contact header URI to use the source IP and
// port. Delegates to core/nat.FixContact.
//
//	C: fix_nated_contact()
func (m *NathelperModule) FixNatedContact(msg *parser.SIPMsg, sourceIP string, sourcePort int) error {
	return nat.FixContact(msg, sourceIP, sourcePort)
}

// AddRportAlias sets the received and rport parameters on the topmost Via
// header to the source IP and port.
//
//	C: add_rcv_param() / rport handling
func (m *NathelperModule) AddRportAlias(msg *parser.SIPMsg, sourceIP string, sourcePort int) error {
	if msg == nil {
		return fmt.Errorf("nathelper: nil message")
	}
	if msg.Via1 == nil {
		return fmt.Errorf("nathelper: no Via header")
	}
	msg.Via1.Received = &parser.ViaParam{Value: str.Mk(sourceIP)}
	msg.Via1.RPort = &parser.ViaParam{Value: str.Mk(strconv.Itoa(sourcePort))}
	return nil
}

// NatPing sends a NAT keepalive to the given URI. Without a bound transport
// socket this records/refreshes the target's ping state and returns nil on
// success.
//
//	C: natping / sip_pinger
func (m *NathelperModule) NatPing(uri string, socket string) error {
	if uri == "" {
		return fmt.Errorf("nathelper: empty ping URI")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.targets == nil {
		m.targets = make(map[string]*NATTarget)
	}
	tgt, ok := m.targets[uri]
	if !ok {
		tgt = &NATTarget{URI: uri, Socket: socket, Active: true}
		m.targets[uri] = tgt
	}
	tgt.Socket = socket
	tgt.LastPing = time.Now()
	tgt.Active = true
	tgt.Failures = 0
	return nil
}

// GetTarget returns the NAT target registered for uri, or nil.
func (m *NathelperModule) GetTarget(uri string) *NATTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.targets[uri]
}

// RemoveTarget removes the NAT target for uri. Returns true when a target
// was removed.
func (m *NathelperModule) RemoveTarget(uri string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.targets[uri]; !ok {
		return false
	}
	delete(m.targets, uri)
	return true
}

// ListTargets returns a snapshot of all registered NAT targets.
func (m *NathelperModule) ListTargets() []*NATTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*NATTarget, 0, len(m.targets))
	for _, t := range m.targets {
		out = append(out, t)
	}
	return out
}

// -----------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------

// msgBodyBytes returns the message body as a byte slice.
func msgBodyBytes(msg *parser.SIPMsg) []byte {
	if msg.Body == nil {
		return nil
	}
	if b, ok := msg.Body.([]byte); ok {
		return b
	}
	return nil
}

// rewriteSDP replaces the connection IP in every c= line and, when port > 0,
// rewrites the media port in every m= line.
func rewriteSDP(sdp, newIP string, port int) string {
	lines := strings.Split(sdp, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "c=in ip4 ") {
			trimmed = "c=IN IP4 " + newIP
		} else if strings.HasPrefix(lower, "c=in ip6 ") {
			trimmed = "c=IN IP6 " + newIP
		} else if port > 0 && strings.HasPrefix(lower, "m=") {
			trimmed = rewriteMediaPort(trimmed, port)
		}
		// preserve the original line terminator
		if strings.HasSuffix(line, "\r") {
			trimmed += "\r"
		}
		lines[i] = trimmed
	}
	return strings.Join(lines, "\n")
}

// rewriteMediaPort replaces the port in an "m=<media> <port> <proto> ..."
// line.
func rewriteMediaPort(line string, port int) string {
	// m=audio 5004 RTP/AVP 0
	body := strings.TrimPrefix(line, "m=")
	parts := strings.SplitN(body, " ", 3)
	if len(parts) < 3 {
		return line
	}
	parts[1] = strconv.Itoa(port)
	return "m=" + strings.Join(parts, " ")
}

// ruriString returns the effective Request-URI string (NewURI if set,
// otherwise the first-line URI).
func ruriString(msg *parser.SIPMsg) string {
	if msg.NewURI.Len > 0 {
		return msg.NewURI.String()
	}
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		return msg.FirstLine.Req.URI.String()
	}
	return ""
}

// setRURIString updates the effective Request-URI in place.
func setRURIString(msg *parser.SIPMsg, uri string) {
	if msg.NewURI.Len > 0 {
		msg.NewURI = str.Mk(uri)
		return
	}
	if msg.FirstLine != nil && msg.FirstLine.Req != nil {
		msg.FirstLine.Req.URI = str.Mk(uri)
	}
}

// extractAliasParam locates the first ";alias=..." parameter in uri,
// returning the alias value, the URI with the alias parameter removed, and
// whether an alias parameter was found.
func extractAliasParam(uri string) (value, cleaned string, ok bool) {
	idx := strings.Index(strings.ToLower(uri), ";alias=")
	if idx < 0 {
		// alias= may also appear without leading ';' at the very start
		// (rare); handle it for robustness.
		if strings.HasPrefix(strings.ToLower(uri), "alias=") {
			idx = 0
		} else {
			return "", "", false
		}
	}
	start := idx
	if idx == 0 {
		// alias= at position 0
	}
	valStart := idx + len(";alias=")
	if idx == 0 {
		valStart = len("alias=")
	}
	// find end of alias value: next ';', '?' or end of string
	rest := uri[valStart:]
	end := len(rest)
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c == ';' || c == '?' {
			end = j
			break
		}
	}
	value = rest[:end]
	// remove the alias parameter (and its leading ';' if present) from uri
	var cleanedURI string
	if start == 0 {
		cleanedURI = uri[valStart+end:]
		// drop a leading ';' left behind
		cleanedURI = strings.TrimPrefix(cleanedURI, ";")
	} else {
		cleanedURI = uri[:start] + uri[valStart+end:]
	}
	return value, cleanedURI, true
}

// parseAliasValue parses "ip~port~proto" into (ip, port, protoName).
func parseAliasValue(v string) (string, int, string, error) {
	parts := strings.SplitN(v, "~", 3)
	if len(parts) < 3 {
		return "", 0, "", fmt.Errorf("nathelper: malformed alias value %q", v)
	}
	ip := parts[0]
	// IPv6 alias values may be bracketed.
	ip = strings.TrimPrefix(ip, "[")
	ip = strings.TrimSuffix(ip, "]")
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", fmt.Errorf("nathelper: invalid alias port %q", parts[1])
	}
	proto := protoName(parts[2])
	return ip, port, proto, nil
}

// protoName maps a Kamailio proto digit to a transport name.
func protoName(digit string) string {
	switch digit {
	case "1":
		return "udp"
	case "2":
		return "tcp"
	case "3":
		return "tls"
	case "4":
		return "sctp"
	case "5":
		return "ws"
	case "6":
		return "wss"
	default:
		return "udp"
	}
}

// contactHost extracts the host portion of the Contact URI.
func contactHost(msg *parser.SIPMsg) string {
	if msg.Contact == nil {
		return ""
	}
	return hostFromURI(msg.Contact.Body.String())
}

// contactIP extracts the host IP of the Contact URI, or nil if it is not an
// IP address.
func contactIP(msg *parser.SIPMsg) net.IP {
	host := contactHost(msg)
	if host == "" {
		return nil
	}
	return net.ParseIP(host)
}

// sdpIP extracts the connection IP from the SDP body, or nil.
func sdpIP(msg *parser.SIPMsg) net.IP {
	body := msgBodyBytes(msg)
	if len(body) == 0 {
		return nil
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "c=in ip4 ") || strings.HasPrefix(lower, "c=in ip6 ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return net.ParseIP(fields[2])
			}
		}
	}
	return nil
}

// hostFromURI extracts the host part of a SIP URI string.
func hostFromURI(uri string) string {
	uri = strings.TrimSpace(uri)
	// strip display name and angle brackets
	if lt := strings.Index(uri, "<"); lt >= 0 {
		if gt := strings.Index(uri, ">"); gt > lt {
			uri = uri[lt+1 : gt]
		}
	}
	if strings.HasPrefix(strings.ToLower(uri), "sips:") {
		uri = uri[5:]
	} else if strings.HasPrefix(strings.ToLower(uri), "sip:") {
		uri = uri[4:]
	}
	// drop parameters and headers
	if idx := strings.IndexAny(uri, ";?"); idx >= 0 {
		uri = uri[:idx]
	}
	// drop user@
	if idx := strings.Index(uri, "@"); idx >= 0 {
		uri = uri[idx+1:]
	}
	// drop port (but keep IPv6 brackets intact)
	if idx := strings.LastIndex(uri, ":"); idx >= 0 && !strings.Contains(uri[idx+1:], ":") {
		uri = uri[:idx]
	}
	uri = strings.Trim(uri, "[]")
	return uri
}

// -----------------------------------------------------------------------
// process-wide singleton (mirrors the C module's global state)
// -----------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultNH *NathelperModule
)

// DefaultNathelper returns the process-wide NathelperModule, creating it on
// first use.
func DefaultNathelper() *NathelperModule {
	defaultMu.RLock()
	m := defaultNH
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultNH == nil {
		defaultNH = New()
	}
	return defaultNH
}

// Init (re)initialises the process-wide NathelperModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultNH = New()
}

// IsRFC1918 is the package-level wrapper around DefaultNathelper().IsRFC1918.
func IsRFC1918(ip string) bool { return DefaultNathelper().IsRFC1918(ip) }

// FixNatedSDP is the package-level wrapper.
func FixNatedSDP(msg *parser.SIPMsg, ip string, port int) error {
	return DefaultNathelper().FixNatedSDP(msg, ip, port)
}

// AddContactAlias is the package-level wrapper.
func AddContactAlias(msg *parser.SIPMsg, aliasIP string, aliasPort int) error {
	return DefaultNathelper().AddContactAlias(msg, aliasIP, aliasPort)
}

// HandleRURIAlias is the package-level wrapper.
func HandleRURIAlias(msg *parser.SIPMsg) (string, int, string, error) {
	return DefaultNathelper().HandleRURIAlias(msg)
}

// NatUacTest is the package-level wrapper.
func NatUacTest(msg *parser.SIPMsg, tests int) bool {
	return DefaultNathelper().NatUacTest(msg, tests)
}

// FixNatedContact is the package-level wrapper.
func FixNatedContact(msg *parser.SIPMsg, sourceIP string, sourcePort int) error {
	return DefaultNathelper().FixNatedContact(msg, sourceIP, sourcePort)
}

// AddRportAlias is the package-level wrapper.
func AddRportAlias(msg *parser.SIPMsg, sourceIP string, sourcePort int) error {
	return DefaultNathelper().AddRportAlias(msg, sourceIP, sourcePort)
}

// NatPing is the package-level wrapper.
func NatPing(uri, socket string) error { return DefaultNathelper().NatPing(uri, socket) }

// GetTarget is the package-level wrapper.
func GetTarget(uri string) *NATTarget { return DefaultNathelper().GetTarget(uri) }

// RemoveTarget is the package-level wrapper.
func RemoveTarget(uri string) bool { return DefaultNathelper().RemoveTarget(uri) }

// ListTargets is the package-level wrapper.
func ListTargets() []*NATTarget { return DefaultNathelper().ListTargets() }
