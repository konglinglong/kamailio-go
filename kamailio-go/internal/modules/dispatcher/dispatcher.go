// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Dispatcher module - load balancing across groups of SIP destinations.
 *
 * Port of the kamailio dispatcher module (src/modules/dispatcher). A
 * DispatcherModule holds named sets of destinations; each request is
 * dispatched to one destination of a set chosen by an algorithm
 * (round-robin, random, weight, priority, hash).
 *
 * In Kamailio the dispatcher list is loaded from a CSV file with columns
 *   set_id, destination, flags, priority, weight, description, attrs
 * and a destination is active until a health probe marks it inactive.
 */
package dispatcher

import (
	"bufio"
	"crypto/sha1"
	"encoding/binary"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// Destination is a single dispatch target (Kamailio ds_dst).
// The URI, Weight, Priority and Flags fields are set when the destination
// is added and are not mutated afterwards; Active reflects the runtime
// state toggled by health probes.
type Destination struct {
	ID          int
	URI         string
	Weight      int
	Priority    int
	Flags       int
	Active      bool
	Description string
}

// DispatchSet is a named group of destinations (Kamailio destination set).
// The mutex guards the destination list and the round-robin cursor.
type DispatchSet struct {
	ID           int
	Name         string
	Destinations []*Destination
	mu           sync.RWMutex
	rrIndex      int
}

// DispatchStats holds dispatcher statistics. The fields are safe for
// concurrent access via atomic operations.
type DispatchStats struct {
	TotalSelected        atomic.Int64
	ActiveDestinations   atomic.Int64
	InactiveDestinations atomic.Int64
}

// DispatcherModule implements the dispatcher module. It is safe for
// concurrent use: the sets map is guarded by mu, each set's destination
// list by the set's own mutex, and the statistics are atomic.
type DispatcherModule struct {
	mu     sync.RWMutex
	sets   map[int]*DispatchSet
	stats  DispatchStats
	nextID int
}

// NewDispatcherModule creates a new DispatcherModule.
func NewDispatcherModule() *DispatcherModule {
	return &DispatcherModule{sets: make(map[int]*DispatchSet)}
}

// AddSet adds a dispatch set with the given id and name, replacing any
// existing set with the same id, and returns the new set.
func (m *DispatcherModule) AddSet(id int, name string) *DispatchSet {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &DispatchSet{ID: id, Name: name}
	m.sets[id] = s
	return s
}

// GetSet returns the set with the given id, or nil if no such set exists.
func (m *DispatcherModule) GetSet(id int) *DispatchSet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sets[id]
}

// AddDestination adds dest to the set identified by setID. Newly added
// destinations are marked active (matching Kamailio, where a destination
// is active until a probe marks it down). Returns the assigned
// destination ID, or -1 if the set does not exist or dest is nil.
func (m *DispatcherModule) AddDestination(setID int, dest *Destination) int {
	if dest == nil {
		return -1
	}
	m.mu.Lock()
	s, ok := m.sets[setID]
	if !ok {
		m.mu.Unlock()
		return -1
	}
	m.nextID++
	id := m.nextID
	m.mu.Unlock()

	dest.ID = id
	dest.Active = true
	s.mu.Lock()
	s.Destinations = append(s.Destinations, dest)
	s.mu.Unlock()
	m.recountStats()
	return id
}

