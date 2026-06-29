// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Keepalive module - SIP keepalive probing.
 * Port of the kamailio keepalive module (src/modules/keepalive).
 *
 * The keepalive module periodically sends SIP OPTIONS (or CR-LF
 * keepalives) to a set of registered contacts and tracks whether each
 * target is still responding. Targets that fail too many consecutive
 * probes are marked inactive.
 *
 * It is safe for concurrent use. The periodic loop started by Start
 * runs in its own goroutine and is stopped by Stop.
 */

package keepalive

import (
	"errors"
	"sync"
	"time"
)

// MaxFailures is the number of consecutive failed probes after which a
// target is marked inactive.
const MaxFailures = 3

// KeepaliveTarget describes a single keepalive destination.
type KeepaliveTarget struct {
	Contact      string
	Socket       string
	LastSent     time.Time
	LastResponse time.Time
	Failures     int
	Active       bool
}

// KeepaliveModule maintains a set of keepalive targets and an optional
// periodic probing loop.
type KeepaliveModule struct {
	mu       sync.RWMutex
	targets  map[string]*KeepaliveTarget
	stopCh   chan struct{}
	running  bool
	interval time.Duration
}

// New creates a KeepaliveModule with empty target storage.
func New() *KeepaliveModule {
	return &KeepaliveModule{
		targets: make(map[string]*KeepaliveTarget),
	}
}

// AddTarget registers a keepalive target for the given contact and
// socket. If a target for the contact already exists its socket is
// updated and the existing target is returned. New targets start in
// the Active state with zero failures.
//
//	C: ka_add_target()
func (m *KeepaliveModule) AddTarget(contact, socket string) *KeepaliveTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.targets == nil {
		m.targets = make(map[string]*KeepaliveTarget)
	}
	if tgt, ok := m.targets[contact]; ok {
		tgt.Socket = socket
		return tgt
	}
	tgt := &KeepaliveTarget{
		Contact: contact,
		Socket:  socket,
		Active:  true,
	}
	m.targets[contact] = tgt
	return tgt
}

// RemoveTarget removes the target identified by contact. Returns true
// when a target was removed.
//
//	C: ka_del_target()
func (m *KeepaliveModule) RemoveTarget(contact string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.targets[contact]; !ok {
		return false
	}
	delete(m.targets, contact)
	return true
}

// SendKeepalive records a keepalive probe for the given target by
// updating its LastSent timestamp. Returns an error when target is nil.
//
//	C: ka_send_keepalive()
func (m *KeepaliveModule) SendKeepalive(target *KeepaliveTarget) error {
	if target == nil {
		return errors.New("keepalive: nil target")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	target.LastSent = time.Now()
	return nil
}

// ProcessResponse records the outcome of a keepalive probe for the
// target identified by contact. On success the failure counter is
// reset and the target is marked active; on failure the counter is
// incremented and the target is marked inactive once it exceeds
// MaxFailures. Unknown contacts are silently ignored.
//
//	C: ka_process_response()
func (m *KeepaliveModule) ProcessResponse(contact string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tgt, exists := m.targets[contact]
	if !exists {
		return
	}
	tgt.LastResponse = time.Now()
	if ok {
		tgt.Failures = 0
		tgt.Active = true
		return
	}
	tgt.Failures++
	if tgt.Failures >= MaxFailures {
		tgt.Active = false
	}
}

// GetTarget returns the target for the given contact, or nil.
func (m *KeepaliveModule) GetTarget(contact string) *KeepaliveTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.targets[contact]
}

// ListTargets returns a slice of all registered targets.
func (m *KeepaliveModule) ListTargets() []*KeepaliveTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*KeepaliveTarget, 0, len(m.targets))
	for _, t := range m.targets {
		out = append(out, t)
	}
	return out
}

// Count returns the number of registered targets.
func (m *KeepaliveModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.targets)
}

// ActiveCount returns the number of targets whose Active flag is set.
func (m *KeepaliveModule) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, t := range m.targets {
		if t.Active {
			count++
		}
	}
	return count
}

