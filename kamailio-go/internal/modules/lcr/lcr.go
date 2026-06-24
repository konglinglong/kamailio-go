// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * LCR module - least-cost routing with ordered gateway fallback.
 *
 * Port of the kamailio lcr module (src/modules/lcr). Rules associate a
 * number prefix (and optionally a From URI) with a gateway; the request
 * is routed to the gateway of the matching rule whose (rule priority,
 * gateway priority) tuple is smallest (lowest cost). NextGateway
 * returns the remaining matching gateways in priority order, allowing
 * serial forking to fallback gateways.
 */
package lcr

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// LCRGateway is a routing target (Kamailio lcr_gw). Priority orders
// gateways within a rule (lower value = lower cost = tried first).
type LCRGateway struct {
	ID       int
	Name     string
	URI      string
	Priority int
	Weight   int
	Flags    int
	Active   bool
}

// LCRRule maps a prefix (and optionally a From URI) to a gateway.
// Priority orders rules (lower value = higher precedence). An empty
// Prefix matches any user part; an empty FromURI matches any From.
type LCRRule struct {
	Prefix    string
	FromURI   string
	GatewayID int
	Priority  int
	Weight    int
}

// LCRModule implements the lcr module. It is safe for concurrent use:
// the gateway/rule stores are guarded by mu, and per-message cursors
// are kept in a sync.Map with their own mutex.
type LCRModule struct {
	mu         sync.RWMutex
	gateways   map[int]*LCRGateway
	rules      []*LCRRule
	nextGwID   int
	nextRuleID int
	cursors    sync.Map // map[string]*lcrCursor
}

// lcrCursor tracks the ordered candidate list and the next index to
// return for a given message. The mutex guards nextIndex.
type lcrCursor struct {
	mu        sync.Mutex
	gateways  []*LCRGateway
	nextIndex int
}

// NewLCRModule creates a new LCRModule.
func NewLCRModule() *LCRModule {
	return &LCRModule{gateways: make(map[int]*LCRGateway)}
}

// AddGateway registers gw and returns the assigned ID. Newly added
// gateways are active by default. Returns -1 if gw is nil.
func (m *LCRModule) AddGateway(gw *LCRGateway) int {
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

// AddRule registers rule and returns the assigned ID. Returns -1 if
// rule is nil.
func (m *LCRModule) AddRule(rule *LCRRule) int {
	if rule == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextRuleID++
	id := m.nextRuleID
	m.rules = append(m.rules, rule)
	return id
}

// LoadRules replaces the rule set with the supplied rules.
func (m *LCRModule) LoadRules(rules []*LCRRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = m.rules[:0]
	for _, r := range rules {
		if r == nil {
			continue
		}
		m.nextRuleID++
		m.rules = append(m.rules, r)
	}
}

// LoadGateways replaces the gateway set with the supplied gateways.
func (m *LCRModule) LoadGateways(gws []*LCRGateway) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.gateways {
		delete(m.gateways, k)
	}
	for _, gw := range gws {
		if gw == nil {
			continue
		}
		m.nextGwID++
		gw.ID = m.nextGwID
		gw.Active = true
		m.gateways[gw.ID] = gw
	}
}

// SelectGateway selects the lowest-cost gateway for msg: the gateway of
// the matching rule with the smallest (rule priority, gateway priority)
// tuple. Matching rules are those whose Prefix is a prefix of the
// Request-URI user part and whose FromURI (if set) equals the message
// From URI. The ordered candidate list is cached so NextGateway can
// return the remaining gateways. Returns an error if no gateway
// matches.
func (m *LCRModule) SelectGateway(msg *parser.SIPMsg) (*LCRGateway, error) {
	if msg == nil {
		return nil, errors.New("lcr: nil message")
	}
	candidates := m.candidatesFor(msg)
	if len(candidates) == 0 {
		return nil, errors.New("lcr: no matching gateway")
	}
	c := &lcrCursor{gateways: candidates, nextIndex: 1}
	m.cursors.Store(msgKey(msg), c)
	return candidates[0], nil
}

