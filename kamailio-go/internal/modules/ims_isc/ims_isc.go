// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * IMS ISC module - Initial Filter Criteria evaluation and AS routing.
 * Port of the kamailio ims_isc module (src/modules/ims_isc).
 *
 * The IMS Service Control module evaluates the Initial Filter Criteria
 * (iFC) for a subscriber and routes SIP messages to the matching
 * Application Server. Each iFC entry carries a trigger point (an SPT
 * expression), the AS address and name, the default handling
 * (continue/terminate on failure), a priority and the session case
 * (originating / terminating / terminating_unregistered).
 *
 * Routes are evaluated in priority order; the first route whose trigger
 * point matches the message is selected.
 *
 * It is safe for concurrent use.
 */

package ims_isc

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Session case values (3GPP TS 23.218).
const (
	SessionCaseOriginating             = 0
	SessionCaseTerminating             = 1
	SessionCaseTerminatingUnregistered = 2
	SessionCaseOriginatingUnregistered = 3
)

// DefaultHandling values.
const (
	DefaultHandlingContinue  = 0
	DefaultHandlingTerminate = 1
)

// ISCRoute describes a single Initial Filter Criteria entry.
type ISCRoute struct {
	ID              int
	TriggerPoint    string
	ASAddress       string
	ASName          string
	DefaultHandling int
	Priority        int
	SessionCase     int
}

// ISCModule maintains the set of iFC routes and evaluates them.
type ISCModule struct {
	mu     sync.RWMutex
	routes map[int]*ISCRoute
	nextID atomic.Int64
}

// NewISCModule creates an ISCModule with empty route storage.
func NewISCModule() *ISCModule {
	return &ISCModule{routes: make(map[int]*ISCRoute)}
}

// AddRoute registers an iFC route and returns the assigned id. The route
// priority and session case are taken from the supplied struct.
//
//	C: isc_add_route()
func (m *ISCModule) AddRoute(route *ISCRoute) int {
	if route == nil {
		return -1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.routes == nil {
		m.routes = make(map[int]*ISCRoute)
	}
	id := int(m.nextID.Add(1))
	route.ID = id
	m.routes[id] = route
	return id
}

// RemoveRoute removes the iFC route with the given id. Returns true when
// a route was removed.
func (m *ISCModule) RemoveRoute(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.routes[id]; !ok {
		return false
	}
	delete(m.routes, id)
	return true
}

// Evaluate inspects msg against the iFC routes for the given session
// case, in priority order (lowest priority value first), and returns the
// first matching route. Returns an error when msg is nil or no route
// matches.
//
//	C: isc_check_routes() / isc_eval_ifc()
func (m *ISCModule) Evaluate(msg *parser.SIPMsg, sessionCase int) (*ISCRoute, error) {
	if msg == nil {
		return nil, errors.New("ims_isc: nil message")
	}
	ranked := m.rankedRoutes(sessionCase)
	for _, r := range ranked {
		if r.SessionCase != sessionCase {
			continue
		}
		if m.IsInitialFilterTriggered(msg, r) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("ims_isc: no matching route for session case %d", sessionCase)
}

// IsInitialFilterTriggered reports whether msg matches the trigger
// point of route. An empty trigger point always matches (iFC with no
// trigger point is unconditionally triggered). Otherwise the trigger
// point is interpreted as a comma-separated list of SPT tokens, any of
// which matches the message (logical OR); a token prefixed with "method:"
// matches the request method, "uri:" matches the request URI, and a
// bare token matches the Call-ID.
//
//	C: isc_match_trigger_point()
func (m *ISCModule) IsInitialFilterTriggered(msg *parser.SIPMsg, route *ISCRoute) bool {
	if msg == nil || route == nil {
		return false
	}
	tp := strings.TrimSpace(route.TriggerPoint)
	if tp == "" {
		return true
	}
	for _, token := range strings.Split(tp, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		switch {
		case strings.HasPrefix(token, "method:"):
			want := strings.TrimSpace(strings.TrimPrefix(token, "method:"))
			if msg.Method() != 0 && strings.EqualFold(parser.MethodName(msg.Method()), want) {
				return true
			}
		case strings.HasPrefix(token, "uri:"):
			want := strings.TrimSpace(strings.TrimPrefix(token, "uri:"))
			if want != "" && msg.FirstLine != nil && msg.FirstLine.Req != nil &&
				strings.Contains(msg.FirstLine.Req.URI.String(), want) {
				return true
			}
		default:
			if callID := headerBody(msg, msg.CallID, parser.HdrCallID); callID != "" && strings.Contains(callID, token) {
				return true
			}
		}
	}
	return false
}

// ListRoutes returns a snapshot of all iFC routes ordered by priority.
func (m *ISCModule) ListRoutes() []*ISCRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ISCRoute, 0, len(m.routes))
	for _, r := range m.routes {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Count returns the number of registered iFC routes.
func (m *ISCModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.routes)
}

// rankedRoutes returns the routes for sessionCase sorted by priority.
func (m *ISCModule) rankedRoutes(sessionCase int) []*ISCRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ISCRoute, 0, len(m.routes))
	for _, r := range m.routes {
		if r.SessionCase == sessionCase {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// headerBody returns the body string of a header, looking it up by quick
// reference first, then by type.
func headerBody(msg *parser.SIPMsg, quick *parser.HdrField, ht parser.HdrType) string {
	if quick != nil {
		return quick.Body.String()
	}
	if msg != nil {
		if h := msg.GetHeaderByType(ht); h != nil {
			return h.Body.String()
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultIM *ISCModule
)

// DefaultISC returns the process-wide ISCModule, creating one on first use.
func DefaultISC() *ISCModule {
	defaultMu.RLock()
	im := defaultIM
	defaultMu.RUnlock()
	if im != nil {
		return im
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultIM == nil {
		defaultIM = NewISCModule()
	}
	return defaultIM
}

// Init (re)initialises the process-wide ISCModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultIM = NewISCModule()
}
