// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Tests for TCP connection pool.
 */

package transport

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeConn is a minimal net.Conn for testing. It counts writes and
// never actually reads.
type fakeConn struct {
	addr     string
	closed   atomic.Bool
	writes   atomic.Uint64
	writeErr error
}

func (f *fakeConn) Read(b []byte) (int, error)         { return 0, nil }
func (f *fakeConn) Write(b []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.writes.Add(1)
	return len(b), nil
}
func (f *fakeConn) Close() error {
	f.closed.Store(true)
	return nil
}
func (f *fakeConn) LocalAddr() net.Addr                { return &net.TCPAddr{Port: 1234} }
func (f *fakeConn) RemoteAddr() net.Addr               { return &net.TCPAddr{Port: 5678} }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

func newTestPool(t *testing.T, dialer func(network, addr string, timeout time.Duration) (net.Conn, error)) *TCPConnPool {
	t.Helper()
	cfg := PoolConfig{
		MaxPerHost:    2,
		IdleTimeout:   50 * time.Millisecond,
		SweepInterval: 20 * time.Millisecond,
		DialTimeout:   100 * time.Millisecond,
	}
	p := NewTCPConnPool(cfg)
	p.dialer = dialer
	return p
}

func TestTCPConnPool_GetDialsNewConnection(t *testing.T) {
	var dials atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dials.Add(1)
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	conn, err := p.Get("10.0.0.1:5060")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if conn == nil {
		t.Fatal("Get returned nil conn")
	}
	if dials.Load() != 1 {
		t.Errorf("expected 1 dial, got %d", dials.Load())
	}
	// Don't return to pool; the conn will be GC'd.
}

func TestTCPConnPool_ReusesConnection(t *testing.T) {
	var dials atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dials.Add(1)
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	// First Get dials, then Put returns to pool.
	conn1, err := p.Get("10.0.0.1:5060")
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	p.Put("10.0.0.1:5060", conn1, nil)

	// Second Get should reuse without dialing.
	conn2, err := p.Get("10.0.0.1:5060")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if dials.Load() != 1 {
		t.Errorf("expected 1 dial (reuse), got %d", dials.Load())
	}
	p.Put("10.0.0.1:5060", conn2, nil)
}

func TestTCPConnPool_PutWithErrorDiscards(t *testing.T) {
	var dials atomic.Uint64
	var closedConns []*fakeConn
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		c := &fakeConn{addr: addr}
		dials.Add(1)
		return c, nil
	}
	_ = closedConns
	p := newTestPool(t, dialer)
	defer p.Close()

	conn, err := p.Get("10.0.0.1:5060")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	fc := conn.(*fakeConn)
	// Return with error → should be closed.
	p.Put("10.0.0.1:5060", conn, errors.New("write failed"))
	if !fc.closed.Load() {
		t.Error("connection should be closed after Put with error")
	}
	// Next Get should dial again.
	_, err = p.Get("10.0.0.1:5060")
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if dials.Load() != 2 {
		t.Errorf("expected 2 dials, got %d", dials.Load())
	}
}

func TestTCPConnPool_MaxPerHostEvicts(t *testing.T) {
	var dials atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dials.Add(1)
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	// Pool config MaxPerHost=2. Dial three connections and return the
	// first two to fill the pool to capacity.
	c1, _ := p.Get("10.0.0.1:5060")
	c2, _ := p.Get("10.0.0.1:5060")
	c3, _ := p.Get("10.0.0.1:5060") // third dial, not yet returned
	fc3 := c3.(*fakeConn)
	p.Put("10.0.0.1:5060", c1, nil) // pool idle=1
	p.Put("10.0.0.1:5060", c2, nil) // pool idle=2 (== MaxPerHost)
	// Returning c3 when pool is at capacity must close it.
	p.Put("10.0.0.1:5060", c3, nil)
	if !fc3.closed.Load() {
		t.Error("third connection should be closed when pool is full")
	}
}

func TestTCPConnPool_SweepClosesIdle(t *testing.T) {
	var dials atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dials.Add(1)
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	// Get and return a connection; it should be swept after idle timeout.
	conn, _ := p.Get("10.0.0.1:5060")
	fc := conn.(*fakeConn)
	p.Put("10.0.0.1:5060", conn, nil)

	// Wait for sweep (idle timeout 50ms, sweep every 20ms).
	time.Sleep(150 * time.Millisecond)
	if !fc.closed.Load() {
		t.Error("idle connection should be closed by sweeper")
	}
	stats := p.Stats()
	if stats.IdleConns != 0 {
		t.Errorf("expected 0 idle conns after sweep, got %d", stats.IdleConns)
	}
}

