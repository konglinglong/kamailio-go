// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DRouting module - prefix-based dynamic routing to gateways.
 *
 * Port of the kamailio drouting module (src/modules/drouting). Rules
 * associate a number prefix (within a routing group) with a gateway; the
 * request is routed to the gateway of the rule whose prefix is the
 * longest match of the Request-URI user part, ties broken by priority.
 * Selected gateways can rewrite the R-URI (strip digits, prepend a
 * prefix) via UseGateway.
 */
package drouting

import (
	"errors"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// Gateway is a routing target (Kamailio dr_gateways). Address is the
// gateway SIP URI; Strip/Prefix rewrite the R-URI user part when the
// gateway is used.
type Gateway struct {
	ID      int
	Address string
	Strip   int
	Prefix  string
	Weight  int
	Flags   int
	Active  bool
}

// Carrier is a named group of gateways (Kamailio dr_carriers).
type Carrier struct {
	ID       int
	Name     string
	Gateways []*Gateway
}

// RouteRule maps a number prefix within a routing group to a gateway.
// Priority breaks ties between rules with the same prefix (higher value
// wins). An empty Prefix matches any user part.
type RouteRule struct {
	ID          int
	Group       int
	Prefix      string
	Priority    int
	GatewayID   int
	Description string
}

// DRoutingModule implements the drouting module. It is safe for
// concurrent use: all state is guarded by mu.
type DRoutingModule struct {
	mu         sync.RWMutex
	gateways   map[int]*Gateway
	carriers   map[int]*Carrier
	rules      []*RouteRule
	nextGwID   int
	nextCarID  int
	nextRuleID int
}

// NewDRoutingModule creates a new DRoutingModule.
func NewDRoutingModule() *DRoutingModule {
	return &DRoutingModule{
		gateways: make(map[int]*Gateway),
		carriers: make(map[int]*Carrier),
	}
}

// AddGateway registers gw and returns the assigned ID. Newly added
// gateways are active by default. Returns -1 if gw is nil.
func (m *DRoutingModule) AddGateway(gw *Gateway) int {
	if gw == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextGwID++
	id := m.nextGwID
	gw.ID = id
	gw.Active = true
	m.gateways[id] = gw
	return id
}

// AddCarrier registers c and returns the assigned ID. Returns -1 if
// c is nil.
func (m *DRoutingModule) AddCarrier(c *Carrier) int {
	if c == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextCarID++
	id := m.nextCarID
	c.ID = id
	m.carriers[id] = c
	return id
}

// AddRule registers rule and returns the assigned ID. Returns -1 if
// rule is nil.
func (m *DRoutingModule) AddRule(rule *RouteRule) int {
	if rule == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextRuleID++
	id := m.nextRuleID
	rule.ID = id
	m.rules = append(m.rules, rule)
	return id
}

// Route selects a gateway for msg in the given group by matching the
// Request-URI user part against rule prefixes. The rule with the
// longest matching prefix wins; ties are broken by priority (higher
// value first). Returns an error if no rule matches or the chosen
// gateway is inactive.
func (m *DRoutingModule) Route(msg *parser.SIPMsg, group int) (*Gateway, error) {
	if msg == nil {
		return nil, errors.New("drouting: nil message")
	}
	user := ruriUser(msg)

	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *RouteRule
	for _, r := range m.rules {
		if r.Group != group {
			continue
		}
		if r.Prefix != "" && !strings.HasPrefix(user, r.Prefix) {
			continue
		}
		if best == nil || len(r.Prefix) > len(best.Prefix) ||
			(len(r.Prefix) == len(best.Prefix) && r.Priority > best.Priority) {
			best = r
		}
	}
	if best == nil {
		return nil, errors.New("drouting: no matching rule")
	}
	gw, ok := m.gateways[best.GatewayID]
	if !ok {
		return nil, errors.New("drouting: rule references unknown gateway")
	}
	if !gw.Active {
		return nil, errors.New("drouting: gateway inactive")
	}
	return gw, nil
}

// RouteToGateway returns the gateway with gwID, or an error if it does
// not exist or is inactive.
func (m *DRoutingModule) RouteToGateway(gwID int) (*Gateway, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	gw, ok := m.gateways[gwID]
	if !ok {
		return nil, errors.New("drouting: gateway not found")
	}
	if !gw.Active {
		return nil, errors.New("drouting: gateway inactive")
	}
	return gw, nil
}

// UseGateway applies the gateway's strip/prefix transformation to the
// Request-URI user part and stores the rewritten URI in msg.NewURI.
func (m *DRoutingModule) UseGateway(msg *parser.SIPMsg, gw *Gateway) error {
	if msg == nil {
		return errors.New("drouting: nil message")
	}
	if gw == nil {
		return errors.New("drouting: nil gateway")
	}
	if msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return errors.New("drouting: no request URI")
	}
	uriStr := msg.FirstLine.Req.URI.String()
	uri, err := parser.ParseURI(uriStr)
	if err != nil {
		return errors.New("drouting: invalid request URI: " + err.Error())
	}
	user := uri.User.String()
	if gw.Strip > 0 {
		if gw.Strip >= len(user) {
			user = ""
		} else {
			user = user[gw.Strip:]
		}
	}
	user = gw.Prefix + user
	uri.User = str.Mk(user)
	msg.NewURI = str.Mk(rebuildURI(uri))
	return nil
}

