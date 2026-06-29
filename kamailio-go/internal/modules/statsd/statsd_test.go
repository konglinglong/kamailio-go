// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * StatsD module tests - metric formatting and sending via a mock conn.
 */
package statsd

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// mockConn records every Write in a buffer guarded by a mutex.
type mockConn struct {
	mu     sync.Mutex
	buf    strings.Builder
	closed bool
}

func (c *mockConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errClosed
	}
	return c.buf.Write(b)
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *mockConn) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// failingConn always returns an error on Write.
type failingConn struct{ writes int }

func (c *failingConn) Write(b []byte) (int, error) {
	c.writes++
	return 0, errWrite
}

func (c *failingConn) Close() error { return nil }

type errString string

func (e errString) Error() string { return string(e) }

const (
	errClosed errString = "connection closed"
	errWrite  errString = "write failed"
)

func newMockModule(t *testing.T, prefix string) (*StatsDModule, *mockConn) {
	t.Helper()
	m := NewStatsDModule()
	conn := &mockConn{}
	// Set the config directly so the prefix is applied, then install the
	// mock conn without going through Init (which would dial).
	m.cfg = StatsDConfig{Prefix: prefix}
	m.SetConn(conn)
	return m, conn
}

func TestIncrementFormat(t *testing.T) {
	m, conn := newMockModule(t, "app")
	m.Increment("requests", 1, 0)
	m.Increment("requests", 5, 0)

	got := conn.String()
	want := "app.requests:1|capp.requests:5|c"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if m.SentCount() != 2 {
		t.Errorf("SentCount = %d, want 2", m.SentCount())
	}
}

func TestIncrementWithSampleRate(t *testing.T) {
	m, conn := newMockModule(t, "")
	m.Increment("hits", 1, 0.5)

	got := conn.String()
	if !strings.HasSuffix(got, "|@0.5") {
		t.Errorf("expected sample-rate suffix, got %q", got)
	}
	// A sample rate of 1.0 (or 0) must not add the suffix.
	conn.buf.Reset()
	m.Increment("hits", 1, 1.0)
	if strings.Contains(conn.String(), "|@") {
		t.Errorf("expected no sample-rate suffix for rate 1.0, got %q", conn.String())
	}
}

func TestDecrement(t *testing.T) {
	m, conn := newMockModule(t, "")
	m.Decrement("queue", 3, 0)
	got := conn.String()
	if got != "queue:-3|c" {
		t.Errorf("got %q, want queue:-3|c", got)
	}
}

func TestGauge(t *testing.T) {
	m, conn := newMockModule(t, "")
	m.Gauge("temp", 23.5)
	got := conn.String()
	if got != "temp:23.5|g" {
		t.Errorf("got %q, want temp:23.5|g", got)
	}
}

func TestTimer(t *testing.T) {
	m, conn := newMockModule(t, "")
	m.Timer("latency", 5*time.Millisecond)
	got := conn.String()
	if !strings.HasPrefix(got, "latency:") || !strings.HasSuffix(got, "|ms") {
		t.Errorf("got %q, want latency:<ms>|ms", got)
	}
	metric, err := ParseMetric(got)
	if err != nil {
		t.Fatalf("ParseMetric: %v", err)
	}
	if metric.Type != MetricTimer {
		t.Errorf("Type = %q, want timer", metric.Type)
	}
	if metric.Value < 4.9 || metric.Value > 5.1 {
		t.Errorf("Value = %v, want ~5", metric.Value)
	}
}

func TestSet(t *testing.T) {
	m, conn := newMockModule(t, "")
	m.Set("uniques", "user-1")
	m.Set("uniques", "user-2")
	got := conn.String()
	want := "uniques:user-1|suniques:user-2|s"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseMetric(t *testing.T) {
	cases := []struct {
		line string
		want MetricType
	}{
		{"app.req:5|c", MetricCounter},
		{"app.req:5|g", MetricGauge},
		{"app.req:5|ms", MetricTimer},
		{"app.req:user|s", MetricSet},
	}
	for _, c := range cases {
		m, err := ParseMetric(c.line)
		if err != nil {
			t.Fatalf("ParseMetric(%q): %v", c.line, err)
		}
		if m.Type != c.want {
			t.Errorf("ParseMetric(%q).Type = %q, want %q", c.line, m.Type, c.want)
		}
	}
	if _, err := ParseMetric(""); err == nil {
		t.Error("expected error for empty line")
	}
	if _, err := ParseMetric("no-colon"); err == nil {
		t.Error("expected error for missing colon")
	}
	if _, err := ParseMetric("a:b|x"); err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestIsConnectedAndClose(t *testing.T) {
	m := NewStatsDModule()
	if m.IsConnected() {
		t.Error("expected not connected before SetConn")
	}
	conn := &mockConn{}
	m.SetConn(conn)
	if !m.IsConnected() {
		t.Error("expected connected after SetConn")
	}
	if err := m.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if m.IsConnected() {
		t.Error("expected not connected after Close")
	}
	if !conn.closed {
		t.Error("expected underlying conn to be closed")
	}
}

func TestInitNoConfig(t *testing.T) {
	m := NewStatsDModule()
	if err := m.Init(nil); err != nil {
		t.Errorf("Init(nil): %v", err)
	}
	if m.IsConnected() {
		t.Error("expected not connected for nil config")
	}
	// Empty host/port also leaves the module disconnected.
	if err := m.Init(&StatsDConfig{}); err != nil {
		t.Errorf("Init(empty): %v", err)
	}
	if m.IsConnected() {
		t.Error("expected not connected for empty config")
	}
}

func TestWriteFailure(t *testing.T) {
	m := NewStatsDModule()
	m.SetConn(&failingConn{})
	m.Increment("x", 1, 0)
	if m.ErrorCount() != 1 {
		t.Errorf("ErrorCount = %d, want 1", m.ErrorCount())
	}
	if m.SentCount() != 0 {
		t.Errorf("SentCount = %d, want 0", m.SentCount())
	}
}

func TestConcurrentSend(t *testing.T) {
	m, conn := newMockModule(t, "")
	const goroutines = 50
	const perG = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Increment("c", 1, 0)
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * perG)
	if m.SentCount() != want {
		t.Errorf("SentCount = %d, want %d", m.SentCount(), want)
	}
	// The buffer should contain exactly want lines concatenated.
	if got := strings.Count(conn.String(), "|c"); got != int(want) {
		t.Errorf("buffer contains %d counters, want %d", got, want)
	}
}

func TestDefaultStatsDAndInit(t *testing.T) {
	Init()
	d1 := DefaultStatsD()
	d2 := DefaultStatsD()
	if d1 != d2 {
		t.Error("DefaultStatsD returned different instances")
	}
	d1.SetConn(&mockConn{})
	if !d2.IsConnected() {
		t.Error("expected default to share state")
	}
	Init()
	if DefaultStatsD().IsConnected() {
		t.Error("expected reset after Init()")
	}
}
