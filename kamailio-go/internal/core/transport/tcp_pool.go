// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TCP connection pool - mirrors the C tcp_main.c connection reuse
 * mechanism. Outbound TCP connections to the same destination are
 * pooled so that subsequent SIP messages reuse an existing connection
 * instead of opening a new one each time.
 *
 * The pool is keyed by "host:port". Each pooled connection carries a
 * last-used timestamp and is closed after exceeding the idle timeout.
 * A background goroutine sweeps idle connections periodically.
 */

package transport

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// PoolConfig tunes the TCP connection pool.
type PoolConfig struct {
	// MaxPerHost is the maximum number of idle connections kept per
	// destination address. Defaults to 4.
	MaxPerHost int
	// IdleTimeout is how long an idle connection is kept before it is
	// closed. Defaults to 5 minutes.
	IdleTimeout time.Duration
	// SweepInterval controls how often the background sweeper runs.
	// Defaults to 1 minute.
	SweepInterval time.Duration
	// DialTimeout is the timeout for opening new connections.
	// Defaults to TCPConnTimeout (10s).
	DialTimeout time.Duration
}

// DefaultPoolConfig returns the default pool configuration.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxPerHost:    4,
		IdleTimeout:   5 * time.Minute,
		SweepInterval: time.Minute,
		DialTimeout:   TCPConnTimeout,
	}
}

// pooledConn wraps a net.Conn with pool metadata.
type pooledConn struct {
	conn     net.Conn
	lastUsed time.Time
	// closed is set atomically when the connection is being torn down
	// so concurrent Get callers don't observe a half-closed conn.
	closed atomic.Bool
}

// TCPConnPool maintains a pool of reusable outbound TCP connections
// keyed by destination address.
type TCPConnPool struct {
	mu      sync.Mutex
	conns   map[string][]*pooledConn
	cfg     PoolConfig
	dialer  func(network, addr string, timeout time.Duration) (net.Conn, error)
	stopCh  chan struct{}
	stopped atomic.Bool
	wg      sync.WaitGroup
	// stats
	totalDials  atomic.Uint64
	totalReuses atomic.Uint64
	totalCloses atomic.Uint64
}

// NewTCPConnPool creates a pool with the given configuration. A nil
// config uses DefaultPoolConfig(). Starts a background sweeper goroutine
// that closes idle connections.
func NewTCPConnPool(cfg PoolConfig) *TCPConnPool {
	if cfg.MaxPerHost <= 0 {
		cfg.MaxPerHost = DefaultPoolConfig().MaxPerHost
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultPoolConfig().IdleTimeout
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = DefaultPoolConfig().SweepInterval
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = DefaultPoolConfig().DialTimeout
	}
	p := &TCPConnPool{
		conns:  make(map[string][]*pooledConn),
		cfg:    cfg,
		dialer: defaultDialer,
		stopCh: make(chan struct{}),
	}
	p.wg.Add(1)
	go p.sweepLoop()
	return p
}

// defaultDialer dials a TCP connection with a timeout.
func defaultDialer(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

// Get returns a pooled connection to addr, dialing a new one if none
// are available. The returned connection must be returned to the pool
// via Put when the caller is done with it; connections that encountered
// errors should be passed to Put with err != nil so they are discarded.
func (p *TCPConnPool) Get(addr string) (net.Conn, error) {
	if p.stopped.Load() {
		return nil, errors.New("tcp pool: closed")
	}
	// Try to reuse an idle connection.
	if pc := p.takeIdle(addr); pc != nil {
		// Verify the connection is still alive with a zero-length read
		// deadline check: SetDeadline doesn't read, so we rely on the
		// next Write/Read to detect a broken connection.
		p.totalReuses.Add(1)
		return pc.conn, nil
	}
	// Dial a new connection.
	p.totalDials.Add(1)
	conn, err := p.dialer("tcp", addr, p.cfg.DialTimeout)
	if err != nil {
		return nil, fmt.Errorf("tcp pool: dial %s: %w", addr, err)
	}
	return conn, nil
}

// takeIdle removes and returns the most recently used idle connection
// for addr, or nil if none is available.
func (p *TCPConnPool) takeIdle(addr string) *pooledConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.conns[addr]
	for len(list) > 0 {
		// Pop the last (most recently used) entry.
		pc := list[len(list)-1]
		list = list[:len(list)-1]
		if pc.closed.Load() {
			continue
		}
		p.conns[addr] = list
		return pc
	}
	delete(p.conns, addr)
	return nil
}

