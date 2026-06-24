// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * StatsD module - send metrics to a statsd server.
 *
 * Port of the kamailio statsd module (src/modules/statsd). A
 * StatsDModule formats metrics in the statsd line protocol and writes
 * them to a UDP-like connection. The connection is abstracted behind
 * the Conn interface so tests can substitute a mock without touching
 * the network.
 *
 * Supported metric types match the C module's exported functions:
 * counters (incr/decr), gauges, timers (histograms) and sets.
 */
package statsd

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MetricType identifies a statsd metric kind.
type MetricType string

const (
	// MetricCounter is a statsd counter (suffix "|c").
	MetricCounter MetricType = "counter"
	// MetricTimer is a statsd timer (suffix "|ms").
	MetricTimer MetricType = "timer"
	// MetricGauge is a statsd gauge (suffix "|g").
	MetricGauge MetricType = "gauge"
	// MetricSet is a statsd set (suffix "|s").
	MetricSet MetricType = "set"
)

// StatsDConfig holds the connection parameters. Host and Port select
// the statsd server; Prefix is prepended to every metric name.
type StatsDConfig struct {
	Host   string
	Port   string
	Prefix string
}

// StatsDMetric is a single recorded metric, kept for inspection in
// tests and for the in-memory buffer when the connection is offline.
type StatsDMetric struct {
	Type  MetricType
	Name  string
	Value float64
}

// Conn is the transport abstraction used by StatsDModule. The net.UDPConn
// returned by net.Dial("udp", ...) satisfies this interface, as does
// the mockConn used in tests.
type Conn interface {
	Write(b []byte) (int, error)
	Close() error
}

// StatsDModule implements the statsd module. It is safe for concurrent
// use: writes to the connection are serialised by mu, and the connected
// flag plus counters are atomic.
type StatsDModule struct {
	mu        sync.Mutex
	conn      Conn
	cfg       StatsDConfig
	connected atomic.Bool
	sent      atomic.Int64
	errors    atomic.Int64
}

// NewStatsDModule creates a new StatsDModule that is not yet connected.
func NewStatsDModule() *StatsDModule {
	return &StatsDModule{}
}

// Init connects to the statsd server described by cfg. A nil or empty
// Host/Port leaves the module disconnected (calls become no-ops), which
// mirrors the C module's tolerance of a missing server.
func (m *StatsDModule) Init(cfg *StatsDConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_ = m.conn.Close()
		m.conn = nil
	}
	m.connected.Store(false)
	if cfg == nil {
		m.cfg = StatsDConfig{}
		return nil
	}
	m.cfg = *cfg
	if cfg.Host == "" || cfg.Port == "" {
		return nil
	}
	conn, err := dial(cfg.Host, cfg.Port)
	if err != nil {
		return err
	}
	m.conn = conn
	m.connected.Store(true)
	return nil
}

// SetConn installs an explicit connection, bypassing Init's dial step.
// This is primarily useful for tests that inject a mock connection.
// Passing nil disconnects the module.
func (m *StatsDModule) SetConn(conn Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.conn = conn
	m.connected.Store(conn != nil)
}

// Increment sends a counter increment. A sampleRate of 1.0 (or less than
// or equal to 0) always sends; otherwise the metric is sent with the
// "@<rate>" suffix expected by statsd.
func (m *StatsDModule) Increment(name string, value int64, sampleRate float64) {
	m.send(formatMetric(m.cfg.Prefix, name, strconv.FormatInt(value, 10), "c", sampleRate))
}

// Decrement sends a counter decrement (a negative increment).
func (m *StatsDModule) Decrement(name string, value int64, sampleRate float64) {
	m.Increment(name, -value, sampleRate)
}

// Gauge sends a gauge metric.
func (m *StatsDModule) Gauge(name string, value float64) {
	m.send(formatMetric(m.cfg.Prefix, name, strconv.FormatFloat(value, 'f', -1, 64), "g", 0))
}

