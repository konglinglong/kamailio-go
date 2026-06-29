// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * I-CSCF S-CSCF capability table and per-call-id candidate list.
 * Port of the kamailio ims_icscf module's scscf_list.c.
 *
 * Two distinct data structures live here:
 *
 *  1. SCSCFCapability: the static S-CSCF catalogue loaded once at
 *     start-up (C: SCSCF_Capabilities[] from db.c). Each entry has an
 *     ID, an S-CSCF URI name, and a list of mandatory+optional capability
 *     codes. There is NO capacity and NO priority field — the C source
 *     never had them; the old Go stub invented them and was wrong.
 *
 *  2. SCSCFCandidateList: a transient list of S-CSCF candidates built by
 *     the UAA/LIA callback for a single REGISTER/INVITE flow, keyed by
 *     the SIP Call-ID. Entries carry a score computed by MatchScore()
 *     (mandatory capabilities must all be satisfied or the S-CSCF is
 *     rejected; each matching optional capability adds 1). The list is
 *     taken from in-order by Select() and is purged by SweepExpired()
 *     after scscf_entry_expiry (default 300 s) of inactivity.
 *
 * The capability-matching algorithm mirrors I_get_capab_match() and
 * I_get_capab_ordered() in the C source: preferred S-CSCF names get
 * INT_MAX - i scores so they sort first, and an explicit Server-Name
 * returned by the HSS gets INT_MAX.
 *
 * It is safe for concurrent use.
 */

package icscf

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrSCSCFNotFound is returned when no S-CSCF with the given ID is
	// registered.
	ErrSCSCFNotFound = errors.New("icscf: s-cscf not found")
	// ErrCandidateListEmpty is returned by Select when the call-id's
	// candidate list is empty or has been exhausted.
	ErrCandidateListEmpty = errors.New("icscf: candidate list empty")
	// ErrNoCandidateList is returned when no candidate list has been
	// built for the given call-id.
	ErrNoCandidateList = errors.New("icscf: no candidate list for call-id")
)

// ---------------------------------------------------------------------------
// SCSCFCapability — static S-CSCF catalogue entry (C: scscf_capabilities)
// ---------------------------------------------------------------------------

// SCSCFCapability describes one S-CSCF the I-CSCF is willing to route to.
// Mirrors the C `scscf_capabilities` struct in scscf_list.h.
type SCSCFCapability struct {
	// ID is the numeric identifier from the s_cscf DB table.
	ID int
	// Name is the S-CSCF URI (e.g. "sip:scscf1.home1.net").
	Name string
	// MandatoryCaps are capability codes that MUST be present in the
	// I-CSCF's required-capability set for an S-CSCF to be selectable.
	MandatoryCaps []int
	// OptionalCaps are capability codes that boost the S-CSCF's score
	// by 1 for each one matched.
	OptionalCaps []int
}

// MatchScore returns the ranking score for this S-CSCF against the
// required-capability set carried in the UAR/LIA from the HSS.
//
// Returns -1 when any mandatory capability is missing (the S-CSCF is
// rejected). Otherwise returns the count of optional capabilities that
// matched the required set.
//
//	C: I_get_capab_match()
func (c *SCSCFCapability) MatchScore(requiredCaps []int) int {
	if c == nil {
		return -1
	}
	required := toSet(requiredCaps)
	// Mandatory must all be present.
	for _, m := range c.MandatoryCaps {
		if !required[m] {
			return -1
		}
	}
	// Each optional that is also required adds 1 to the score.
	score := 0
	for _, o := range c.OptionalCaps {
		if required[o] {
			score++
		}
	}
	return score
}

// ---------------------------------------------------------------------------
// SCSCFCandidate — one entry in the per-call-id candidate list
// ---------------------------------------------------------------------------

// SCSCFCandidate is a single S-CSCF candidate selected for a particular
// call. Mirrors the C `scscf_entry` struct.
type SCSCFCandidate struct {
	// Name is the S-CSCF URI; on originating requests the C source
	// appends ";orig" so the S-CSCF can distinguish orig/term routing.
	Name string
	// Score is the rank produced by MatchScore or by the preferred-list
	// / explicit-Server-Name policy in BuildCandidateList.
	Score int
	// StartTime is when the candidate was added to the list. Used by
	// SweepExpired to drop entries older than scscf_entry_expiry.
	StartTime time.Time
}

// SCSCFCandidateList is the ordered set of candidates for one call-id.
type SCSCFCandidateList struct {
	// CallID is the SIP Call-ID this list belongs to.
	CallID string
	// Candidates is sorted by Score descending; Select pops index 0.
	Candidates []SCSCFCandidate
	// CreatedAt is when the list was first added. Used for expiry.
	CreatedAt time.Time
	// Orig indicates this is an originating-side candidate list (the
	// C source appends ";orig" to the S-CSCF name when orig=1).
	Orig bool
}

// ---------------------------------------------------------------------------
// SCSCFTable — the static catalogue + per-call-id lists
// ---------------------------------------------------------------------------