// Put returns a connection to the pool. If err is non-nil the
// connection is considered broken and is closed immediately instead of
// being returned to the pool.
func (p *TCPConnPool) Put(addr string, conn net.Conn, err error) {
	if conn == nil {
		return
	}
	if err != nil {
		p.totalCloses.Add(1)
		conn.Close()
		return
	}
	if p.stopped.Load() {
		p.totalCloses.Add(1)
		conn.Close()
		return
	}
	pc := &pooledConn{conn: conn, lastUsed: time.Now()}
	p.mu.Lock()
	list := p.conns[addr]
	if len(list) >= p.cfg.MaxPerHost {
		// Pool full for this host: close the connection.
		p.mu.Unlock()
		p.totalCloses.Add(1)
		conn.Close()
		return
	}
	p.conns[addr] = append(list, pc)
	p.mu.Unlock()
}

// Close closes all pooled connections and stops the background sweeper.
func (p *TCPConnPool) Close() {
	if !p.stopped.CompareAndSwap(false, true) {
		return
	}
	close(p.stopCh)
	p.wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, list := range p.conns {
		for _, pc := range list {
			pc.closed.Store(true)
			pc.conn.Close()
			p.totalCloses.Add(1)
		}
		delete(p.conns, addr)
	}
}

// sweepLoop periodically closes connections that have been idle for
// longer than IdleTimeout.
func (p *TCPConnPool) sweepLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.sweep()
		}
	}
}

// sweep closes idle connections. Called by the sweep loop.
func (p *TCPConnPool) sweep() {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for addr, list := range p.conns {
		kept := list[:0]
		for _, pc := range list {
			if now.Sub(pc.lastUsed) > p.cfg.IdleTimeout {
				pc.closed.Store(true)
				pc.conn.Close()
				p.totalCloses.Add(1)
				continue
			}
			kept = append(kept, pc)
		}
		if len(kept) == 0 {
			delete(p.conns, addr)
		} else {
			p.conns[addr] = kept
		}
	}
}

// Stats returns current pool statistics.
type PoolStats struct {
	Dials   uint64
	Reuses  uint64
	Closes  uint64
	Hosts   int
	IdleConns int
}

// Stats returns a snapshot of pool statistics.
func (p *TCPConnPool) Stats() PoolStats {
	p.mu.Lock()
	hosts := len(p.conns)
	idle := 0
	for _, list := range p.conns {
		idle += len(list)
	}
	p.mu.Unlock()
	return PoolStats{
		Dials:     p.totalDials.Load(),
		Reuses:    p.totalReuses.Load(),
		Closes:    p.totalCloses.Load(),
		Hosts:     hosts,
		IdleConns: idle,
	}
}

// PooledSender wraps a TCPConnPool to provide a simple Send API. Each
// Send borrows a connection, writes, and returns it. If the write fails
// the connection is discarded.
type PooledSender struct {
	pool *TCPConnPool
}

// NewPooledSender creates a sender backed by the given pool.
func NewPooledSender(pool *TCPConnPool) *PooledSender {
	return &PooledSender{pool: pool}
}

// Send writes data to addr, reusing a pooled connection if available.
// Returns an error if the write fails; in that case the underlying
// connection is discarded.
func (s *PooledSender) Send(addr string, data []byte) error {
	conn, err := s.pool.Get(addr)
	if err != nil {
		return err
	}
	conn.SetWriteDeadline(time.Now().Add(TCPWriteTimeout))
	_, werr := conn.Write(data)
	if werr != nil {
		s.pool.Put(addr, conn, werr)
		return fmt.Errorf("tcp pool: write to %s: %w", addr, werr)
	}
	s.pool.Put(addr, conn, nil)
	return nil
}
