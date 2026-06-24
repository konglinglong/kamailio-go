// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Topos module - topology hiding / storage.
 * Port of the kamailio topos module (src/modules/topos).
 *
 * The topos module records the topology of a SIP dialog as it transits
 * the proxy and hides the original From / To / Request-URI from
 * downstream peers. On the return path the original values are
 * restored before the message is processed internally.
 *
 * Each recorded dialog is identified by its Call-ID and From-tag and is
 * kept in an in-memory store. Expired records are reclaimed by
 * CleanupExpired.
 *
 * It is safe for concurrent use.
 */

package topos

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// DefaultTTL is the default lifetime of a topology record, after which
// CleanupExpired will reclaim it.
const DefaultTTL = 2 * time.Hour

// TopoRecord captures the original and hidden topology of a single
// SIP dialog leg.
type TopoRecord struct {
	CallID       string
	FromTag      string
	ToTag        string
	Direction    string
	OriginalFrom string
	OriginalTo   string
	OriginalRURI string
	HiddenFrom   string
	HiddenTo     string
	HiddenRURI   string

	// createdAt tracks when the record was stored, used by CleanupExpired.
	// It is unexported to keep the public API stable.
	createdAt time.Time
}

// ToposModule records and restores SIP dialog topology.
type ToposModule struct {
	mu      sync.RWMutex
	records map[string]*TopoRecord
	counter atomic.Uint64
	ttl     time.Duration
}

// New creates a ToposModule configured with the default TTL.
func New() *ToposModule {
	return &ToposModule{
		records: make(map[string]*TopoRecord),
		ttl:     DefaultTTL,
	}
}

// Record captures the topology of msg, replaces its From / To / RURI
// with hidden values, stores the record and returns it. Returns nil
// when msg is nil or has no Call-ID.
//
//	C: tops_record() / tops_store()
func (m *ToposModule) Record(msg *parser.SIPMsg, direction string) *TopoRecord {
	if msg == nil {
		return nil
	}
	callID := headerBody(msg, msg.CallID, parser.HdrCallID)
	if callID == "" {
		return nil
	}
	fromBody := headerBody(msg, msg.From, parser.HdrFrom)
	toBody := headerBody(msg, msg.To, parser.HdrTo)
	ruri := requestURI(msg)

	fromTag := extractTag(fromBody)
	toTag := extractTag(toBody)

	rec := &TopoRecord{
		CallID:       callID,
		FromTag:      fromTag,
		ToTag:        toTag,
		Direction:    direction,
		OriginalFrom: fromBody,
		OriginalTo:   toBody,
		OriginalRURI: ruri,
		HiddenFrom:   m.hideAddr(fromBody, "from"),
		HiddenTo:     m.hideAddr(toBody, "to"),
		HiddenRURI:   m.hideRURI(ruri),
		createdAt:    time.Now(),
	}

	// Replace the on-message values with the hidden ones.
	if msg.From != nil {
		msg.From.Body = str.Mk(rec.HiddenFrom)
	}
	if msg.To != nil {
		msg.To.Body = str.Mk(rec.HiddenTo)
	}
	if ruri != "" && msg.FirstLine != nil && msg.FirstLine.Req != nil {
		msg.FirstLine.Req.URI = str.Mk(rec.HiddenRURI)
	}

	key := recordKey(callID, fromTag)
	m.mu.Lock()
	if m.records == nil {
		m.records = make(map[string]*TopoRecord)
	}
	m.records[key] = rec
	m.mu.Unlock()
	return rec
}

// Restore looks up the topology record for msg's dialog and writes the
// original From / To / RURI back onto the message. Returns the matched
// record, or an error when msg is nil or no record exists.
//
//	C: tops_restore()
func (m *ToposModule) Restore(msg *parser.SIPMsg) (*TopoRecord, error) {
	if msg == nil {
		return nil, fmt.Errorf("topos: nil message")
	}
	callID := headerBody(msg, msg.CallID, parser.HdrCallID)
	fromTag := extractTag(headerBody(msg, msg.From, parser.HdrFrom))
	key := recordKey(callID, fromTag)

	m.mu.RLock()
	rec, ok := m.records[key]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("topos: no record for call-id %q tag %q", callID, fromTag)
	}

	if msg.From != nil {
		msg.From.Body = str.Mk(rec.OriginalFrom)
	}
	if msg.To != nil {
		msg.To.Body = str.Mk(rec.OriginalTo)
	}
	if rec.OriginalRURI != "" && msg.FirstLine != nil && msg.FirstLine.Req != nil {
		msg.FirstLine.Req.URI = str.Mk(rec.OriginalRURI)
	}
	return rec, nil
}