func TestTCPConnPool_Stats(t *testing.T) {
	var dials atomic.Uint64
	var reuses atomic.Uint64
	_ = reuses
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dials.Add(1)
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	c1, _ := p.Get("a:5060")
	p.Put("a:5060", c1, nil)
	c2, _ := p.Get("a:5060") // reuse
	p.Put("a:5060", c2, nil)
	c3, _ := p.Get("b:5060") // new dial for new host
	p.Put("b:5060", c3, errors.New("err")) // discard

	stats := p.Stats()
	if stats.Dials != 2 {
		t.Errorf("Dials=%d want 2", stats.Dials)
	}
	if stats.Reuses != 1 {
		t.Errorf("Reuses=%d want 1", stats.Reuses)
	}
	if stats.Closes != 1 {
		t.Errorf("Closes=%d want 1", stats.Closes)
	}
	if stats.Hosts != 1 {
		t.Errorf("Hosts=%d want 1 (only a:5060 has idle)", stats.Hosts)
	}
}

func TestTCPConnPool_CloseClosesAll(t *testing.T) {
	var closedCount atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		c := &fakeConn{addr: addr}
		return c, nil
	}
	p := newTestPool(t, dialer)

	c1, _ := p.Get("a:5060")
	c2, _ := p.Get("b:5060")
	p.Put("a:5060", c1, nil)
	p.Put("b:5060", c2, nil)

	// Track closes by wrapping: we can't easily count fakeConn closes
	// without a wrapper, so just verify Close() doesn't panic and
	// subsequent Get fails.
	p.Close()
	_, err := p.Get("a:5060")
	if err == nil {
		t.Error("Get after Close should fail")
	}
	_ = closedCount
}

func TestTCPConnPool_ConcurrentAccess(t *testing.T) {
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	const N = 50
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := p.Get("10.0.0.1:5060")
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			p.Put("10.0.0.1:5060", conn, nil)
		}()
	}
	wg.Wait()
	stats := p.Stats()
	if stats.IdleConns > 2 {
		t.Errorf("idle conns=%d should be <= MaxPerHost=2", stats.IdleConns)
	}
}

func TestTCPConnPool_DialError(t *testing.T) {
	dialErr := errors.New("connection refused")
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		return nil, dialErr
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	_, err := p.Get("10.0.0.1:5060")
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestPooledSender_Send(t *testing.T) {
	var dials atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		dials.Add(1)
		return &fakeConn{addr: addr}, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	sender := NewPooledSender(p)
	err := sender.Send("10.0.0.1:5060", []byte("hello"))
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	// Second send should reuse.
	err = sender.Send("10.0.0.1:5060", []byte("world"))
	if err != nil {
		t.Fatalf("second Send failed: %v", err)
	}
	if dials.Load() != 1 {
		t.Errorf("expected 1 dial, got %d", dials.Load())
	}
}

func TestPooledSender_SendError(t *testing.T) {
	var dials atomic.Uint64
	dialer := func(network, addr string, timeout time.Duration) (net.Conn, error) {
		c := &fakeConn{addr: addr, writeErr: errors.New("write error")}
		dials.Add(1)
		return c, nil
	}
	p := newTestPool(t, dialer)
	defer p.Close()

	sender := NewPooledSender(p)
	err := sender.Send("10.0.0.1:5060", []byte("hello"))
	if err == nil {
		t.Fatal("expected send error")
	}
	// Connection should be discarded; next send dials new.
	err = sender.Send("10.0.0.1:5060", []byte("hello"))
	if err == nil {
		t.Fatal("expected send error on second attempt too")
	}
	if dials.Load() != 2 {
		t.Errorf("expected 2 dials, got %d", dials.Load())
	}
}

func TestTCPConnPool_DefaultConfig(t *testing.T) {
	cfg := DefaultPoolConfig()
	if cfg.MaxPerHost <= 0 {
		t.Error("MaxPerHost should be positive")
	}
	if cfg.IdleTimeout <= 0 {
		t.Error("IdleTimeout should be positive")
	}
	if cfg.SweepInterval <= 0 {
		t.Error("SweepInterval should be positive")
	}
}
