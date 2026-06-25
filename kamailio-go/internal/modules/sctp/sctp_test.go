// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - SCTP module tests.
 */

package sctp

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Backward-compatible API tests (simulation mode, since kernel SCTP is not
// available in CI containers).
// ---------------------------------------------------------------------------

func TestInitSendReceive(t *testing.T) {
	m := New()
	if m.IsConnected() {
		t.Fatal("expected not connected before Init")
	}
	if err := m.Init("127.0.0.1:5060"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !m.IsConnected() {
		t.Fatal("expected connected after Init")
	}
	if m.Addr() != "127.0.0.1:5060" {
		t.Fatalf("Addr = %q", m.Addr())
	}
	if err := m.Send([]byte("hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	rx := m.Receive()
	if rx == nil {
		t.Fatal("expected non-nil receive channel")
	}
	select {
	case got := <-rx:
		if string(got) != "hello" {
			t.Fatalf("received %q, want hello", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for received data")
	}
}

func TestInitErrors(t *testing.T) {
	m := New()
	if err := m.Init(""); err == nil {
		t.Fatal("expected error for empty address")
	}
	if err := m.Send([]byte("x")); err == nil {
		t.Fatal("expected error when not connected")
	}
	// An unresolvable host returns an error (no simulation fallback
	// because the address parsing fails before the listener is created).
	if err := m.Init("nonexistent.invalid:5060"); err == nil {
		t.Fatal("expected error for unresolvable host")
	}
	// Send before connected still returns ErrEmptyData for empty input
	// (the empty-data check happens first).
	if err := m.Send(nil); err != ErrEmptyData {
		t.Fatalf("Send(nil) = %v, want ErrEmptyData", err)
	}
	if err := m.Send([]byte{}); err != ErrEmptyData {
		t.Fatalf("Send([]byte{}) = %v, want ErrEmptyData", err)
	}
}

func TestClose(t *testing.T) {
	m := New()
	if err := m.Init("127.0.0.1:5060"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Send([]byte("x")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	m.Close()
	if m.IsConnected() {
		t.Fatal("expected not connected after Close")
	}
	if err := m.Send([]byte("x")); err == nil {
		t.Fatal("expected error when sending after Close")
	}
	if m.Receive() != nil {
		t.Fatal("expected nil receive channel after Close")
	}
	// Close again should be a no-op.
	m.Close()
}

func TestReceiveNilWhenNotConnected(t *testing.T) {
	m := New()
	if m.Receive() != nil {
		t.Fatal("expected nil receive channel when not connected")
	}
}

func TestGlobalFunctions(t *testing.T) {
	if err := Init("127.0.0.1:5061"); err != nil {
		t.Fatalf("global Init: %v", err)
	}
	if !IsConnected() {
		t.Fatal("expected global connected")
	}
	if err := Send([]byte("g")); err != nil {
		t.Fatalf("global Send: %v", err)
	}
	select {
	case <-Receive():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for global receive")
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := New()
	if err := m.Init("127.0.0.1:5062"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Send([]byte("x"))
			_ = m.IsConnected()
			_ = m.Receive()
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SoRcvbuf <= 0 {
		t.Errorf("SoRcvbuf = %d, want > 0", cfg.SoRcvbuf)
	}
	if cfg.SoSndbuf <= 0 {
		t.Errorf("SoSndbuf = %d, want > 0", cfg.SoSndbuf)
	}
	if cfg.Autoclose <= 0 {
		t.Errorf("Autoclose = %d, want > 0", cfg.Autoclose)
	}
	if !cfg.AssocTracking {
		t.Error("AssocTracking should default true")
	}
	if !cfg.AssocReuse {
		t.Error("AssocReuse should default true")
	}
	if cfg.MaxAssocs != -1 {
		t.Errorf("MaxAssocs = %d, want -1", cfg.MaxAssocs)
	}
	if !cfg.Nodelay {
		t.Error("Nodelay should default true")
	}
}

func TestSetConfig(t *testing.T) {
	m := New()
	orig := m.Config()
	cfg := orig
	cfg.SoRcvbuf = 65536
	cfg.Autoclose = 0
	cfg.Nodelay = false
	m.SetConfig(cfg)
	got := m.Config()
	if got.SoRcvbuf != 65536 {
		t.Errorf("SoRcvbuf = %d, want 65536", got.SoRcvbuf)
	}
	if got.Autoclose != 0 {
		t.Errorf("Autoclose = %d, want 0", got.Autoclose)
	}
	if got.Nodelay {
		t.Error("Nodelay should be false after SetConfig")
	}
}

func TestNewWithConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SoRcvbuf = 32768
	m := NewWithConfig(cfg)
	if m.Config().SoRcvbuf != 32768 {
		t.Errorf("SoRcvbuf = %d, want 32768", m.Config().SoRcvbuf)
	}
}

// ---------------------------------------------------------------------------
// Association tracker tests
// ---------------------------------------------------------------------------

func TestAssociationTracker_TrackUpThenDown(t *testing.T) {
	tr := newAssociationTracker()
	// COMM_UP for assoc 1.
	id1 := tr.track(1, &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1234}, assocUpSeen)
	if id1 == 0 {
		t.Fatal("expected non-zero local id")
	}
	if tr.count() != 1 {
		t.Fatalf("count = %d, want 1", tr.count())
	}
	// Same assoc + COMM_DOWN → entry should be removed.
	tr.track(1, nil, assocDownSeen)
	if tr.count() != 0 {
		t.Fatalf("count = %d, want 0 after UP+DOWN", tr.count())
	}
}

func TestAssociationTracker_TrackMultiAssocs(t *testing.T) {
	tr := newAssociationTracker()
	id1 := tr.track(10, nil, assocUpSeen)
	id2 := tr.track(20, nil, assocUpSeen)
	id3 := tr.track(30, nil, assocUpSeen)
	if id1 == id2 || id2 == id3 || id1 == id3 {
		t.Fatalf("expected distinct local ids: %d %d %d", id1, id2, id3)
	}
	if tr.count() != 3 {
		t.Fatalf("count = %d, want 3", tr.count())
	}
}

func TestAssociationTracker_GetByLocalID(t *testing.T) {
	tr := newAssociationTracker()
	id := tr.track(42, &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5060}, assocUpSeen)
	state := tr.getByLocalID(id)
	if state == nil {
		t.Fatal("expected non-nil state for known local id")
	}
	if state.AssocID != 42 {
		t.Errorf("AssocID = %d, want 42", state.AssocID)
	}
	if state.LocalID != id {
		t.Errorf("LocalID = %d, want %d", state.LocalID, id)
	}
	if tr.getByLocalID(9999) != nil {
		t.Fatal("expected nil state for unknown local id")
	}
}

func TestAssociationTracker_Flush(t *testing.T) {
	tr := newAssociationTracker()
	tr.track(1, nil, assocUpSeen)
	tr.track(2, nil, assocUpSeen)
	tr.track(3, nil, assocUpSeen)
	if tr.count() != 3 {
		t.Fatalf("count = %d, want 3", tr.count())
	}
	tr.flush()
	if tr.count() != 0 {
		t.Fatalf("count = %d, want 0 after flush", tr.count())
	}
	// nextID should reset so the first new assoc after flush gets id 1.
	id := tr.track(99, nil, assocUpSeen)
	if id != 1 {
		t.Errorf("first local id after flush = %d, want 1", id)
	}
}

func TestAssociationTracker_DownBeforeUp(t *testing.T) {
	// If DOWN arrives before UP (unlikely but possible on restart), the
	// entry is created and then immediately removed by a subsequent UP.
	tr := newAssociationTracker()
	id := tr.track(7, nil, assocDownSeen)
	if tr.count() != 1 {
		t.Fatalf("count = %d, want 1 after DOWN-only", tr.count())
	}
	tr.track(7, nil, assocUpSeen)
	if tr.count() != 0 {
		t.Fatalf("count = %d, want 0 after DOWN+UP", tr.count())
	}
	_ = id
}

// ---------------------------------------------------------------------------
// SCTPListener tests
// ---------------------------------------------------------------------------

func TestSCTPListener_NotAvailableInContainer(t *testing.T) {
	// In CI containers the kernel SCTP module is typically not loaded,
	// so ListenAndServe should return ErrSCTPNotAvailable. If it does
	// happen to be loaded, the test still validates that the listener
	// reaches the running state and can be shut down cleanly.
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	err := l.ListenAndServe()
	if err == nil {
		// Kernel SCTP is available: shut down the listener cleanly.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := l.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		return
	}
	if !errors.Is(err, ErrSCTPNotAvailable) {
		t.Fatalf("ListenAndServe = %v, want ErrSCTPNotAvailable", err)
	}
}

func TestSCTPListener_ShutdownNotStarted(t *testing.T) {
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Shutdown on an unstarted listener should be a no-op.
	if err := l.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown on unstarted listener: %v", err)
	}
}

func TestSCTPListener_StatsZeroBeforeStart(t *testing.T) {
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	s := l.Stats()
	if s.ConnectionsNo != 0 || s.TotalConns != 0 || s.MsgsReceived != 0 ||
		s.MsgsSent != 0 || s.MsgsSendFailed != 0 {
		t.Fatalf("Stats = %+v, want all zeros", s)
	}
	if l.TrackedAssocs() != 0 {
		t.Fatalf("TrackedAssocs = %d, want 0", l.TrackedAssocs())
	}
}

func TestSCTPListener_SendWhenNotRunning(t *testing.T) {
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	err := l.Send(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}, []byte("x"))
	if err == nil {
		t.Fatal("expected error when sending on unstarted listener")
	}
	err = l.Send(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}, nil)
	if err != ErrEmptyData {
		t.Fatalf("Send(nil) = %v, want ErrEmptyData", err)
	}
}

func TestSCTPListener_HandleAssocChange(t *testing.T) {
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	// Build a fake sctp_assoc_change notification:
	//   sac_type=1 (SCTP_ASSOC_CHANGE), sac_state=1 (COMM_UP),
	//   sac_assoc_id=0xCAFEBABE.
	data := make([]byte, 20)
	binary.LittleEndian.PutUint16(data[0:2], sctpAssocChange) // type
	binary.LittleEndian.PutUint16(data[8:10], sctpCommUp)    // state
	binary.LittleEndian.PutUint32(data[16:20], 0xCAFEBABE)   // assoc_id
	l.handleAssocChange(data)
	if l.stats.ConnectionsNo != 1 {
		t.Errorf("ConnectionsNo = %d, want 1", l.stats.ConnectionsNo)
	}
	if l.stats.TotalConns != 1 {
		t.Errorf("TotalConns = %d, want 1", l.stats.TotalConns)
	}
	if l.TrackedAssocs() != 1 {
		t.Errorf("TrackedAssocs = %d, want 1", l.TrackedAssocs())
	}
	// Now COMM_LOST.
	binary.LittleEndian.PutUint16(data[8:10], sctpCommLost)
	l.handleAssocChange(data)
	if l.stats.ConnectionsNo != 0 {
		t.Errorf("ConnectionsNo = %d, want 0 after COMM_LOST", l.stats.ConnectionsNo)
	}
	if l.TrackedAssocs() != 0 {
		t.Errorf("TrackedAssocs = %d, want 0 after UP+DOWN", l.TrackedAssocs())
	}
}

func TestSCTPListener_HandleAssocChange_NoTracking(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AssocTracking = false
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, cfg, nil)
	data := make([]byte, 20)
	binary.LittleEndian.PutUint16(data[0:2], sctpAssocChange)
	binary.LittleEndian.PutUint16(data[8:10], sctpCommUp)
	binary.LittleEndian.PutUint32(data[16:20], 42)
	l.handleAssocChange(data)
	if l.TrackedAssocs() != 0 {
		t.Errorf("TrackedAssocs = %d, want 0 when tracking disabled", l.TrackedAssocs())
	}
	if l.stats.ConnectionsNo != 1 {
		t.Errorf("ConnectionsNo = %d, want 1 even without tracking", l.stats.ConnectionsNo)
	}
}

func TestSCTPListener_HandleNotification(t *testing.T) {
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	// Too short.
	l.handleNotification([]byte{1, 2, 3, 4})
	if l.stats.MsgsSendFailed != 0 {
		t.Errorf("MsgsSendFailed = %d, want 0 after short notification", l.stats.MsgsSendFailed)
	}
	// SCTP_SEND_FAILED.
	data := make([]byte, 16)
	binary.LittleEndian.PutUint16(data[0:2], sctpSendFailed)
	l.handleNotification(data)
	if atomic.LoadInt64(&l.stats.MsgsSendFailed) != 1 {
		t.Errorf("MsgsSendFailed = %d, want 1", atomic.LoadInt64(&l.stats.MsgsSendFailed))
	}
}

func TestSCTPListener_HandleNotification_UnknownType(t *testing.T) {
	l := NewSCTPListener(net.IPv4(127, 0, 0, 1), 0, DefaultConfig(), nil)
	// Unknown notification type — should be silently ignored.
	data := make([]byte, 16)
	binary.LittleEndian.PutUint16(data[0:2], 9999)
	l.handleNotification(data)
}

// ---------------------------------------------------------------------------
// SCTPModule tests
// ---------------------------------------------------------------------------

func TestSCTPModule_IsRealMode(t *testing.T) {
	m := New()
	// In containers without kernel SCTP, Init falls back to simulation
	// so IsRealMode returns false.
	if err := m.Init("127.0.0.1:5063"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.IsRealMode() {
		// If kernel SCTP is available, the listener is real.
		// Just validate the relationship holds.
		if m.listener == nil {
			t.Error("IsRealMode true but listener nil")
		}
	} else {
		if m.listener != nil {
			t.Error("IsRealMode false but listener non-nil")
		}
	}
}

func TestSCTPModule_StatsInSimulation(t *testing.T) {
	m := New()
	if err := m.Init("127.0.0.1:5064"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	s := m.Stats()
	// Simulation mode returns zero stats.
	if s.ConnectionsNo != 0 || s.TotalConns != 0 {
		t.Errorf("Stats = %+v, want zeros in simulation", s)
	}
	if m.TrackedAssocs() != 0 {
		t.Errorf("TrackedAssocs = %d, want 0 in simulation", m.TrackedAssocs())
	}
}

func TestSCTPModule_SendSimulationMirrorsToRx(t *testing.T) {
	m := New()
	if err := m.Init("127.0.0.1:5065"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Skip in real mode — the loopback mirror only works in simulation.
	if m.IsRealMode() {
		return
	}
	if err := m.Send([]byte("mirror-test")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-m.Receive():
		if string(got) != "mirror-test" {
			t.Fatalf("got %q, want mirror-test", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mirrored data")
	}
}

func TestSCTPModule_SendFullBuffer(t *testing.T) {
	m := New()
	if err := m.Init("127.0.0.1:5066"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.IsRealMode() {
		return // test only valid in simulation mode.
	}
	// Drain the rx buffer by overfilling it.
	for i := 0; i < 100; i++ {
		_ = m.Send([]byte{byte(i)})
	}
	// After 64 (the buffer size) the simulation drops additional sends
	// silently (non-blocking) or returns an error. Either is acceptable;
	// we just ensure Send doesn't panic.
}

func TestSCTPModule_Addr(t *testing.T) {
	m := New()
	if m.Addr() != "" {
		t.Fatalf("Addr = %q, want empty before Init", m.Addr())
	}
	if err := m.Init("127.0.0.1:5067"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.Addr() != "127.0.0.1:5067" {
		t.Fatalf("Addr = %q, want 127.0.0.1:5067", m.Addr())
	}
}

// ---------------------------------------------------------------------------
// Low-level helper tests
// ---------------------------------------------------------------------------

func TestExtractAssocID(t *testing.T) {
	// Build a cmsg with a fake sctp_sndrcvinfo carrying assoc_id=0xDEADBEEF.
	sinfo := buildSndRcvInfo(0, 0, 0, 0)
	sinfo.assocID = 0xDEADBEEF
	cmsg := buildSndRcvCmsg(sinfo)
	got := extractAssocID(cmsg)
	if got != 0xDEADBEEF {
		t.Errorf("extractAssocID = 0x%X, want 0xDEADBEEF", got)
	}
}

func TestExtractAssocID_Empty(t *testing.T) {
	if extractAssocID(nil) != 0 {
		t.Error("expected 0 for nil cmsg")
	}
	if extractAssocID([]byte{}) != 0 {
		t.Error("expected 0 for empty cmsg")
	}
}

func TestExtractAssocID_NotSCTP(t *testing.T) {
	// Build a cmsg with a different level — should return 0.
	buf := make([]byte, 40)
	binary.LittleEndian.PutUint64(buf[0:8], 36) // cmsg_len
	binary.LittleEndian.PutUint32(buf[8:12], 0) // SOL_IP, not SOL_SCTP
	binary.LittleEndian.PutUint32(buf[12:16], 0)
	if extractAssocID(buf) != 0 {
		t.Error("expected 0 for non-SCTP cmsg")
	}
}

func TestBuildSndRcvCmsg_RoundTrip(t *testing.T) {
	sinfo := buildSndRcvInfo(7, sctpUnordered, 1000, 5)
	sinfo.ppid = 0x01020304
	sinfo.assocID = 0xCAFEBABE
	cmsg := buildSndRcvCmsg(sinfo)
	// The cmsg should be parseable and yield the same assoc_id.
	if got := extractAssocID(cmsg); got != 0xCAFEBABE {
		t.Errorf("round-trip assocID = 0x%X, want 0xCAFEBABE", got)
	}
}

func TestBuildSndRcvInfo(t *testing.T) {
	si := buildSndRcvInfo(3, sctpUnordered, 500, 2)
	if si.stream != 3 {
		t.Errorf("stream = %d, want 3", si.stream)
	}
	if si.flags != sctpUnordered {
		t.Errorf("flags = 0x%X, want 0x%X", si.flags, sctpUnordered)
	}
	if si.ttl != 500 {
		t.Errorf("ttl = %d, want 500", si.ttl)
	}
	if si.context != 2 {
		t.Errorf("context = %d, want 2", si.context)
	}
}

// ---------------------------------------------------------------------------
// Default singleton tests
// ---------------------------------------------------------------------------

func TestDefaultSCTP(t *testing.T) {
	m1 := DefaultSCTP()
	m2 := DefaultSCTP()
	if m1 != m2 {
		t.Fatal("DefaultSCTP should return the same instance")
	}
}

func TestInitResets(t *testing.T) {
	// The package-level Init(addr) initialises the existing singleton
	// (it does not replace it) — the same singleton keeps being used but
	// its bound address changes.
	if err := Init("127.0.0.1:5070"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	m1 := DefaultSCTP()
	if m1.Addr() != "127.0.0.1:5070" {
		t.Fatalf("Addr = %q, want 127.0.0.1:5070", m1.Addr())
	}
	if err := Init("127.0.0.1:5071"); err != nil {
		t.Fatalf("Init again: %v", err)
	}
	m2 := DefaultSCTP()
	if m1 != m2 {
		t.Fatal("expected the same singleton instance after re-Init")
	}
	if m2.Addr() != "127.0.0.1:5071" {
		t.Fatalf("Addr = %q, want 127.0.0.1:5071 after re-Init", m2.Addr())
	}
}

// ---------------------------------------------------------------------------
// Concurrent access to tracker
// ---------------------------------------------------------------------------

func TestAssociationTracker_Concurrent(t *testing.T) {
	tr := newAssociationTracker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			assocID := uint32(n + 1)
			tr.track(assocID, nil, assocUpSeen)
			tr.track(assocID, nil, assocDownSeen)
			tr.getByLocalID(assocID)
			_ = tr.count()
		}(i)
	}
	wg.Wait()
	// All UP+DOWN pairs should have been removed.
	if tr.count() != 0 {
		t.Errorf("count = %d, want 0 after all UP+DOWN pairs", tr.count())
	}
}
