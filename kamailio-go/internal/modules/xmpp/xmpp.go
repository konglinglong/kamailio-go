// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xmpp module - SIP to XMPP gateway.
 *
 * Port of the kamailio xmpp module (src/modules/xmpp). Bridges SIP MESSAGE
 * and presence to XMPP. The actual XMPP transport is performed through the
 * XMPPConn interface so tests can substitute an in-memory mock and
 * production code can plug in a real XMPP client (e.g. mellium.im/xmpp).
 *
 * C equivalent: xmpp.so - xmpp.c / xmpp_api.c.
 */

package xmpp

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Config holds the XMPP gateway configuration.
//
// C equivalent: the xmpp_host / xmpp_port / xmpp_domain modparams.
type Config struct {
	Server        string // XMPP server host
	Port          int    // XMPP server port (5222 client / 5269 s2s)
	JID           string // gateway JID (user@domain)
	Password      string // gateway password
	Domain        string // XMPP domain (served)
	GatewayDomain string // SIP domain mapped to XMPP
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Server:        "127.0.0.1",
		Port:          5222,
		JID:           "kamailio@xmpp.example.org",
		Password:      "",
		Domain:        "xmpp.example.org",
		GatewayDomain: "sip.example.org",
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("xmpp: nil config")
	}
	if strings.TrimSpace(c.Server) == "" {
		return errors.New("xmpp: empty server")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("xmpp: invalid port %d", c.Port)
	}
	if strings.TrimSpace(c.Domain) == "" {
		return errors.New("xmpp: empty domain")
	}
	return nil
}

// ---------------------------------------------------------------------------
// XMPPConn - abstracted XMPP connection (no hard dependency on a client lib)
// ---------------------------------------------------------------------------

// XMPPConn is the minimal subset of XMPP operations required by the
// module. It is an interface so tests can substitute an in-memory mock and
// production code can plug in a real client.
type XMPPConn interface {
	Connect() error
	SendMessage(to, body string) error
	SendPresence(status string) error
	Disconnect() error
}

// ConnFactory builds an XMPPConn for the given config. Production builds
// inject a real factory; the default returns a mock.
type ConnFactory func(cfg Config) (XMPPConn, error)

// mockXMPPConn is an in-memory, concurrency-safe XMPPConn.
type mockXMPPConn struct {
	mu        sync.Mutex
	cfg       Config
	connected bool
	messages  []mockMessage
	presence  []string
	closed    bool
}

type mockMessage struct {
	To   string
	Body string
}

func (c *mockXMPPConn) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("xmpp: connection closed")
	}
	c.connected = true
	return nil
}

func (c *mockXMPPConn) SendMessage(to, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("xmpp: connection closed")
	}
	if !c.connected {
		return errors.New("xmpp: not connected")
	}
	c.messages = append(c.messages, mockMessage{To: to, Body: body})
	return nil
}

func (c *mockXMPPConn) SendPresence(status string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("xmpp: connection closed")
	}
	if !c.connected {
		return errors.New("xmpp: not connected")
	}
	c.presence = append(c.presence, status)
	return nil
}

func (c *mockXMPPConn) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	c.closed = true
	return nil
}

// newMockConn creates an unconnected mock XMPPConn.
func newMockConn(cfg Config) *mockXMPPConn {
	return &mockXMPPConn{cfg: cfg}
}

// ---------------------------------------------------------------------------
// XMPP wire types (minimal subset for message conversion)
// ---------------------------------------------------------------------------

// xmppMessage is a minimal XMPP <message> stanza.
type xmppMessage struct {
	XMLName xml.Name `xml:"message"`
	From    string   `xml:"from,attr"`
	To      string   `xml:"to,attr"`
	Type    string   `xml:"type,attr,omitempty"`
	Body    string   `xml:"body"`
}

// xmppPresence is a minimal XMPP <presence> stanza.
type xmppPresence struct {
	XMLName xml.Name `xml:"presence"`
	From    string   `xml:"from,attr"`
	To      string   `xml:"to,attr,omitempty"`
	Type    string   `xml:"type,attr,omitempty"`
	Show    string   `xml:"show,omitempty"`
	Status  string   `xml:"status,omitempty"`
}

// ---------------------------------------------------------------------------
// XMPPModule
// ---------------------------------------------------------------------------

// XMPPModule is the SIP-to-XMPP gateway. It owns an XMPP connection and
// translates SIP MESSAGE / presence into XMPP stanzas and back.
//
// C equivalent: the module global state plus the xmpp_api connection.
type XMPPModule struct {
	mu      sync.RWMutex
	cfg     Config
	factory ConnFactory
	conn    XMPPConn
	sent    atomic.Int64
}

// New creates an XMPPModule with default configuration and a mock factory.
func New() *XMPPModule {
	cfg := *DefaultConfig()
	m := &XMPPModule{cfg: cfg}
	m.factory = func(c Config) (XMPPConn, error) {
		return newMockConn(c), nil
	}
	return m
}

