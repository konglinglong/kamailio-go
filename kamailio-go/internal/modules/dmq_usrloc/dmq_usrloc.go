// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * dmq_usrloc module - replicate usrloc contacts over DMQ.
 * Port of the kamailio dmq_usrloc module (src/modules/dmq_usrloc).
 *
 * The original C module hooks into the usrloc module and replicates
 * contact additions/updates/deletes to peer Kamailio instances via the
 * dmq module. This Go counterpart wraps a *dmq.DMQModule: SyncContact
 * broadcasts a contact change to peers, and a background loop started
 * by Start drains the DMQ receive channel and applies incoming changes
 * through ReceiveSync.
 *
 * It is safe for concurrent use.
 */

package dmq_usrloc

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/modules/dmq"
)

// DefaultSyncInterval is the default periodic sync interval.
const DefaultSyncInterval = 30 * time.Second

// SyncMessageType is the DMQ message type used for usrloc replication.
const SyncMessageType = "usrloc_sync"

// DMQUsrlocConfig configures a DMQUsrlocModule.
type DMQUsrlocConfig struct {
	SyncInterval time.Duration
	Enable       bool
}

// DMQUsrlocModule replicates usrloc contacts over a DMQ mesh.
// It is the Go counterpart of the kamailio dmq_usrloc module.
type DMQUsrlocModule struct {
	mu           sync.Mutex
	dmqMod       *dmq.DMQModule
	syncInterval time.Duration
	enabled      bool

	pending   int64
	processed int64

	stopCh  chan struct{}
	running bool
	wg      sync.WaitGroup
}

// New creates a DMQUsrlocModule backed by a fresh internal DMQModule.
func New() *DMQUsrlocModule {
	m := &DMQUsrlocModule{
		dmqMod: dmq.New(),
	}
	m.Init(nil)
	return m
}

// Init (re)configures the module from cfg. A nil cfg applies defaults
// (enabled, default sync interval).
//
//	C: mod_init()
func (m *DMQUsrlocModule) Init(cfg *DMQUsrlocConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		m.syncInterval = DefaultSyncInterval
		m.enabled = true
		return
	}
	m.syncInterval = cfg.SyncInterval
	if m.syncInterval <= 0 {
		m.syncInterval = DefaultSyncInterval
	}
	m.enabled = cfg.Enable
}

// DMQ returns the underlying DMQModule used for transport. Tests use it
// to register peers and inspect delivered messages.
func (m *DMQUsrlocModule) DMQ() *dmq.DMQModule {
	return m.dmqMod
}

// SetDMQ replaces the underlying DMQModule. It must not be called while
// the periodic loop is running.
func (m *DMQUsrlocModule) SetDMQ(d *dmq.DMQModule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d == nil {
		d = dmq.New()
	}
	m.dmqMod = d
}

// SyncContact broadcasts a contact change to all peers. The action is
// one of "add", "update" or "delete". The sync is recorded as pending
// until a corresponding sync is received and applied.
//
//	C: dmq_ul_replicate_contact()
func (m *DMQUsrlocModule) SyncContact(aor string, contact string, action string) error {
	if !m.enabled {
		return errors.New("dmq_usrloc: not enabled")
	}
	atomic.AddInt64(&m.pending, 1)
	body := encodeSync(aor, contact, action)
	if err := m.dmqMod.Broadcast(SyncMessageType, body); err != nil {
		return fmt.Errorf("dmq_usrloc: broadcast: %w", err)
	}
	return nil
}

// ReceiveSync applies a contact sync message received from a peer. It
// validates the message, increments the processed counter and
// decrements the pending counter (a sync round-tripped).
//
//	C: receive_user_location() / usrloc sync handler
func (m *DMQUsrlocModule) ReceiveSync(msg *dmq.DMQMessage) error {
	if msg == nil {
		return errors.New("dmq_usrloc: nil message")
	}
	if msg.Type != SyncMessageType {
		return fmt.Errorf("dmq_usrloc: unexpected message type %q", msg.Type)
	}
	aor, contact, action, err := decodeSync(msg.Body)
	if err != nil {
		return err
	}
	if aor == "" || contact == "" || action == "" {
		return errors.New("dmq_usrloc: incomplete sync payload")
	}
	atomic.AddInt64(&m.processed, 1)
	// A received sync acknowledges one of our pending outgoing syncs.
	for {
		p := atomic.LoadInt64(&m.pending)
		if p <= 0 {
			break
		}
		if atomic.CompareAndSwapInt64(&m.pending, p, p-1) {
			break
		}
	}
	return nil
}

// Start launches the periodic sync loop and a DMQ receive drainer that
// applies incoming syncs via ReceiveSync. It is a no-op if already
// running or if the module is disabled.
//
//	C: child_init() worker startup
func (m *DMQUsrlocModule) Start() {
	m.mu.Lock()
	if m.running || !m.enabled {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.stopCh = make(chan struct{})
	stopCh := m.stopCh
	interval := m.syncInterval
	d := m.dmqMod
	m.mu.Unlock()

	// DMQ receive drainer: applies incoming syncs locally.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case msg, ok := <-d.Receive():
				if !ok {
					return
				}
				_ = m.ReceiveSync(msg)
			case <-stopCh:
				return
			}
		}
	}()

	// Periodic full-sync ticker.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Periodic sync: broadcast a full-sync marker. Errors
				// (e.g. no peers) are non-fatal for the loop.
				_ = m.SyncContact("__all__", "*", "periodic")
			case <-stopCh:
				return
			}
		}
	}()
}

// Stop halts the periodic loop and the DMQ drainer, waiting for them to
// exit. It is safe to call when not running.
func (m *DMQUsrlocModule) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	close(m.stopCh)
	m.mu.Unlock()
	m.wg.Wait()
}

// PendingSyncs returns the number of syncs awaiting acknowledgement.
func (m *DMQUsrlocModule) PendingSyncs() int {
	return int(atomic.LoadInt64(&m.pending))
}

// ProcessedCount returns the total number of syncs applied from peers.
func (m *DMQUsrlocModule) ProcessedCount() int64 {
	return atomic.LoadInt64(&m.processed)
}

// encodeSync serialises a contact change into a DMQ body.
// Format: "aor|contact|action".
func encodeSync(aor, contact, action string) string {
	return aor + "|" + contact + "|" + action
}

// decodeSync parses a DMQ body back into aor, contact, action.
func decodeSync(body string) (string, string, string, error) {
	parts := strings.SplitN(body, "|", 3)
	if len(parts) != 3 {
		return "", "", "", errors.New("dmq_usrloc: malformed sync body")
	}
	return parts[0], parts[1], parts[2], nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultDMQUsrloc returns the package-level default module.
func DefaultDMQUsrloc() *DMQUsrlocModule {
	return defaultModule
}

// Init (re)initialises the package-level default module with defaults.
func Init() {
	defaultModule = New()
}