// NextGateway returns the next matching gateway in priority order for
// msg, following a prior SelectGateway call. Returns an error if
// SelectGateway has not been called for msg or no more gateways remain.
func (m *LCRModule) NextGateway(msg *parser.SIPMsg) (*LCRGateway, error) {
	if msg == nil {
		return nil, errors.New("lcr: nil message")
	}
	v, ok := m.cursors.Load(msgKey(msg))
	if !ok {
		return nil, errors.New("lcr: SelectGateway not called for this message")
	}
	c := v.(*lcrCursor)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nextIndex >= len(c.gateways) {
		return nil, errors.New("lcr: no more gateways")
	}
	gw := c.gateways[c.nextIndex]
	c.nextIndex++
	return gw, nil
}

// MarkGateway sets the active state of the gateway with gwID.
func (m *LCRModule) MarkGateway(gwID int, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if gw, ok := m.gateways[gwID]; ok {
		gw.Active = active
	}
}

// CountGateways returns the number of registered gateways.
func (m *LCRModule) CountGateways() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.gateways)
}

// CountRules returns the number of registered rules.
func (m *LCRModule) CountRules() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// ListGateways returns all registered gateways. The order is unspecified.
func (m *LCRModule) ListGateways() []*LCRGateway {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*LCRGateway, 0, len(m.gateways))
	for _, gw := range m.gateways {
		out = append(out, gw)
	}
	return out
}

// candidate pairs a rule with its resolved gateway for ordering.
type candidate struct {
	rulePriority int
	gwPriority   int
	gw           *LCRGateway
}

// candidatesFor returns the active gateways of all matching rules,
// ordered by (rule priority, gateway priority) ascending so that the
// first element is the lowest-cost gateway.
func (m *LCRModule) candidatesFor(msg *parser.SIPMsg) []*LCRGateway {
	user := ruriUser(msg)
	fromURI := fromURIStr(msg)

	m.mu.RLock()
	defer m.mu.RUnlock()

	var cands []candidate
	for _, r := range m.rules {
		if r.Prefix != "" && !strings.HasPrefix(user, r.Prefix) {
			continue
		}
		if r.FromURI != "" && r.FromURI != fromURI {
			continue
		}
		gw, ok := m.gateways[r.GatewayID]
		if !ok || !gw.Active {
			continue
		}
		cands = append(cands, candidate{
			rulePriority: r.Priority,
			gwPriority:   gw.Priority,
			gw:           gw,
		})
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].rulePriority != cands[j].rulePriority {
			return cands[i].rulePriority < cands[j].rulePriority
		}
		return cands[i].gwPriority < cands[j].gwPriority
	})
	out := make([]*LCRGateway, len(cands))
	for i, c := range cands {
		out[i] = c.gw
	}
	return out
}

// ruriUser returns the user part of the Request-URI, or "" if it cannot
// be parsed.
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

// fromURIStr returns the From header URI as a string (e.g.
// "sip:alice@example.com"), or "" if it cannot be parsed.
func fromURIStr(msg *parser.SIPMsg) string {
	if msg == nil || msg.From == nil {
		return ""
	}
	fb, err := parser.ParseFromBody(msg.From.Body)
	if err != nil || fb == nil || fb.URI == nil {
		return ""
	}
	return fb.URI.String()
}

// msgKey returns a stable key identifying a message for cursor storage,
// preferring the Call-ID and falling back to the Request-URI.
func msgKey(msg *parser.SIPMsg) string {
	if msg != nil && msg.CallID != nil {
		if s := msg.CallID.Body.String(); s != "" {
			return s
		}
	}
	if msg != nil && msg.FirstLine != nil && msg.FirstLine.Req != nil {
		return msg.FirstLine.Req.URI.String()
	}
	return ""
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultLC *LCRModule
)

// DefaultLCR returns the process-wide LCRModule, creating one on first
// use.
func DefaultLCR() *LCRModule {
	defaultMu.RLock()
	l := defaultLC
	defaultMu.RUnlock()
	if l != nil {
		return l
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultLC == nil {
		defaultLC = NewLCRModule()
	}
	return defaultLC
}

// Init (re)initialises the process-wide LCRModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultLC = NewLCRModule()
}
