// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * memcached module - Memcached cache client.
 *
 * Port of the kamailio memcached module (src/modules/memcached). Provides a
 * key/value cache backed by a Memcached server. The actual Memcached
 * operations are performed through the MemcacheClient interface so tests
 * can substitute an in-memory mock and production code can plug in
 * github.com/bradfitz/gomemcache/memcache or similar.
 *
 * C equivalent: memcached.so - mcd_var.c / memcached.c.
 */

package memcached

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds the Memcached client configuration.
//
// C equivalent: the memcached_servers / memcached_timeout modparams.
type Config struct {
	Servers  []string      // memcached server addresses (host:port)
	Timeout  time.Duration // dial / command timeout
	MaxConns int           // connection pool size per server
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Servers:  []string{"127.0.0.1:11211"},
		Timeout:  5 * time.Second,
		MaxConns: 10,
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("memcached: nil config")
	}
	if len(c.Servers) == 0 {
		return errors.New("memcached: at least one server is required")
	}
	for _, s := range c.Servers {
		if strings.TrimSpace(s) == "" {
			return errors.New("memcached: empty server address")
		}
	}
	if c.Timeout < 0 {
		return fmt.Errorf("memcached: invalid timeout %v", c.Timeout)
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("memcached: invalid max conns %d", c.MaxConns)
	}
	return nil
}

// ---------------------------------------------------------------------------
// MemcacheClient - abstracted Memcached client (no hard dependency on a lib)
// ---------------------------------------------------------------------------

// MemcacheClient is the minimal subset of Memcached operations required by
// the module. It is an interface so tests can substitute an in-memory mock
// and production code can plug in a real client.
type MemcacheClient interface {
	Get(key string) (string, error)
	Set(key, value string, ttl int) error
	Delete(key string) error
	Stats() (map[string]string, error)
	Close() error
}

// ClientFactory builds a MemcacheClient for the given config. Production
// builds inject a real factory; the default returns a mock.
type ClientFactory func(cfg Config) (MemcacheClient, error)

// ErrNotFound is returned by Get when a key is absent.
var ErrNotFound = errors.New("memcached: key not found")

// cacheEntry holds a value and its expiry time.
type cacheEntry struct {
	value  string
	expiry time.Time
}

// isExpired reports whether the entry has expired. A zero expiry means no
// expiration.
func (e cacheEntry) isExpired(now time.Time) bool {
	return !e.expiry.IsZero() && now.After(e.expiry)
}

// mockMemcacheClient is an in-memory, concurrency-safe MemcacheClient.
type mockMemcacheClient struct {
	mu     sync.Mutex
	data   map[string]cacheEntry
	stats  map[string]string
	closed bool
	ops    atomic.Int64
}

// newMockClient creates an empty in-memory MemcacheClient.
func newMockClient() *mockMemcacheClient {
	return &mockMemcacheClient{
		data:  make(map[string]cacheEntry),
		stats: make(map[string]string),
	}
}

func (c *mockMemcacheClient) Get(key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "", errors.New("memcached: client closed")
	}
	c.ops.Add(1)
	e, ok := c.data[key]
	if !ok || e.isExpired(time.Now()) {
		if ok {
			delete(c.data, key)
		}
		return "", ErrNotFound
	}
	return e.value, nil
}

func (c *mockMemcacheClient) Set(key, value string, ttl int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("memcached: client closed")
	}
	c.ops.Add(1)
	var expiry time.Time
	if ttl > 0 {
		expiry = time.Now().Add(time.Duration(ttl) * time.Second)
	}
	c.data[key] = cacheEntry{value: value, expiry: expiry}
	return nil
}

func (c *mockMemcacheClient) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("memcached: client closed")
	}
	c.ops.Add(1)
	delete(c.data, key)
	return nil
}

func (c *mockMemcacheClient) Stats() (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("memcached: client closed")
	}
	out := make(map[string]string, len(c.stats)+2)
	for k, v := range c.stats {
		out[k] = v
	}
	out["curr_items"] = fmt.Sprintf("%d", len(c.data))
	out["total_ops"] = fmt.Sprintf("%d", c.ops.Load())
	return out, nil
}

func (c *mockMemcacheClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// MemcachedModule
// ---------------------------------------------------------------------------

// MemcachedModule is the Memcached cache client. It maintains a connection
// pool keyed by server address and dispatches cache operations.
//
// C equivalent: the module global state plus the memcached client pool.
type MemcachedModule struct {
	mu      sync.RWMutex
	cfg     Config
	factory ClientFactory
	clients map[string]MemcacheClient
	ops     atomic.Int64
}

// New creates a MemcachedModule with default configuration and a mock factory.
func New() *MemcachedModule {
	cfg := *DefaultConfig()
	m := &MemcachedModule{
		cfg:     cfg,
		clients: make(map[string]MemcacheClient),
	}
	m.factory = func(c Config) (MemcacheClient, error) {
		return newMockClient(), nil
	}
	return m
}

// NewWithConfig creates a MemcachedModule using the supplied configuration.
func NewWithConfig(cfg Config) *MemcachedModule {
	m := &MemcachedModule{cfg: cfg, clients: make(map[string]MemcacheClient)}
	m.factory = func(c Config) (MemcacheClient, error) {
		return newMockClient(), nil
	}
	return m
}

// Init (re)configures the module with the supplied config and resets the
// client pool.
//
// C equivalent: memcached_init() / mod_init().
func (m *MemcachedModule) Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	for _, c := range m.clients {
		_ = c.Close()
	}
	m.clients = make(map[string]MemcacheClient)
	m.ops.Store(0)
	return nil
}

