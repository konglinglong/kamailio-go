// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * CarrierRoute module - prefix-based routing across carrier trees.
 *
 * Port of the kamailio carrierroute module (src/modules/carrierroute).
 * Carriers group routes; each RouteEntry maps a (domain, scan-prefix)
 * to a destination host:port. Route selects the active route whose
 * ScanPrefix is the longest prefix of the dialled number within the
 * requested domain, ties broken by priority then weight.
 *
 * The package is safe for concurrent use.
 */
package carrierroute

import (
	"errors"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Carrier is a named group of routes (Kamailio cr_carriers).
type Carrier struct {
	ID          int
	Name        string
	Description string
}

// RouteEntry is one routing rule (Kamailio cr_rules). ScanPrefix is the
// number prefix matched against the dialled number; an empty ScanPrefix
// matches any number. Priority and Weight break ties (higher wins).
type RouteEntry struct {
	CarrierID  int
	Domain     string
	Prefix     string
	ScanPrefix string
	Host       string
	Port       int
	Priority   int
	Weight     int
	Active     bool
}

// CarrierRouteModule implements the carrierroute module. It is safe for
// concurrent use: all state is guarded by mu.
type CarrierRouteModule struct {
	mu             sync.RWMutex
	carriers       map[int]*Carrier
	routes         map[int]*RouteEntry
	routeOrder     []int // route IDs in insertion order
	nextCarrierID  int
	nextRouteID    int
}

// NewCarrierRouteModule creates a new CarrierRouteModule.
func NewCarrierRouteModule() *CarrierRouteModule {
	return &CarrierRouteModule{
		carriers: make(map[int]*Carrier),
		routes:   make(map[int]*RouteEntry),
	}
}

// AddCarrier registers a carrier with the given name and returns it. The
// returned carrier may be configured (e.g. Description) before concurrent
// use. Returns nil if name is empty.
func (m *CarrierRouteModule) AddCarrier(name string) *Carrier {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextCarrierID++
	id := m.nextCarrierID
	c := &Carrier{ID: id, Name: name}
	m.carriers[id] = c
	return c
}

// AddRoute registers route and returns the assigned internal ID. Newly
// added routes are active by default. Returns -1 if route is nil.
func (m *CarrierRouteModule) AddRoute(route *RouteEntry) int {
	if route == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextRouteID++
	id := m.nextRouteID
	route.Active = true
	m.routes[id] = route
	m.routeOrder = append(m.routeOrder, id)
	return id
}

// Route selects an active route for msg within domain whose ScanPrefix
// is the longest prefix of the dialled number. The number is taken from
// prefix; when empty it falls back to the Request-URI user part of msg.
// Ties on prefix length are broken by priority then weight (higher
// wins). Returns an error if no route matches.
func (m *CarrierRouteModule) Route(msg *parser.SIPMsg, domain string, prefix string) (*RouteEntry, error) {
	if prefix == "" {
		prefix = ruriUser(msg)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *RouteEntry
	for _, id := range m.routeOrder {
		r := m.routes[id]
		if !r.Active {
			continue
		}
		if domain != "" && r.Domain != domain {
			continue
		}
		if r.ScanPrefix != "" && !strings.HasPrefix(prefix, r.ScanPrefix) {
			continue
		}
		if betterRoute(best, r) {
			best = r
		}
	}
	if best == nil {
		return nil, errors.New("carrierroute: no matching route")
	}
	cp := *best
	return &cp, nil
}

// RouteByCarrier is like Route but restricted to routes belonging to the
// carrier identified by carrierID.
func (m *CarrierRouteModule) RouteByCarrier(carrierID int, domain string, prefix string) (*RouteEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *RouteEntry
	for _, id := range m.routeOrder {
		r := m.routes[id]
		if !r.Active {
			continue
		}
		if r.CarrierID != carrierID {
			continue
		}
		if domain != "" && r.Domain != domain {
			continue
		}
		if r.ScanPrefix != "" && !strings.HasPrefix(prefix, r.ScanPrefix) {
			continue
		}
		if betterRoute(best, r) {
			best = r
		}
	}
	if best == nil {
		return nil, errors.New("carrierroute: no matching route for carrier")
	}
	cp := *best
	return &cp, nil
}

// MarkRoute sets the active state of the route with the given ID.
func (m *CarrierRouteModule) MarkRoute(id int, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.routes[id]; ok {
		r.Active = active
	}
}

// ListCarriers returns copies of all registered carriers.
func (m *CarrierRouteModule) ListCarriers() []*Carrier {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Carrier, 0, len(m.carriers))
	for _, c := range m.carriers {
		cp := *c
		out = append(out, &cp)
	}
	return out
}

// ListRoutes returns copies of all registered routes in insertion order.
func (m *CarrierRouteModule) ListRoutes() []*RouteEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*RouteEntry, 0, len(m.routeOrder))
	for _, id := range m.routeOrder {
		cp := *m.routes[id]
		out = append(out, &cp)
	}
	return out
}

// CountCarriers returns the number of registered carriers.
func (m *CarrierRouteModule) CountCarriers() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.carriers)
}

// CountRoutes returns the number of registered routes.
func (m *CarrierRouteModule) CountRoutes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.routes)
}

// betterRoute reports whether cur is a better match than best: longer
// ScanPrefix, then higher priority, then higher weight.
func betterRoute(best, cur *RouteEntry) bool {
	if best == nil {
		return true
	}
	if len(cur.ScanPrefix) != len(best.ScanPrefix) {
		return len(cur.ScanPrefix) > len(best.ScanPrefix)
	}
	if cur.Priority != best.Priority {
		return cur.Priority > best.Priority
	}
	return cur.Weight > best.Weight
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

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultCR *CarrierRouteModule
)

// DefaultCarrierRoute returns the process-wide CarrierRouteModule,
// creating one on first use.
func DefaultCarrierRoute() *CarrierRouteModule {
	defaultMu.RLock()
	c := defaultCR
	defaultMu.RUnlock()
	if c != nil {
		return c
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCR == nil {
		defaultCR = NewCarrierRouteModule()
	}
	return defaultCR
}

// Init (re)initialises the process-wide CarrierRouteModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCR = NewCarrierRouteModule()
}
