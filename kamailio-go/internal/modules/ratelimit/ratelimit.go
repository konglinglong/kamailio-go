// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * RateLimit module - per-key rate limiting.
 *
 * Port of the kamailio ratelimit module (src/modules/ratelimit). A
 * RateLimitModule tracks per-key counters and decides whether a new
 * request is allowed under the configured limit and interval.
 *
 * Two algorithms are provided, matching the C module's PIPE_ALGO_* enum:
 *   - token: a token bucket that refills at limit/interval, allowing
 *     bursts up to the bucket capacity;
 *   - taildrop: a fixed-window counter that rejects once limit requests
 *     have been seen in the current interval.
 *
 * The Check method dispatches on the configured algorithm; the
 * CheckTokenBucket and CheckTaildrop methods apply a specific algorithm
 * regardless of configuration.
 */
package ratelimit

import (
	"sync"
	"time"
)

// RateLimitConfig holds the default algorithm and parameters. The
// Algorithm field selects which algorithm Check uses; "token" selects
// the token bucket, "taildrop" selects the fixed-window counter.
type RateLimitConfig struct {
	Algorithm string
	Limit     int
	Interval  time.Duration
}

// bucketState is the per-key state for the token bucket algorithm.
type bucketState struct {
	tokens     float64
	lastRefill time.Time
}

// windowState is the per-key state for the taildrop algorithm.
type windowState struct {
	count       int
	windowStart time.Time
}

// RateLimitModule implements the ratelimit module. It is safe for
// concurrent use: the per-key maps are guarded by mu.
type RateLimitModule struct {
	mu       sync.Mutex
	cfg      RateLimitConfig
	buckets  map[string]*bucketState
	windows  map[string]*windowState
}

// NewRateLimitModule creates a new RateLimitModule with a default
// token-bucket configuration.
func NewRateLimitModule() *RateLimitModule {
	return &RateLimitModule{
		cfg: RateLimitConfig{
			Algorithm: "token",
			Limit:     100,
			Interval:  time.Second,
		},
		buckets: make(map[string]*bucketState),
		windows: make(map[string]*windowState),
	}
}

// SetConfig updates the default configuration. The Algorithm field
// controls which algorithm Check dispatches to.
func (m *RateLimitModule) SetConfig(cfg RateLimitConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// GetConfig returns a copy of the current configuration.
func (m *RateLimitModule) GetConfig() RateLimitConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Check returns true if a request for the given key is allowed under the
// specified limit and interval, using the algorithm from the module
// configuration. A non-positive limit always rejects.
func (m *RateLimitModule) Check(key string, limit int, interval time.Duration) bool {
	m.mu.Lock()
	alg := m.cfg.Algorithm
	m.mu.Unlock()
	switch alg {
	case "taildrop":
		return m.CheckTaildrop(key, limit, interval)
	default:
		return m.CheckTokenBucket(key, limit, interval)
	}
}

// CheckTokenBucket implements a token bucket. The bucket refills at a
// rate of limit tokens per interval; each allowed request consumes one
// token. The bucket capacity is limit, so bursts of up to limit
// requests are allowed immediately after a refill.
func (m *RateLimitModule) CheckTokenBucket(key string, limit int, interval time.Duration) bool {
	if limit <= 0 || interval <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	b, ok := m.buckets[key]
	if !ok {
		// A new bucket starts full so the first burst is allowed.
		b = &bucketState{tokens: float64(limit), lastRefill: now}
		m.buckets[key] = b
	}
	// Refill based on elapsed time.
	elapsed := now.Sub(b.lastRefill)
	if elapsed > 0 {
		refill := float64(elapsed) / float64(interval) * float64(limit)
		b.tokens += refill
		if b.tokens > float64(limit) {
			b.tokens = float64(limit)
		}
		b.lastRefill = now
	}
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// CheckTaildrop implements a fixed-window counter. Within each interval
// up to limit requests are allowed; subsequent requests are rejected
// until the window rolls over.
func (m *RateLimitModule) CheckTaildrop(key string, limit int, interval time.Duration) bool {
	if limit <= 0 || interval <= 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	w, ok := m.windows[key]
	if !ok || now.Sub(w.windowStart) >= interval {
		w = &windowState{count: 0, windowStart: now}
		m.windows[key] = w
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

// Reset clears the state for the given key, so the next request starts
// a fresh bucket/window.
func (m *RateLimitModule) Reset(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.buckets, key)
	delete(m.windows, key)
}

// Count returns the number of requests recorded for the key in the
// current window. For the token bucket this is approximated by the
// consumed tokens (limit - remaining); for taildrop it is the window
// counter.
func (m *RateLimitModule) Count(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.windows[key]; ok {
		return w.count
	}
	if b, ok := m.buckets[key]; ok {
		// Approximate consumed count from remaining tokens.
		return int(float64(m.cfg.Limit) - b.tokens)
	}
	return 0
}

// List returns a snapshot of the per-key counts. For taildrop keys this
// is the window counter; for token-bucket keys it is the consumed count
// approximation.
func (m *RateLimitModule) List() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int, len(m.windows)+len(m.buckets))
	for k, w := range m.windows {
		out[k] = w.count
	}
	limit := float64(m.cfg.Limit)
	for k, b := range m.buckets {
		if _, ok := out[k]; ok {
			continue
		}
		out[k] = int(limit - b.tokens)
		if out[k] < 0 {
			out[k] = 0
		}
	}
	return out
}

// Clear removes all per-key state.
func (m *RateLimitModule) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buckets = make(map[string]*bucketState)
	m.windows = make(map[string]*windowState)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu        sync.RWMutex
	defaultRateLimit *RateLimitModule
)

// DefaultRateLimit returns the process-wide RateLimitModule, creating
// one on first use.
func DefaultRateLimit() *RateLimitModule {
	defaultMu.RLock()
	m := defaultRateLimit
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultRateLimit == nil {
		defaultRateLimit = NewRateLimitModule()
	}
	return defaultRateLimit
}

// Init (re)initialises the process-wide RateLimitModule to a fresh
// state, mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultRateLimit = NewRateLimitModule()
}