// RemoveDestination removes the destination with destID from the set.
// Returns true if a destination was removed.
func (m *DispatcherModule) RemoveDestination(setID, destID int) bool {
	m.mu.RLock()
	s, ok := m.sets[setID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	for i, d := range s.Destinations {
		if d.ID == destID {
			s.Destinations = append(s.Destinations[:i], s.Destinations[i+1:]...)
			if s.rrIndex > 0 && s.rrIndex >= len(s.Destinations) {
				s.rrIndex = 0
			}
			s.mu.Unlock()
			m.recountStats()
			return true
		}
	}
	s.mu.Unlock()
	return false
}

// SelectDestination selects a single destination from the set using the
// given algorithm. Supported algorithms: "round-robin", "random",
// "weight", "priority", "hash". Returns nil for an unknown algorithm or
// a set with no active destinations.
func (m *DispatcherModule) SelectDestination(setID int, algorithm string) *Destination {
	return m.SelectDestinationWithMsg(setID, algorithm, nil)
}

// SelectDestinationWithMsg is like SelectDestination but supplies a SIP
// message used by the "hash" algorithm (hashed on Call-ID, falling back
// to the Request-URI).
func (m *DispatcherModule) SelectDestinationWithMsg(setID int, algorithm string, msg *parser.SIPMsg) *Destination {
	m.mu.RLock()
	s, ok := m.sets[setID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	var sel *Destination
	switch algorithm {
	case "round-robin":
		sel = m.selectRoundRobin(s)
	case "random":
		sel = selectFromActive(s, selectRandom)
	case "weight":
		sel = selectFromActive(s, selectWeight)
	case "priority":
		sel = selectFromActive(s, selectPriority)
	case "hash":
		sel = selectFromActive(s, func(active []*Destination) *Destination {
			return selectHash(active, msg)
		})
	default:
		return nil
	}
	if sel != nil {
		m.stats.TotalSelected.Add(1)
	}
	return sel
}

// SelectDestinations selects up to count destinations from the set. For
// "round-robin" the cursor advances across the active destinations; for
// the other algorithms the first count active destinations are returned.
func (m *DispatcherModule) SelectDestinations(setID int, count int, algorithm string) []*Destination {
	m.mu.RLock()
	s, ok := m.sets[setID]
	m.mu.RUnlock()
	if !ok || count <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active := make([]*Destination, 0, len(s.Destinations))
	for _, d := range s.Destinations {
		if d.Active {
			active = append(active, d)
		}
	}
	if len(active) == 0 {
		return nil
	}
	if count > len(active) {
		count = len(active)
	}
	result := make([]*Destination, 0, count)
	switch algorithm {
	case "round-robin":
		for i := 0; i < count; i++ {
			result = append(result, active[s.rrIndex%len(active)])
			s.rrIndex = (s.rrIndex + 1) % len(active)
		}
	default:
		for i := 0; i < count; i++ {
			result = append(result, active[i])
		}
	}
	m.stats.TotalSelected.Add(int64(len(result)))
	return result
}

// MarkDestination sets the active state of the destination with destID.
func (m *DispatcherModule) MarkDestination(setID, destID int, active bool) {
	m.mu.RLock()
	s, ok := m.sets[setID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	for _, d := range s.Destinations {
		if d.ID == destID {
			d.Active = active
			break
		}
	}
	s.mu.Unlock()
	m.recountStats()
}

// IsActive reports whether the destination with destID is active.
func (m *DispatcherModule) IsActive(setID, destID int) bool {
	m.mu.RLock()
	s, ok := m.sets[setID]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.Destinations {
		if d.ID == destID {
			return d.Active
		}
	}
	return false
}

// Count returns the number of destinations in the set (active or not).
func (m *DispatcherModule) Count(setID int) int {
	m.mu.RLock()
	s, ok := m.sets[setID]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Destinations)
}

// ListSets returns all dispatch sets. The order is unspecified.
func (m *DispatcherModule) ListSets() []*DispatchSet {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*DispatchSet, 0, len(m.sets))
	for _, s := range m.sets {
		out = append(out, s)
	}
	return out
}

// LoadFromCSV loads destinations from a Kamailio-style dispatcher list
// file. Each non-comment line has the columns
//
//	set_id, destination, flags, priority, weight, description[, attrs]
//
// Missing sets are created on demand. Lines that cannot be parsed are
// skipped.
func (m *DispatcherModule) LoadFromCSV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		setID, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			continue
		}
		dest := &Destination{URI: strings.TrimSpace(fields[1])}
		if len(fields) > 2 {
			dest.Flags, _ = strconv.Atoi(strings.TrimSpace(fields[2]))
		}
		if len(fields) > 3 {
			dest.Priority, _ = strconv.Atoi(strings.TrimSpace(fields[3]))
		}
		if len(fields) > 4 {
			dest.Weight, _ = strconv.Atoi(strings.TrimSpace(fields[4]))
		}
		if len(fields) > 5 {
			dest.Description = strings.TrimSpace(fields[5])
		}
		if m.GetSet(setID) == nil {
			m.AddSet(setID, "")
		}
		m.AddDestination(setID, dest)
	}
	return scanner.Err()
}