// Start launches a goroutine that sends a keepalive probe to every
// registered target at the given interval. It is safe to call when
// already running: the previous loop is stopped first.
//
//	C: ka_start()
func (m *KeepaliveModule) Start(interval time.Duration) {
	m.Stop()
	if interval <= 0 {
		return
	}
	m.mu.Lock()
	m.interval = interval
	m.stopCh = make(chan struct{})
	m.running = true
	stopCh := m.stopCh
	m.mu.Unlock()
	go m.keepaliveLoop(interval, stopCh)
}

// Stop terminates the periodic keepalive loop started by Start. It is
// idempotent and safe to call when no loop is running.
//
//	C: ka_stop()
func (m *KeepaliveModule) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	stopCh := m.stopCh
	m.stopCh = nil
	m.mu.Unlock()
	if stopCh != nil {
		close(stopCh)
	}
}

// LastSentTime returns the LastSent timestamp of the target identified
// by contact, protected by the module lock. It is intended for
// concurrent observation of the probing loop.
func (m *KeepaliveModule) LastSentTime(contact string) time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t := m.targets[contact]; t != nil {
		return t.LastSent
	}
	return time.Time{}
}

// keepaliveLoop ticks at interval and sends a probe to every target
// until stopCh is closed.
func (m *KeepaliveModule) keepaliveLoop(interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, tgt := range m.snapshotTargets() {
				m.SendKeepalive(tgt)
			}
		case <-stopCh:
			return
		}
	}
}

// snapshotTargets returns a copy of the current target slice, taken
// under the module read lock so the loop can iterate without holding
// the lock during SendKeepalive.
func (m *KeepaliveModule) snapshotTargets() []*KeepaliveTarget {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*KeepaliveTarget, 0, len(m.targets))
	for _, t := range m.targets {
		out = append(out, t)
	}
	return out
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultKM *KeepaliveModule
)

// DefaultKeepalive returns the process-wide KeepaliveModule, creating it
// on first use.
func DefaultKeepalive() *KeepaliveModule {
	defaultMu.RLock()
	m := defaultKM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultKM == nil {
		defaultKM = New()
	}
	return defaultKM
}

// Init (re)initialises the process-wide KeepaliveModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple
// times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultKM != nil {
		defaultKM.Stop()
	}
	defaultKM = New()
}

// AddTarget is the package-level wrapper around DefaultKeepalive().AddTarget.
func AddTarget(contact, socket string) *KeepaliveTarget {
	return DefaultKeepalive().AddTarget(contact, socket)
}

// RemoveTarget is the package-level wrapper around DefaultKeepalive().RemoveTarget.
func RemoveTarget(contact string) bool {
	return DefaultKeepalive().RemoveTarget(contact)
}

// SendKeepalive is the package-level wrapper around DefaultKeepalive().SendKeepalive.
func SendKeepalive(target *KeepaliveTarget) error {
	return DefaultKeepalive().SendKeepalive(target)
}

// ProcessResponse is the package-level wrapper around DefaultKeepalive().ProcessResponse.
func ProcessResponse(contact string, ok bool) {
	DefaultKeepalive().ProcessResponse(contact, ok)
}

// GetTarget is the package-level wrapper around DefaultKeepalive().GetTarget.
func GetTarget(contact string) *KeepaliveTarget {
	return DefaultKeepalive().GetTarget(contact)
}

// ListTargets is the package-level wrapper around DefaultKeepalive().ListTargets.
func ListTargets() []*KeepaliveTarget {
	return DefaultKeepalive().ListTargets()
}

// Count is the package-level wrapper around DefaultKeepalive().Count.
func Count() int {
	return DefaultKeepalive().Count()
}

// ActiveCount is the package-level wrapper around DefaultKeepalive().ActiveCount.
func ActiveCount() int {
	return DefaultKeepalive().ActiveCount()
}

// Start is the package-level wrapper around DefaultKeepalive().Start.
func Start(interval time.Duration) {
	DefaultKeepalive().Start(interval)
}

// Stop is the package-level wrapper around DefaultKeepalive().Stop.
func Stop() {
	DefaultKeepalive().Stop()
}