// Timer sends a timer metric in milliseconds.
func (m *StatsDModule) Timer(name string, duration time.Duration) {
	ms := float64(duration.Microseconds()) / 1000.0
	m.send(formatMetric(m.cfg.Prefix, name, strconv.FormatFloat(ms, 'f', -1, 64), "ms", 0))
}

// Set sends a set metric (unique value counter).
func (m *StatsDModule) Set(name string, value string) {
	m.send(formatMetric(m.cfg.Prefix, name, value, "s", 0))
}

// Close closes the connection to the statsd server.
func (m *StatsDModule) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connected.Store(false)
	if m.conn != nil {
		err := m.conn.Close()
		m.conn = nil
		return err
	}
	return nil
}

// IsConnected reports whether the module currently has an open
// connection to a statsd server.
func (m *StatsDModule) IsConnected() bool {
	return m.connected.Load()
}

// SentCount returns the number of metrics successfully written.
func (m *StatsDModule) SentCount() int64 {
	return m.sent.Load()
}

// ErrorCount returns the number of metrics that failed to write.
func (m *StatsDModule) ErrorCount() int64 {
	return m.errors.Load()
}

// send writes a single formatted statsd line. Failures are counted but
// not propagated: the C module treats statsd as fire-and-forget.
func (m *StatsDModule) send(line string) {
	if line == "" {
		return
	}
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return
	}
	if _, err := conn.Write([]byte(line)); err != nil {
		m.errors.Add(1)
		return
	}
	m.sent.Add(1)
}

// formatMetric builds a statsd line "<prefix>.<name>:<value>|<type>[@<rate>]".
// A non-positive sample rate omits the @-suffix.
func formatMetric(prefix, name, value, mtype string, sampleRate float64) string {
	full := name
	if prefix != "" {
		full = prefix + "." + name
	}
	line := full + ":" + value + "|" + mtype
	if sampleRate > 0 && sampleRate < 1.0 {
		line += "|@" + strconv.FormatFloat(sampleRate, 'f', -1, 64)
	}
	return line
}

// ParseMetric parses a statsd line back into a StatsDMetric. It is
// provided for tests and tooling that need to inspect what was sent.
// For set metrics the Value field is not meaningful (the raw string
// value is carried in the line itself).
func ParseMetric(line string) (*StatsDMetric, error) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return nil, fmt.Errorf("empty metric line")
	}
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return nil, fmt.Errorf("missing ':' in %q", line)
	}
	name := line[:colon]
	rest := line[colon+1:]
	parts := strings.SplitN(rest, "|", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("missing type in %q", line)
	}
	var mt MetricType
	switch parts[1] {
	case "c":
		mt = MetricCounter
	case "ms":
		mt = MetricTimer
	case "g":
		mt = MetricGauge
	case "s":
		mt = MetricSet
	default:
		return nil, fmt.Errorf("unknown metric type %q", parts[1])
	}
	// Set values are arbitrary strings; only attempt numeric parsing for
	// the other metric types.
	var val float64
	if mt != MetricSet {
		v, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return nil, fmt.Errorf("bad value %q: %w", parts[0], err)
		}
		val = v
	}
	return &StatsDMetric{Type: mt, Name: name, Value: val}, nil
}

// dial is the default connection factory. It is a package-level
// variable so tests can replace it with a fake that does not touch the
// network.
var dial = func(host, port string) (Conn, error) {
	return nil, fmt.Errorf("statsd: dial not configured")
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultStatsD *StatsDModule
)

// DefaultStatsD returns the process-wide StatsDModule, creating one on
// first use.
func DefaultStatsD() *StatsDModule {
	defaultMu.RLock()
	m := defaultStatsD
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStatsD == nil {
		defaultStatsD = NewStatsDModule()
	}
	return defaultStatsD
}

// Init (re)initialises the process-wide StatsDModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStatsD = NewStatsDModule()
}