// DefaultEntryExpiry is the default lifetime of a per-call-id candidate
// list (mirrors the C `scscf_entry_expiry` modparam, default 300 s).
const DefaultEntryExpiry = 300 * time.Second

// SCSCFTable combines the static S-CSCF catalogue with the per-call-id
// candidate-list hash. It corresponds to the C globals SCSCF_Capabilities[]
// and i_hash_table[].
type SCSCFTable struct {
	mu sync.RWMutex

	// Catalogue of S-CSCFs (loaded from DB at init time).
	capabilities []SCSCFCapability

	// Per-call-id candidate lists. Mirrors i_hash_table[].
	lists map[string]*SCSCFCandidateList

	// entryExpiry controls how long an idle list lingers before SweepExpired
	// removes it. Mirrors scscf_entry_expiry modparam.
	entryExpiry time.Duration

	// preferred is the ordered list of S-CSCF URIs the operator wants
	// preferred above score ordering. Mirrors preferred_scscf_uri modparam.
	preferred []string

	// usePreferred mirrors use_preferred_scscf_uri modparam.
	usePreferred bool
}

// NewSCSCFTable returns an empty S-CSCF table with the default entry
// expiry (300 s) and no preferred S-CSCFs.
func NewSCSCFTable() *SCSCFTable {
	return &SCSCFTable{
		lists:       make(map[string]*SCSCFCandidateList),
		entryExpiry: DefaultEntryExpiry,
	}
}

// SetEntryExpiry replaces the per-call-id list expiry duration.
// Mirrors setting the scscf_entry_expiry modparam at init time.
func (t *SCSCFTable) SetEntryExpiry(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d > 0 {
		t.entryExpiry = d
	}
}

// EntryExpiry returns the currently configured list expiry.
func (t *SCSCFTable) EntryExpiry() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.entryExpiry
}

// SetPreferredSCSCFs configures the preferred S-CSCF list and the
// use_preferred_scscf_uri flag. When usePreferred is true and the HSS
// returns no explicit Server-Name, candidates matching the preferred
// list are ranked first (with score INT_MAX - i so that index 0 wins).
func (t *SCSCFTable) SetPreferredSCSCFs(names []string, usePreferred bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.preferred = append([]string(nil), names...)
	t.usePreferred = usePreferred
}

// PreferredSCSCFs returns a snapshot of the preferred list.
func (t *SCSCFTable) PreferredSCSCFs() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]string(nil), t.preferred...)
}

// UsePreferred reports whether preferred-SCSCF selection is enabled.
func (t *SCSCFTable) UsePreferred() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.usePreferred
}

// ---------------------------------------------------------------------------
// Catalogue operations (mirrors db.c: ims_icscf_db_get_scscf + _capabilities)
// ---------------------------------------------------------------------------

// LoadSCSCFs replaces the static catalogue. Mirrors I_get_capabilities()
// loading s_cscf + s_cscf_capabilities from the DB at mod_init time.
// Callers in production would translate rows from the s_cscf DB table
// into []SCSCFCapability before invoking this.
func (t *SCSCFTable) LoadSCSCFs(caps []SCSCFCapability) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Deep copy to insulate the table from caller mutations.
	t.capabilities = make([]SCSCFCapability, len(caps))
	for i := range caps {
		t.capabilities[i] = caps[i].clone()
	}
}

// AddSCSCF appends one S-CSCF to the catalogue. Useful when populating
// the table incrementally (e.g. during tests).
func (t *SCSCFTable) AddSCSCF(c SCSCFCapability) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capabilities = append(t.capabilities, c.clone())
}

// SCSCFs returns a snapshot of the static catalogue.
func (t *SCSCFTable) SCSCFs() []SCSCFCapability {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]SCSCFCapability, len(t.capabilities))
	for i := range t.capabilities {
		out[i] = t.capabilities[i].clone()
	}
	return out
}

// FindSCSCF returns the S-CSCF with the given URI name, or nil.
func (t *SCSCFTable) FindSCSCF(name string) *SCSCFCapability {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := range t.capabilities {
		if t.capabilities[i].Name == name {
			c := t.capabilities[i].clone()
			return &c
		}
	}
	return nil
}

// SCSCFCount returns the number of S-CSCFs in the catalogue.
func (t *SCSCFTable) SCSCFCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.capabilities)
}

// ---------------------------------------------------------------------------
// Candidate-list construction (mirrors I_get_capab_ordered)
// ---------------------------------------------------------------------------