// MarkGateway sets the active state of the gateway with gwID.
func (m *DRoutingModule) MarkGateway(gwID int, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if gw, ok := m.gateways[gwID]; ok {
		gw.Active = active
	}
}

// CountGateways returns the number of registered gateways.
func (m *DRoutingModule) CountGateways() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.gateways)
}

// CountCarriers returns the number of registered carriers.
func (m *DRoutingModule) CountCarriers() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.carriers)
}

// ListGateways returns all registered gateways. The order is unspecified.
func (m *DRoutingModule) ListGateways() []*Gateway {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Gateway, 0, len(m.gateways))
	for _, gw := range m.gateways {
		out = append(out, gw)
	}
	return out
}

// ListCarriers returns all registered carriers. The order is unspecified.
func (m *DRoutingModule) ListCarriers() []*Carrier {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Carrier, 0, len(m.carriers))
	for _, c := range m.carriers {
		out = append(out, c)
	}
	return out
}

// ruriUser returns the user part of the Request-URI, or "" if it
// cannot be parsed.
func ruriUser(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	uri, err := parser.ParseURI(msg.FirstLine.Req.URI.String())
	if err != nil || uri == nil {
		return ""
	}
	return uri.User.String()
}

// rebuildURI reconstructs a SIP URI string from its parsed components.
func rebuildURI(uri *parser.SIPURI) string {
	var sb strings.Builder
	if uri.Type == parser.SIPSURIT {
		sb.WriteString("sips:")
	} else {
		sb.WriteString("sip:")
	}
	if uri.User.Len > 0 {
		sb.WriteString(uri.User.String())
		if uri.Passwd.Len > 0 {
			sb.WriteByte(':')
			sb.WriteString(uri.Passwd.String())
		}
		sb.WriteByte('@')
	}
	sb.WriteString(uri.Host.String())
	if uri.Port.Len > 0 {
		sb.WriteByte(':')
		sb.WriteString(uri.Port.String())
	}
	if uri.Params.Len > 0 {
		sb.WriteByte(';')
		sb.WriteString(uri.Params.String())
	}
	if uri.Headers.Len > 0 {
		sb.WriteByte('?')
		sb.WriteString(uri.Headers.String())
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultDR *DRoutingModule
)

// DefaultDRouting returns the process-wide DRoutingModule, creating one
// on first use.
func DefaultDRouting() *DRoutingModule {
	defaultMu.RLock()
	d := defaultDR
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDR == nil {
		defaultDR = NewDRoutingModule()
	}
	return defaultDR
}

// Init (re)initialises the process-wide DRoutingModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDR = NewDRoutingModule()
}