// NewWithConfig creates an XMPPModule using the supplied configuration.
func NewWithConfig(cfg Config) *XMPPModule {
	m := &XMPPModule{cfg: cfg}
	m.factory = func(c Config) (XMPPConn, error) {
		return newMockConn(c), nil
	}
	return m
}

// Init (re)configures the module with the supplied config and drops any
// active connection.
//
// C equivalent: xmpp_init() / mod_init().
func (m *XMPPModule) Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_ = m.conn.Disconnect()
		m.conn = nil
	}
	m.cfg = cfg
	return nil
}

// Config returns a copy of the current configuration.
func (m *XMPPModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// SetServer configures the XMPP server host and port.
func (m *XMPPModule) SetServer(server string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Server = server
	m.cfg.Port = port
}

// SetCredentials configures the gateway JID and password.
func (m *XMPPModule) SetCredentials(jid, password string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.JID = jid
	m.cfg.Password = password
}

// SetDomain configures the XMPP domain.
func (m *XMPPModule) SetDomain(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Domain = domain
}

// SetConnFactory injects a real connection factory (production wiring).
func (m *XMPPModule) SetConnFactory(f ConnFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factory = f
}

// Connect establishes the XMPP connection using the configured factory.
//
// C equivalent: xmpp_connect().
func (m *XMPPModule) Connect() error {
	m.mu.RLock()
	cfg := m.cfg
	factory := m.factory
	conn := m.conn
	m.mu.RUnlock()

	if conn != nil {
		return nil // already connected
	}
	c, err := factory(cfg)
	if err != nil {
		return fmt.Errorf("xmpp connect: %w", err)
	}
	if err := c.Connect(); err != nil {
		return fmt.Errorf("xmpp connect: %w", err)
	}
	m.mu.Lock()
	if m.conn != nil {
		// Another goroutine raced ahead; prefer the existing one.
		_ = c.Disconnect()
		m.mu.Unlock()
		return nil
	}
	m.conn = c
	m.mu.Unlock()
	return nil
}

// IsConnected reports whether the XMPP connection is established.
func (m *XMPPModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn != nil
}

// SendXMPPMessage sends a chat message to an XMPP recipient.
//
// C equivalent: xmpp_send_message().
func (m *XMPPModule) SendXMPPMessage(to, body string) error {
	if to == "" {
		return errors.New("xmpp: empty recipient")
	}
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()
	if conn == nil {
		return errors.New("xmpp: not connected")
	}
	m.sent.Add(1)
	return conn.SendMessage(to, body)
}

// SendXMPPPresence publishes a presence status.
//
// C equivalent: xmpp_send_presence().
func (m *XMPPModule) SendXMPPPresence(status string) error {
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()
	if conn == nil {
		return errors.New("xmpp: not connected")
	}
	m.sent.Add(1)
	return conn.SendPresence(status)
}

// Disconnect tears down the XMPP connection.
//
// C equivalent: xmpp_disconnect().
func (m *XMPPModule) Disconnect() error {
	m.mu.Lock()
	conn := m.conn
	m.conn = nil
	m.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Disconnect()
}

// SentCount returns the number of sent stanzas.
func (m *XMPPModule) SentCount() int64 {
	return m.sent.Load()
}

// SIPToXMPP converts a SIP MESSAGE request into an XMPP <message> stanza
// XML string. The From user becomes the XMPP sender and the To user the
// recipient (both mapped to the configured XMPP domain).
//
// C equivalent: sip2xmpp() in xmpp.c.
func (m *XMPPModule) SIPToXMPP(msg *parser.SIPMsg) (string, error) {
	if msg == nil {
		return "", errors.New("xmpp: nil sip message")
	}
	if !msg.IsRequest() {
		return "", errors.New("xmpp: not a sip request")
	}
	fromUser := extractUser(msg.From)
	toUser := extractUser(msg.To)
	body := extractBody(msg)

	m.mu.RLock()
	domain := m.cfg.Domain
	m.mu.RUnlock()

	stanza := xmppMessage{
		From: jidOf(fromUser, domain),
		To:   jidOf(toUser, domain),
		Type: "chat",
		Body: body,
	}
	out, err := xml.Marshal(stanza)
	if err != nil {
		return "", fmt.Errorf("xmpp marshal: %w", err)
	}
	return string(out), nil
}

// XMPPToSIP parses an XMPP <message> stanza and builds a SIP MESSAGE
// request. The returned SIPMsg is parsed from the generated raw bytes.
//
// C equivalent: xmpp2sip() in xmpp.c.
func (m *XMPPModule) XMPPToSIP(xmppMsg string) (*parser.SIPMsg, error) {
	if strings.TrimSpace(xmppMsg) == "" {
		return nil, errors.New("xmpp: empty message")
	}
	var stanza xmppMessage
	if err := xml.Unmarshal([]byte(xmppMsg), &stanza); err != nil {
		return nil, fmt.Errorf("xmpp parse: %w", err)
	}
	fromUser := userOfJID(stanza.From)
	toUser := userOfJID(stanza.To)

	m.mu.RLock()
	gwDomain := m.cfg.GatewayDomain
	m.mu.RUnlock()
	if gwDomain == "" {
		gwDomain = "sip.example.org"
	}

	sipBytes := buildSIPMessage(fromUser, toUser, gwDomain, stanza.Body)
	msg, err := parser.ParseMsg(sipBytes)
	if err != nil {
		return nil, fmt.Errorf("xmpp sip parse: %w", err)
	}
	return msg, nil
}

// buildSIPMessage constructs a minimal SIP MESSAGE request as bytes.
func buildSIPMessage(fromUser, toUser, domain, body string) []byte {
	if fromUser == "" {
		fromUser = "xmpp"
	}
	if toUser == "" {
		toUser = "xmpp"
	}
	callID := "xmpp-" + fromUser + "-" + toUser + "@kamailio"
	var b strings.Builder
	fmt.Fprintf(&b, "MESSAGE sip:%s@%s SIP/2.0\r\n", toUser, domain)
	b.WriteString("Via: SIP/2.0/UDP kamailio;branch=z9hG4bK-xmpp\r\n")
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <sip:%s@%s>;tag=xmpp-from\r\n", fromUser, domain)
	fmt.Fprintf(&b, "To: <sip:%s@%s>\r\n", toUser, domain)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	b.WriteString("CSeq: 1 MESSAGE\r\n")
	b.WriteString("Content-Type: text/plain\r\n")
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// extractUser returns the user portion of a From/To header's URI.
func extractUser(h *parser.HdrField) string {
	if h == nil {
		return ""
	}
	// Use the parsed body if present.
	if h.Parsed != nil {
		if tb, ok := h.Parsed.(*parser.ToBody); ok && tb.URI != nil {
			return tb.URI.User.String()
		}
	}
	// Otherwise parse the header body on demand.
	tb, err := parser.ParseToBody(h.Body)
	if err != nil || tb == nil || tb.URI == nil {
		return ""
	}
	return tb.URI.User.String()
}

// extractBody returns the SIP message body as a string.
func extractBody(msg *parser.SIPMsg) string {
	if msg == nil {
		return ""
	}
	if b, ok := msg.Body.([]byte); ok && len(b) > 0 {
		return string(b)
	}
	// Fall back to the raw buffer after the header/body separator.
	if len(msg.Buf) > 0 {
		if idx := bytesIndex(msg.Buf, []byte("\r\n\r\n")); idx >= 0 {
			return string(msg.Buf[idx+4:])
		}
	}
	return ""
}

// bytesIndex returns the index of needle in haystack, or -1.
func bytesIndex(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// jidOf builds a JID from a user and domain.
func jidOf(user, domain string) string {
	if user == "" {
		return domain
	}
	return user + "@" + domain
}

// userOfJID extracts the user portion of a JID (user@domain).
func userOfJID(jid string) string {
	if idx := strings.Index(jid, "@"); idx > 0 {
		return jid[:idx]
	}
	return jid
}

// Compile-time interface checks.
var _ XMPPConn = (*mockXMPPConn)(nil)

// ---------------------------------------------------------------------------
// Process-wide singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *XMPPModule
)

// DefaultXMPP returns the process-wide module, creating it on first use.
func DefaultXMPP() *XMPPModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)configures the process-wide module with the supplied config and
// drops any active connection.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = &XMPPModule{cfg: cfg}
	defaultM.factory = func(c Config) (XMPPConn, error) {
		return newMockConn(c), nil
	}
	return nil
}

// Connect is the package-level wrapper around DefaultXMPP().Connect.
func Connect() error { return DefaultXMPP().Connect() }

// SendXMPPMessage is the package-level wrapper around
// DefaultXMPP().SendXMPPMessage.
func SendXMPPMessage(to, body string) error { return DefaultXMPP().SendXMPPMessage(to, body) }

// SendXMPPPresence is the package-level wrapper around
// DefaultXMPP().SendXMPPPresence.
func SendXMPPPresence(status string) error { return DefaultXMPP().SendXMPPPresence(status) }

// Disconnect is the package-level wrapper around DefaultXMPP().Disconnect.
func Disconnect() error { return DefaultXMPP().Disconnect() }

// SIPToXMPP is the package-level wrapper around DefaultXMPP().SIPToXMPP.
func SIPToXMPP(msg *parser.SIPMsg) (string, error) { return DefaultXMPP().SIPToXMPP(msg) }

// XMPPToSIP is the package-level wrapper around DefaultXMPP().XMPPToSIP.
func XMPPToSIP(xmppMsg string) (*parser.SIPMsg, error) { return DefaultXMPP().XMPPToSIP(xmppMsg) }