// BuildCandidateList computes the ranked S-CSCF candidate list for the
// given call-id using the static catalogue, the HSS-required capability
// set, and (optionally) an explicit Server-Name returned by the HSS.
//
// When explicitServerName is non-empty it gets score math.MaxInt32 and
// sorts ahead of everything else (mirrors the C INT_MAX rule for the
// Server-Name AVP). When usePreferred is true, preferred S-CSCFs that
// match the catalogue get math.MaxInt32 - i so they sort second.
//
// requiredCaps corresponds to the Capabilities AVP from the UAA/LIA.
// orig indicates the originating side; when true, candidates matching
// the preferred list have ";orig" appended to their Name (mirrors the
// ";orig" suffix in new_scscf_entry()).
//
// The resulting list replaces any previous list for the same call-id.
//
//	C: I_get_capab_ordered()
func (t *SCSCFTable) BuildCandidateList(
	callID string, requiredCaps []int, explicitServerName string, orig bool,
) *SCSCFCandidateList {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	list := &SCSCFCandidateList{
		CallID:     callID,
		Orig:       orig,
		CreatedAt:  now,
	}

	// 1) If the HSS named a specific server, that's the only candidate.
	if explicitServerName != "" {
		list.Candidates = []SCSCFCandidate{{
			Name:      explicitServerName,
			Score:     math.MaxInt32,
			StartTime: now,
		}}
		t.lists[callID] = list
		return list
	}

	// 2) Otherwise walk the catalogue, scoring each S-CSCF.
	type scored struct {
		cap   SCSCFCapability
		score int
	}
	var picks []scored
	for i := range t.capabilities {
		c := &t.capabilities[i]
		s := c.MatchScore(requiredCaps)
		if s < 0 {
			continue
		}
		picks = append(picks, scored{*c, s})
	}

	// 3) Apply preferred-SCSCF bonus (mirrors preferred_scscf_uri modparam).
	if t.usePreferred && len(t.preferred) > 0 {
		for i := range picks {
			for j, p := range t.preferred {
				if picks[i].cap.Name == p {
					// Higher index in preferred list -> lower bonus.
					picks[i].score = math.MaxInt32 - j
					break
				}
			}
		}
	}

	// 4) Sort by score descending; ties broken by catalogue order (stable).
	sort.SliceStable(picks, func(i, j int) bool {
		return picks[i].score > picks[j].score
	})

	// 5) Materialise the candidates, appending ";orig" when orig=true and
	// the S-CSCF was selected via the preferred list.
	for i := range picks {
		name := picks[i].cap.Name
		if orig {
			name = name + ";orig"
		}
		list.Candidates = append(list.Candidates, SCSCFCandidate{
			Name:      name,
			Score:     picks[i].score,
			StartTime: now,
		})
	}

	t.lists[callID] = list
	return list
}

// CandidateList returns the list for callID without removing it.
// Returns nil when no list has been built.
func (t *SCSCFTable) CandidateList(callID string) *SCSCFCandidateList {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if l, ok := t.lists[callID]; ok {
		out := *l
		out.Candidates = append([]SCSCFCandidate(nil), l.Candidates...)
		return &out
	}
	return nil
}

// Select returns and removes the highest-scored candidate from the
// call-id's list. The list itself is left in place (so subsequent
// failures can call Select again to try the next S-CSCF). Returns
// ErrNoCandidateList when the call-id has no list, or
// ErrCandidateListEmpty when the list has been exhausted.
//
//	C: take_scscf_entry()
func (t *SCSCFTable) Select(callID string) (SCSCFCandidate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list, ok := t.lists[callID]
	if !ok {
		return SCSCFCandidate{}, ErrNoCandidateList
	}
	if len(list.Candidates) == 0 {
		return SCSCFCandidate{}, ErrCandidateListEmpty
	}
	c := list.Candidates[0]
	list.Candidates = list.Candidates[1:]
	return c, nil
}

// DropList removes the candidate list for callID. Mirrors I_scscf_drop().
func (t *SCSCFTable) DropList(callID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.lists, callID)
}

// ListCount returns the number of currently-tracked call-ids.
func (t *SCSCFTable) ListCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.lists)
}

// ---------------------------------------------------------------------------
// Expiry sweep (mirrors ims_icscf_timer_routine)
// ---------------------------------------------------------------------------

// SweepExpired removes every candidate list whose CreatedAt is older
// than the configured entryExpiry. Returns the number of lists removed.
//
//	C: ims_icscf_timer_routine() — called every 60 s by the timer thread.
func (t *SCSCFTable) SweepExpired(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.Add(-t.entryExpiry)
	removed := 0
	for id, l := range t.lists {
		if l.CreatedAt.Before(cutoff) {
			delete(t.lists, id)
			removed++
		}
	}
	return removed
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toSet converts a capability slice to a set for O(1) lookup.
func toSet(caps []int) map[int]bool {
	m := make(map[int]bool, len(caps))
	for _, c := range caps {
		m[c] = true
	}
	return m
}

// clone returns a deep copy of c (so callers can mutate the returned
// slice without affecting the catalogue).
func (c SCSCFCapability) clone() SCSCFCapability {
	out := c
	out.MandatoryCaps = append([]int(nil), c.MandatoryCaps...)
	out.OptionalCaps = append([]int(nil), c.OptionalCaps...)
	return out
}

// String returns a one-line summary for logging.
func (c SCSCFCapability) String() string {
	return fmt.Sprintf("SCSCF{id=%d name=%q mandatory=%v optional=%v}",
		c.ID, c.Name, c.MandatoryCaps, c.OptionalCaps)
}
