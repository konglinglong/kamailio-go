// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Outbound module - SIP Outbound (RFC 5626) flow-token management.
 * Port of the kamailio outbound module (src/modules/outbound).
 *
 * SIP Outbound lets a registrar route subsequent requests back to the
 * registering client over the same flow (TCP/TLS/WebSocket connection)
 * even when the client is behind a NAT. The registrar stores an opaque
 * flow token that encodes the proxy address and connection id used by
 * the flow; later requests carrying that token are validated and routed
 * over the original connection.
 *
 * This Go counterpart generates reversible flow tokens (base64-url of
 * "ip:port:connID"), validates them, and inspects SIP messages for the
 * +sip.instance parameter that signals Outbound support.
 *
 * It is safe for concurrent use.
 */

package outbound

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// FlowTokenParam is the Contact-header parameter name used to carry the
// flow token, matching the C module's use of the Contact parameters.
const FlowTokenParam = "ob"

// OutboundConfig holds the configuration for the Outbound module.
type OutboundConfig struct {
	FlowTokenKey string
	FlowTokenTTL time.Duration
}

// OutboundModule implements SIP Outbound flow-token management.
// C: struct module outbound
type OutboundModule struct {
	mu  sync.RWMutex
	cfg *OutboundConfig
}

// New creates an OutboundModule with default configuration.
func New() *OutboundModule {
	return &OutboundModule{cfg: &OutboundConfig{FlowTokenTTL: 30 * time.Minute}}
}

// Init configures the module with the supplied OutboundConfig. A nil
// config leaves the defaults in place. Mirrors the C module's mod_init.
//
//	C: mod_init()
func (m *OutboundModule) Init(cfg *OutboundConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg != nil {
		m.cfg = cfg
		return
	}
	m.cfg = &OutboundConfig{FlowTokenTTL: 30 * time.Minute}
}

// GenerateFlowToken returns an opaque, reversible flow token encoding
// proxyIP, proxyPort and connID. The token is the URL-safe base64 encoding
// of "ip:port:connID".
//
//	C: ob_generate_flow_token() analogue
func (m *OutboundModule) GenerateFlowToken(proxyIP string, proxyPort int, connID string) string {
	raw := fmt.Sprintf("%s:%d:%s", proxyIP, proxyPort, connID)
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// ValidateFlowToken decodes token and returns the embedded proxyIP, port,
// connID and a validity flag. Returns ("", 0, "", false) when the token
// cannot be decoded or is malformed.
//
//	C: ob_validate_flow_token() analogue
func (m *OutboundModule) ValidateFlowToken(token string) (string, int, string, bool) {
	if token == "" {
		return "", 0, "", false
	}
	dec, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		// Fall back to raw std encoding for tolerance.
		dec, err = base64.StdEncoding.DecodeString(token)
		if err != nil {
			return "", 0, "", false
		}
	}
	parts := strings.SplitN(string(dec), ":", 3)
	if len(parts) != 3 {
		return "", 0, "", false
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", false
	}
	if parts[0] == "" || parts[2] == "" {
		return "", 0, "", false
	}
	return parts[0], port, parts[2], true
}

// IsOutboundSupported reports whether msg carries a Contact header whose
// body contains the +sip.instance parameter (RFC 5626).
//
//	C: is_outbound_supported() / use_outbound()
func (m *OutboundModule) IsOutboundSupported(msg *parser.SIPMsg) bool {
	if msg == nil || msg.Contact == nil {
		return false
	}
	contacts, err := parser.ParseContactList(msg.Contact.Body)
	if err != nil || len(contacts) == 0 {
		return false
	}
	for _, c := range contacts {
		if c.Instance.Len > 0 {
			return true
		}
	}
	return false
}

// GetFlowToken extracts the flow token from the Contact header of msg.
// It looks first for the "ob" parameter (FlowTokenParam) and falls back to
// the +sip.instance value. Returns the empty string when no token is found
// or msg has no Contact header.
//
//	C: ob_get_flow_token() analogue
func (m *OutboundModule) GetFlowToken(msg *parser.SIPMsg) string {
	if msg == nil || msg.Contact == nil {
		return ""
	}
	contacts, err := parser.ParseContactList(msg.Contact.Body)
	if err != nil || len(contacts) == 0 {
		return ""
	}
	for _, c := range contacts {
		if token := contactParam(c, FlowTokenParam); token != "" {
			return token
		}
	}
	for _, c := range contacts {
		if c.Instance.Len > 0 {
			return c.Instance.String()
		}
	}
	return ""
}

// AddFlowTokenToContact appends the flow token to the Contact header of
// msg as a ";ob=<token>" parameter. Returns 0 on success or -1 when msg
// is nil, has no Contact header, or token is empty.
//
//	C: ob_add_flow_token() analogue
func (m *OutboundModule) AddFlowTokenToContact(msg *parser.SIPMsg, token string) int {
	if msg == nil || msg.Contact == nil || token == "" {
		return -1
	}
	body := msg.Contact.Body.String()
	if body == "" {
		msg.Contact.Body = str.Mk(FlowTokenParam + "=" + token)
		return 0
	}
	msg.Contact.Body = str.Mk(body + ";" + FlowTokenParam + "=" + token)
	return 0
}

// contactParam returns the value of the named parameter from a parsed
// Contact body, or the empty string when absent.
func contactParam(c *parser.ContactBody, name string) string {
	if c == nil || c.Params.Len == 0 {
		return ""
	}
	for _, p := range strings.Split(c.Params.String(), ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if strings.EqualFold(kv[0], name) {
			if len(kv) == 2 {
				return kv[1]
			}
			return ""
		}
	}
	return ""
}

// ErrNoContact is returned when a message has no Contact header.
var ErrNoContact = errors.New("outbound: no Contact header")

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultOutbound *OutboundModule
)

// DefaultOutbound returns the process-wide OutboundModule, creating it on
// first use.
func DefaultOutbound() *OutboundModule {
	defaultMu.RLock()
	m := defaultOutbound
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultOutbound == nil {
		defaultOutbound = New()
	}
	return defaultOutbound
}

// Init (re)initialises the process-wide OutboundModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultOutbound = New()
}