// Config returns a copy of the current configuration.
func (m *MemcachedModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// SetServers configures the memcached server list.
func (m *MemcachedModule) SetServers(servers []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Servers = append([]string(nil), servers...)
}

// SetTimeout configures the command timeout.
func (m *MemcachedModule) SetTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Timeout = timeout
}

// SetClientFactory injects a real client factory (production wiring).
func (m *MemcachedModule) SetClientFactory(f ClientFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factory = f
}

// client returns the client for the first configured server, creating it on
// first use. Connections are pooled per server address.
func (m *MemcachedModule) client() (MemcacheClient, error) {
	m.mu.RLock()
	cfg := m.cfg
	factory := m.factory
	m.mu.RUnlock()

	if len(cfg.Servers) == 0 {
		return nil, errors.New("memcached: no servers configured")
	}
	addr := cfg.Servers[0]

	m.mu.Lock()
	if c, ok := m.clients[addr]; ok {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()

	c, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("memcached connect: %w", err)
	}
	m.mu.Lock()
	if existing, ok := m.clients[addr]; ok {
		_ = c.Close()
		m.mu.Unlock()
		return existing, nil
	}
	m.clients[addr] = c
	m.mu.Unlock()
	return c, nil
}

// Get fetches a value by key. Returns ErrNotFound when the key is absent.
//
// C equivalent: memcached_get().
func (m *MemcachedModule) Get(key string) (string, error) {
	if key == "" {
		return "", errors.New("memcached: empty key")
	}
	c, err := m.client()
	if err != nil {
		return "", err
	}
	m.ops.Add(1)
	return c.Get(key)
}

// Set stores a value with the given TTL (seconds). A TTL <= 0 means no
// expiration.
//
// C equivalent: memcached_set().
func (m *MemcachedModule) Set(key, value string, ttl int) error {
	if key == "" {
		return errors.New("memcached: empty key")
	}
	c, err := m.client()
	if err != nil {
		return err
	}
	m.ops.Add(1)
	return c.Set(key, value, ttl)
}

// Delete removes a key. Missing keys are not an error.
//
// C equivalent: memcached_delete().
func (m *MemcachedModule) Delete(key string) error {
	if key == "" {
		return errors.New("memcached: empty key")
	}
	c, err := m.client()
	if err != nil {
		return err
	}
	m.ops.Add(1)
	return c.Delete(key)
}

// GetMulti fetches multiple keys in one logical round-trip. Missing keys
// are omitted from the result map.
//
// C equivalent: memcached_get_multi().
func (m *MemcachedModule) GetMulti(keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		v, err := m.Get(k)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// Stats returns server statistics as a string map.
//
// C equivalent: memcached_stats().
func (m *MemcachedModule) Stats() (map[string]string, error) {
	c, err := m.client()
	if err != nil {
		return nil, err
	}
	return c.Stats()
}

// FlushAll clears all keys on the server.
//
// C equivalent: memcached_flush().
func (m *MemcachedModule) FlushAll() error {
	c, err := m.client()
	if err != nil {
		return err
	}
	m.ops.Add(1)
	// The mock client exposes no flush method; emulate by re-creating the
	// data store. Real clients implement FLUSH_ALL natively.
	if mc, ok := c.(*mockMemcacheClient); ok {
		mc.mu.Lock()
		mc.data = make(map[string]cacheEntry)
		mc.mu.Unlock()
		return nil
	}
	return errors.New("memcached: flush not supported by client")
}

// OpsCount returns the number of dispatched operations.
func (m *MemcachedModule) OpsCount() int64 {
	return m.ops.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *MemcachedModule
)

// DefaultMemcached returns the process-wide module, creating it on first use.
func DefaultMemcached() *MemcachedModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)configures the process-wide module with the supplied config and
// resets the client pool.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = &MemcachedModule{cfg: cfg, clients: make(map[string]MemcacheClient)}
	defaultM.factory = func(c Config) (MemcacheClient, error) {
		return newMockClient(), nil
	}
	return nil
}

// Get is the package-level wrapper around DefaultMemcached().Get.
func Get(key string) (string, error) { return DefaultMemcached().Get(key) }

// Set is the package-level wrapper around DefaultMemcached().Set.
func Set(key, value string, ttl int) error { return DefaultMemcached().Set(key, value, ttl) }

// Delete is the package-level wrapper around DefaultMemcached().Delete.
func Delete(key string) error { return DefaultMemcached().Delete(key) }

// GetMulti is the package-level wrapper around DefaultMemcached().GetMulti.
func GetMulti(keys []string) (map[string]string, error) {
	return DefaultMemcached().GetMulti(keys)
}

// Stats is the package-level wrapper around DefaultMemcached().Stats.
func Stats() (map[string]string, error) { return DefaultMemcached().Stats() }

// FlushAll is the package-level wrapper around DefaultMemcached().FlushAll.
func FlushAll() error { return DefaultMemcached().FlushAll() }
