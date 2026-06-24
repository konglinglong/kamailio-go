// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PrefixRoute module - request routing based on R-URI number prefixes.
 *
 * Port of the kamailio prefix_route module (src/modules/prefix_route).
 * Routes associate a number prefix with a script route name. Match
 * selects the enabled route whose Prefix is the longest prefix of the
 * Request-URI user part; ties on prefix length are broken by priority
 * (higher wins), then by insertion order.
 *
 * The package is safe for concurrent use.
 */
package prefix_route

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// PrefixRoute is one prefix-based routing entry (Kamailio prefix_route
// row). Prefix is the number prefix matched against the dialled number;
// an empty Prefix matches any number. Route is the script route name to
// execute on match.
type PrefixRoute struct {
	Prefix      string
	Route       string
	Description string
	Priority    int
	Enabled     bool
}

// PrefixRouteModule implements the prefix_route module. It is safe for
// concurrent use: all state is guarded by mu.
type PrefixRouteModule struct {
	mu     sync.RWMutex
	routes map[int]*PrefixRoute
	order  []int // route IDs in insertion order
	nextID int
}

// NewPrefixRouteModule creates a new PrefixRouteModule.
func NewPrefixRouteModule() *PrefixRouteModule {
	return &PrefixRouteModule{routes: make(map[int]*PrefixRoute)}
}

// AddRoute registers route and returns the assigned ID. The Enabled
// field is taken as supplied by the caller. Returns -1 if route is nil.
func (m *PrefixRouteModule) AddRoute(route *PrefixRoute) int {
	if route == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	m.routes[id] = route
	m.order = append(m.order, id)
	return id
}

// RemoveRoute deletes all routes whose Prefix equals prefix. Returns
// true when at least one route was removed.
func (m *PrefixRouteModule) RemoveRoute(prefix string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := false
	var newOrder []int
	for _, id := range m.order {
		r := m.routes[id]
		if r.Prefix == prefix {
			delete(m.routes, id)
			removed = true
			continue
		}
		newOrder = append(newOrder, id)
	}
	m.order = newOrder
	return removed
}

// Match selects the enabled route whose Prefix is the longest prefix of
// the Request-URI user part of msg. Ties on prefix length are broken by
// priority (higher wins), then by insertion order. Returns an error if
// no route matches or msg has no usable Request-URI.
func (m *PrefixRouteModule) Match(msg *parser.SIPMsg) (*PrefixRoute, error) {
	if msg == nil {
		return nil, errors.New("prefix_route: nil message")
	}
	return m.MatchPrefix(ruriUser(msg))
}

// MatchPrefix selects the enabled route whose Prefix is the longest
// prefix of prefix. Ties on prefix length are broken by priority (higher
// wins), then by insertion order. Returns an error if no route matches.
func (m *PrefixRouteModule) MatchPrefix(prefix string) (*PrefixRoute, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bestID := -1
	for _, id := range m.order {
		r := m.routes[id]
		if !r.Enabled {
			continue
		}
		if r.Prefix != "" && !strings.HasPrefix(prefix, r.Prefix) {
			continue
		}
		if bestID == -1 || betterPrefix(m.routes[bestID], r) {
			bestID = id
		}
	}
	if bestID == -1 {
		return nil, errors.New("prefix_route: no matching route")
	}
	cp := *m.routes[bestID]
	return &cp, nil
}

// ListRoutes returns copies of all registered routes in insertion order.
func (m *PrefixRouteModule) ListRoutes() []*PrefixRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*PrefixRoute, 0, len(m.order))
	for _, id := range m.order {
		cp := *m.routes[id]
		out = append(out, &cp)
	}
	return out
}

// Count returns the number of registered routes.
func (m *PrefixRouteModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.routes)
}

// EnableRoute enables all routes whose Prefix equals prefix. Returns
// true when at least one matching route exists.
func (m *PrefixRouteModule) EnableRoute(prefix string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for _, id := range m.order {
		r := m.routes[id]
		if r.Prefix == prefix {
			r.Enabled = true
			found = true
		}
	}
	return found
}

// DisableRoute disables all routes whose Prefix equals prefix. Returns
// true when at least one matching route exists.
func (m *PrefixRouteModule) DisableRoute(prefix string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for _, id := range m.order {
		r := m.routes[id]
		if r.Prefix == prefix {
			r.Enabled = false
			found = true
		}
	}
	return found
}

// LoadFromCSV loads routes from a CSV file. The expected header row is:
//
//	prefix,route,description,priority,enabled
//
// where priority is an integer (default 0) and enabled is "1"/"true" for
// enabled and anything else for disabled; a missing enabled column
// defaults to enabled. Existing routes are preserved; loaded routes are
// appended.
func (m *PrefixRouteModule) LoadFromCSV(path string) error {
	if path == "" {
		return errors.New("prefix_route: empty csv path")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("prefix_route: open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("prefix_route: read csv: %w", err)
	}
	if len(records) == 0 {
		return errors.New("prefix_route: empty csv")
	}

	start := 0
	if first := records[0]; len(first) > 0 && strings.EqualFold(first[0], "prefix") {
		start = 1
	}
	for i := start; i < len(records); i++ {
		row := records[i]
		prefix := strings.TrimSpace(cell(row, 0))
		route := strings.TrimSpace(cell(row, 1))
		description := strings.TrimSpace(cell(row, 2))
		priority, _ := strconv.Atoi(strings.TrimSpace(cell(row, 3)))
		enabled := parseBool(cell(row, 4), true)
		if prefix == "" {
			continue
		}
		m.AddRoute(&PrefixRoute{
			Prefix:      prefix,
			Route:       route,
			Description: description,
			Priority:    priority,
			Enabled:     enabled,
		})
	}
	return nil
}

// betterPrefix reports whether cur is a better match than best: longer
// Prefix, then higher priority. Equal scores keep the earlier (best)
// route, preserving insertion order.
func betterPrefix(best, cur *PrefixRoute) bool {
	if len(cur.Prefix) != len(best.Prefix) {
		return len(cur.Prefix) > len(best.Prefix)
	}
	return cur.Priority > best.Priority
}

// cell returns row[i] or "" when i is out of range.
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// parseBool parses a boolean from "1"/"true"/"yes" (case-insensitive);
// missing/empty yields dflt.
func parseBool(s string, dflt bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return dflt
	}
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
	defaultPR *PrefixRouteModule
)

// DefaultPrefixRoute returns the process-wide PrefixRouteModule, creating
// one on first use.
func DefaultPrefixRoute() *PrefixRouteModule {
	defaultMu.RLock()
	p := defaultPR
	defaultMu.RUnlock()
	if p != nil {
		return p
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultPR == nil {
		defaultPR = NewPrefixRouteModule()
	}
	return defaultPR
}

// Init (re)initialises the process-wide PrefixRouteModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPR = NewPrefixRouteModule()
}