// GetRecord returns the topology record for the given dialog, or nil
// when no record exists.
func (m *ToposModule) GetRecord(callID, fromTag string) *TopoRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.records[recordKey(callID, fromTag)]
}

// DeleteRecord removes every record whose Call-ID matches callID.
func (m *ToposModule) DeleteRecord(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, rec := range m.records {
		if rec.CallID == callID {
			delete(m.records, key)
		}
	}
}

// Count returns the number of stored topology records.
func (m *ToposModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.records)
}

// CleanupExpired removes records older than the module TTL.
func (m *ToposModule) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for key, rec := range m.records {
		if now.Sub(rec.createdAt) > m.ttl {
			delete(m.records, key)
		}
	}
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// hideAddr returns a masked From/To header body that preserves the tag
// (if any) but replaces the URI with an opaque value.
func (m *ToposModule) hideAddr(body, role string) string {
	tag := extractTag(body)
	n := m.counter.Add(1)
	hidden := fmt.Sprintf("<sip:topos%d@hidden.local>", n)
	if tag != "" {
		hidden += ";tag=" + tag
	}
	return hidden
}

// hideRURI returns a masked request URI.
func (m *ToposModule) hideRURI(ruri string) string {
	if ruri == "" {
		return ""
	}
	n := m.counter.Add(1)
	return fmt.Sprintf("sip:topos%d@hidden.local", n)
}

// recordKey produces a stable key from a Call-ID and From-tag.
func recordKey(callID, fromTag string) string {
	return callID + "|" + fromTag
}

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

// requestURI returns the request URI string from msg's request line.
func requestURI(msg *parser.SIPMsg) string {
	if msg == nil || msg.FirstLine == nil || msg.FirstLine.Req == nil {
		return ""
	}
	return msg.FirstLine.Req.URI.String()
}

// extractTag scans a From/To header body and returns the value of the
// "tag" parameter, or the empty string if not present.
func extractTag(body string) string {
	if body == "" {
		return ""
	}
	lower := strings.ToLower(body)
	idx := strings.Index(lower, ";tag=")
	if idx < 0 {
		// tolerate a leading "tag=" (no semicolon)
		if strings.HasPrefix(lower, "tag=") {
			rest := body[4:]
			if semi := strings.IndexByte(rest, ';'); semi >= 0 {
				return strings.TrimSpace(rest[:semi])
			}
			return strings.TrimSpace(rest)
		}
		return ""
	}
	rest := body[idx+len(";tag="):]
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		return strings.TrimSpace(rest[:semi])
	}
	return strings.TrimSpace(rest)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultTM *ToposModule
)

// DefaultTopos returns the process-wide ToposModule, creating it on first use.
func DefaultTopos() *ToposModule {
	defaultMu.RLock()
	m := defaultTM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTM == nil {
		defaultTM = New()
	}
	return defaultTM
}

// Init (re)initialises the process-wide ToposModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTM = New()
}

// Record is the package-level wrapper around DefaultTopos().Record.
func Record(msg *parser.SIPMsg, direction string) *TopoRecord {
	return DefaultTopos().Record(msg, direction)
}

// Restore is the package-level wrapper around DefaultTopos().Restore.
func Restore(msg *parser.SIPMsg) (*TopoRecord, error) {
	return DefaultTopos().Restore(msg)
}

// GetRecord is the package-level wrapper around DefaultTopos().GetRecord.
func GetRecord(callID, fromTag string) *TopoRecord {
	return DefaultTopos().GetRecord(callID, fromTag)
}

// DeleteRecord is the package-level wrapper around DefaultTopos().DeleteRecord.
func DeleteRecord(callID string) {
	DefaultTopos().DeleteRecord(callID)
}

// Count is the package-level wrapper around DefaultTopos().Count.
func Count() int {
	return DefaultTopos().Count()
}

// CleanupExpired is the package-level wrapper around DefaultTopos().CleanupExpired.
func CleanupExpired() {
	DefaultTopos().CleanupExpired()
}