// Stats returns the dispatcher statistics. The counts are recomputed from
// the current state before returning.
func (m *DispatcherModule) Stats() *DispatchStats {
	m.recountStats()
	return &m.stats
}

// recountStats walks every set and recomputes the active/inactive counts.
// Lock ordering is module mu -> set mu, matching the rest of the module.
func (m *DispatcherModule) recountStats() {
	m.mu.RLock()
	var active, inactive int64
	for _, s := range m.sets {
		s.mu.RLock()
		for _, d := range s.Destinations {
			if d.Active {
				active++
			} else {
				inactive++
			}
		}
		s.mu.RUnlock()
	}
	m.mu.RUnlock()
	m.stats.ActiveDestinations.Store(active)
	m.stats.InactiveDestinations.Store(inactive)
}

// selectRoundRobin returns the next active destination in round-robin
// order and advances the cursor.
func (m *DispatcherModule) selectRoundRobin(s *DispatchSet) *Destination {
	s.mu.Lock()
	defer s.mu.Unlock()
	active := make([]*Destination, 0, len(s.Destinations))
	for _, d := range s.Destinations {
		if d.Active {
			active = append(active, d)
		}
	}
	if len(active) == 0 {
		return nil
	}
	sel := active[s.rrIndex%len(active)]
	s.rrIndex = (s.rrIndex + 1) % len(active)
	return sel
}

// selectFromActive gathers the active destinations of s (under a read
// lock) and applies pick to choose one. The pick function only reads
// fields that are immutable after a destination is added, so it is safe
// to call without holding the lock.
func selectFromActive(s *DispatchSet, pick func([]*Destination) *Destination) *Destination {
	s.mu.RLock()
	active := make([]*Destination, 0, len(s.Destinations))
	for _, d := range s.Destinations {
		if d.Active {
			active = append(active, d)
		}
	}
	s.mu.RUnlock()
	return pick(active)
}

// selectRandom returns a uniformly random active destination.
func selectRandom(active []*Destination) *Destination {
	if len(active) == 0 {
		return nil
	}
	return active[rand.Intn(len(active))]
}

// selectWeight returns a destination chosen with probability proportional
// to its weight. A non-positive weight is treated as 1.
func selectWeight(active []*Destination) *Destination {
	if len(active) == 0 {
		return nil
	}
	total := 0
	for _, d := range active {
		w := d.Weight
		if w <= 0 {
			w = 1
		}
		total += w
	}
	if total == 0 {
		return nil
	}
	r := rand.Intn(total)
	for _, d := range active {
		w := d.Weight
		if w <= 0 {
			w = 1
		}
		r -= w
		if r < 0 {
			return d
		}
	}
	return active[len(active)-1]
}

// selectPriority returns the active destination with the lowest priority
// value (Kamailio treats lower numeric priority as higher precedence).
func selectPriority(active []*Destination) *Destination {
	if len(active) == 0 {
		return nil
	}
	best := active[0]
	for _, d := range active[1:] {
		if d.Priority < best.Priority {
			best = d
		}
	}
	return best
}

// selectHash returns a destination chosen by hashing the message Call-ID
// (falling back to the Request-URI, then a constant) so that the same
// message always maps to the same destination.
func selectHash(active []*Destination, msg *parser.SIPMsg) *Destination {
	if len(active) == 0 {
		return nil
	}
	var key string
	if msg != nil {
		if msg.CallID != nil {
			key = msg.CallID.Body.String()
		}
		if key == "" && msg.FirstLine != nil && msg.FirstLine.Req != nil {
			key = msg.FirstLine.Req.URI.String()
		}
	}
	if key == "" {
		key = "kamailio-go-dispatcher"
	}
	sum := sha1.Sum([]byte(key))
	v := binary.BigEndian.Uint64(sum[:8])
	return active[v%uint64(len(active))]
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultDispatch *DispatcherModule
)

// DefaultDispatcher returns the process-wide DispatcherModule, creating
// one on first use.
func DefaultDispatcher() *DispatcherModule {
	defaultMu.RLock()
	d := defaultDispatch
	defaultMu.RUnlock()
	if d != nil {
		return d
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultDispatch == nil {
		defaultDispatch = NewDispatcherModule()
	}
	return defaultDispatch
}

// Init (re)initialises the process-wide DispatcherModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultDispatch = NewDispatcherModule()
}
