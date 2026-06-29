// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SCTP transport layer - real SOCK_SEQPACKET implementation.
 * Port of the kamailio sctp module (src/modules/sctp) and core SCTP
 * dispatcher (src/core/sctp_core).
 *
 * Kamailio uses one-to-many (SOCK_SEQPACKET) SCTP sockets: a single
 * listening socket accepts new associations automatically via the kernel,
 * and recvmsg() returns either a data message or a notification
 * (SCTP_ASSOC_CHANGE, SCTP_PEER_ADDR_CHANGE, etc.). This mirrors the C
 * sctp_rcv_loop() / sctp_msg_send() / sctp_handle_assoc_change() flow.
 *
 * The kernel SCTP module may not be available (e.g. in containers or
 * non-Linux environments). In that case the listener returns
 * ErrSCTPNotAvailable from ListenAndServe, allowing the caller to fall
 * back to another transport. All other operations (Config, Stats, the
 * association tracker) work without kernel support.
 *
 * The module also exposes the simulated API (Init/Send/Receive/Close) for
 * backwards compatibility with existing callers; when kernel SCTP is
 * available, Init performs a real bind+listen instead of just marking the
 * module connected.
 *
 * It is safe for concurrent use.
 */

package sctp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// Protocol constants (mirrors /usr/include/linux/sctp.h)
// ---------------------------------------------------------------------------

// IPPROTO_SCTP is the IP protocol number for SCTP (132).
// On Linux, this value doubles as the SOL_SCTP socket-option level.
const (
	IPPROTO_SCTP = 132 // syscall.IPPROTO_SCTP
	SOL_SCTP     = 132 // Linux: SOL_SCTP == IPPROTO_SCTP
)

// SCTP socket option names (mirrors linux/sctp.h).
const (
	sctpRtoInfo            = 0
	sctpAssocInfo          = 1
	sctpInitMsg            = 2
	sctpNodelay            = 3
	sctpAutoclose          = 4
	sctpDisableFragments   = 8
	sctpPeerAddrParams     = 9
	sctpEvents             = 11
	sctpFragmentInterleave = 18
	sctpPartialDeliveryPt  = 19
	sctpMaxBurst           = 20
	sctpDelayedSack         = 16
)

// SCTP notification / event types (mirrors sctp_sn_type enum).
const (
	sctpSnTypeBase     = 0
	sctpDataIOEvent    = sctpSnTypeBase       // 0
	sctpAssocChange    = sctpSnTypeBase + 1   // 1
	sctpPeerAddrChange = sctpSnTypeBase + 2   // 2
	sctpSendFailed     = sctpSnTypeBase + 3   // 3
	sctpShutdownEvent  = sctpSnTypeBase + 4   // 4
)

// SCTP_ASSOC_CHANGE states (mirrors sctp_spinfo_state).
const (
	sctpCommUp        = 1
	sctpCommLost      = 2
	sctpRestart       = 3
	sctpShutdownComp  = 4
	sctpCantStrAssoc  = 5
)

// SCTP send flags.
const (
	sctpUnordered = 0x0400 // SCTP_UNORDERED
	msgNotification = 0x800 // MSG_NOTIFICATION
	msgEOR          = 0x80  // MSG_EOR
	msgDontWait     = 0x40  // MSG_DONTWAIT
)

// Buffer sizes (mirrors C MAX_RECV_BUFFER_SIZE).
const (
	maxRecvBufferSize = 256 * 1024
	maxSendBufferSize = 256 * 1024
)

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrSCTPNotAvailable is returned when the kernel SCTP module is not
	// loaded or the platform does not support SOCK_SEQPACKET.
	ErrSCTPNotAvailable = errors.New("sctp: kernel SCTP not available")
	// ErrNotConnected is returned when Send is called before a successful
	// Init / ListenAndServe.
	ErrNotConnected = errors.New("sctp: not connected")
	// ErrEmptyData is returned by Send for empty data.
	ErrEmptyData = errors.New("sctp: empty data")
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds the SCTP socket options, mirroring the C cfg_group_sctp.
type Config struct {
	SoRcvbuf       int  // SO_RCVBUF
	SoSndbuf       int  // SO_SNDBUF
	Autoclose      int  // seconds (SCTP_AUTOCLOSE), 0 = disabled
	SendTTL        int  // ms (sinfo_timetolive), 0 = no PR-SCTP
	SendRetries    int  // 0..9
	AssocTracking  bool // track associations
	AssocReuse     bool // reuse req-assoc for reply
	MaxAssocs      int  // -1 = unlimited
	Nodelay        bool // SCTP_NODELAY
}

// DefaultConfig returns sensible defaults mirroring the C defaults.
func DefaultConfig() Config {
	return Config{
		SoRcvbuf:      maxRecvBufferSize,
		SoSndbuf:      maxSendBufferSize,
		Autoclose:     180,
		SendTTL:       32000,
		SendRetries:   0,
		AssocTracking: true,
		AssocReuse:    true,
		MaxAssocs:     -1,
		Nodelay:       true,
	}
}

// ---------------------------------------------------------------------------
// Association tracking
// ---------------------------------------------------------------------------

// AssociationState tracks the lifecycle of one SCTP association.
type AssociationState struct {
	AssocID   uint32    // kernel-assigned association id
	LocalID   uint32    // Kamailio-Go monotonically-increasing id
	Remote    net.Addr  // remote address
	CreatedAt time.Time
	Flags     uint8     // SCTP_CON_UP_SEEN / RCV_SEEN / DOWN_SEEN
}

const (
	assocUpSeen   uint8 = 1 << 0
	assocRcvSeen  uint8 = 1 << 1
	assocDownSeen uint8 = 1 << 2
)

// associationTracker maps kernel assoc_ids to local tracking state,
// mirroring the C sctp_con_track / sctp_con_get_assoc hash tables.
type associationTracker struct {
	mu       sync.RWMutex
	byAssoc  map[uint32]*AssociationState // kernel assoc_id -> state
	byLocal  map[uint32]*AssociationState // local id -> state
	nextID   uint32
}

func newAssociationTracker() *associationTracker {
	return &associationTracker{
		byAssoc: make(map[uint32]*AssociationState),
		byLocal: make(map[uint32]*AssociationState),
		nextID:  1,
	}
}

// track records an association state change. Returns the local id.
func (t *associationTracker) track(assocID uint32, remote net.Addr, event uint8) uint32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.byAssoc[assocID]
	if !ok {
		state = &AssociationState{
			AssocID:   assocID,
			LocalID:   t.nextID,
			Remote:    remote,
			CreatedAt: time.Now(),
		}
		t.nextID++
		t.byAssoc[assocID] = state
		t.byLocal[state.LocalID] = state
	}
	state.Flags |= event
	// If we have seen UP then DOWN (or DOWN then UP), remove the entry.
	if state.Flags&assocUpSeen != 0 && state.Flags&assocDownSeen != 0 {
		delete(t.byAssoc, assocID)
		delete(t.byLocal, state.LocalID)
		return state.LocalID
	}
	return state.LocalID
}

// getByLocalID returns the association state for a local id.
func (t *associationTracker) getByLocalID(localID uint32) *AssociationState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.byLocal[localID]
}

// count returns the number of tracked associations.
func (t *associationTracker) count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.byAssoc)
}

// flush removes all tracked associations.
func (t *associationTracker) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byAssoc = make(map[uint32]*AssociationState)
	t.byLocal = make(map[uint32]*AssociationState)
	t.nextID = 1
}

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

// Stats mirrors the C sctp_gen_info.
type Stats struct {
	ConnectionsNo  int  // currently open associations
	TotalConns     int64 // cumulative counter
	MsgsReceived   int64
	MsgsSent       int64
	MsgsSendFailed int64
}

// ---------------------------------------------------------------------------
// SCTPListener - real SOCK_SEQPACKET listener
// ---------------------------------------------------------------------------

// MessageHandler is the callback for received SCTP messages.
type MessageHandler func(data []byte, srcAddr net.Addr, assocID uint32)

// SCTPListener is a real SCTP SOCK_SEQPACKET listener.
// C: sctp_server / sctp_rcv_loop
type SCTPListener struct {
	mu        sync.Mutex
	fd        int          // raw socket file descriptor (-1 = closed)
	si        socketInfo   // local bind address
	cfg       Config
	handler   MessageHandler
	tracker   *associationTracker
	stats     Stats
	running   atomic.Bool
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// socketInfo is a lightweight local-socket descriptor (avoids importing
// the transport package which would create a cycle).
type socketInfo struct {
	IP   net.IP
	Port uint16
}

// NewSCTPListener creates an SCTP listener bound to the given address.
// The socket is not yet created; call ListenAndServe to open it.
func NewSCTPListener(ip net.IP, port uint16, cfg Config, handler MessageHandler) *SCTPListener {
	return &SCTPListener{
		fd:      -1,
		si:      socketInfo{IP: ip, Port: port},
		cfg:     cfg,
		handler: handler,
		tracker: newAssociationTracker(),
	}
}

// ListenAndServe opens the SCTP socket and starts the receive loop.
// Returns ErrSCTPNotAvailable when the kernel SCTP module is not loaded.
//
//	C: init_sctp() + sctp_rcv_loop()
func (l *SCTPListener) ListenAndServe() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running.Load() {
		return errors.New("sctp: listener already running")
	}

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET, IPPROTO_SCTP)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSCTPNotAvailable, err)
	}

	// Apply common socket options.
	if err := l.applySockopts(fd); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("sctp: setsockopt: %w", err)
	}

	// Bind.
	var addr [4]byte
	copy(addr[:], l.si.IP.To4())
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Port: int(l.si.Port), Addr: addr}); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("sctp: bind %s:%d: %w", l.si.IP, l.si.Port, err)
	}

	// Listen enables accepting new associations on the SEQPACKET socket.
	if err := syscall.Listen(fd, 1); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("sctp: listen: %w", err)
	}

	l.fd = fd
	l.running.Store(true)
	l.stopCh = make(chan struct{})

	l.wg.Add(1)
	go l.receiveLoop()
	return nil
}

// applySockopts sets the common SCTP socket options.
// C: sctp_init_sock_opt_common()
func (l *SCTPListener) applySockopts(fd int) error {
	// SO_RCVBUF / SO_SNDBUF.
	if l.cfg.SoRcvbuf > 0 {
		if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, l.cfg.SoRcvbuf); err != nil {
			return err
		}
	}
	if l.cfg.SoSndbuf > 0 {
		if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, l.cfg.SoSndbuf); err != nil {
			return err
		}
	}
	// SO_REUSEADDR.
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	// SCTP_NODELAY.
	if l.cfg.Nodelay {
		if err := syscall.SetsockoptInt(fd, SOL_SCTP, sctpNodelay, 1); err != nil {
			return err
		}
	}
	// SCTP_AUTOCLOSE (seconds).
	if l.cfg.Autoclose > 0 {
		if err := syscall.SetsockoptInt(fd, SOL_SCTP, sctpAutoclose, l.cfg.Autoclose); err != nil {
			return err
		}
	}
	// SCTP_DISABLE_FRAGMENTS = 0 (fragmentation enabled so the kernel
	// fragments/reassembles large SIP messages).
	if err := syscall.SetsockoptInt(fd, SOL_SCTP, sctpDisableFragments, 0); err != nil {
		// Non-fatal: some kernels don't support this.
		_ = err
	}
	// SCTP_EVENTS — subscribe to association and address events.
	// The sctp_event_subscribe struct is 13 bytes (one uint8 per event).
	events := make([]byte, 13)
	events[0] = 1 // sctp_data_io_event → per-msg sctp_sndrcvinfo
	events[1] = 1 // sctp_association_event → SCTP_ASSOC_CHANGE
	events[2] = 1 // sctp_address_event → SCTP_PEER_ADDR_CHANGE
	events[3] = 1 // sctp_send_failure_event → SCTP_SEND_FAILED
	events[4] = 1 // sctp_peer_error_event
	events[5] = 1 // sctp_shutdown_event
	// syscall has no SetsockoptBuffer on Linux; SetsockoptString passes
	// the raw bytes (no NUL terminator) to setsockopt(2).
	if err := syscall.SetsockoptString(fd, SOL_SCTP, sctpEvents, string(events)); err != nil {
		return fmt.Errorf("SCTP_EVENTS: %w", err)
	}
	return nil
}

// receiveLoop is the main SCTP receive loop.
// C: sctp_rcv_loop()
func (l *SCTPListener) receiveLoop() {
	defer l.wg.Done()
	buf := make([]byte, maxRecvBufferSize)
	cmsgBuf := make([]byte, 256)
	for l.running.Load() {
		// Set a read deadline via a select on stopCh.
		l.mu.Lock()
		fd := l.fd
		l.mu.Unlock()
		if fd < 0 {
			return
		}
		// recvmsg with a short deadline so we can check stopCh.
		var srcAddr syscall.Sockaddr
		n, cmsg, flags, _, err := recvmsg(fd, buf, cmsgBuf, 0)
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EINTR) {
				select {
				case <-l.stopCh:
					return
				case <-time.After(50 * time.Millisecond):
				}
				continue
			}
			if errors.Is(err, syscall.EBADF) {
				return
			}
			continue
		}
		_ = srcAddr
		_ = cmsg
		// Notification or data?
		if flags&msgNotification != 0 {
			l.handleNotification(buf[:n])
			continue
		}
		if flags&msgEOR == 0 {
			// Partial delivery not supported.
			continue
		}
		// Extract assoc_id from cmsg (SCTP_SNDRCV).
		assocID := extractAssocID(cmsg)
		// Build a copy of the data.
		data := make([]byte, n)
		copy(data, buf[:n])
		atomic.AddInt64(&l.stats.MsgsReceived, 1)
		if l.cfg.AssocTracking {
			_ = l.tracker.track(assocID, nil, assocRcvSeen)
		}
		if l.handler != nil {
			l.handler(data, nil, assocID)
		}
	}
}

// handleNotification dispatches an SCTP notification.
// C: sctp_handle_notification() / sctp_handle_assoc_change()
func (l *SCTPListener) handleNotification(data []byte) {
	if len(data) < 16 {
		return
	}
	// sn_type is at offset 0 (uint16 LE).
	snType := binary.LittleEndian.Uint16(data[0:2])
	switch snType {
	case sctpAssocChange:
		l.handleAssocChange(data)
	case sctpPeerAddrChange:
		// Multi-homing path event — logged but no action needed.
	case sctpSendFailed:
		atomic.AddInt64(&l.stats.MsgsSendFailed, 1)
	case sctpShutdownEvent:
		// Shutdown event — assoc change will follow.
	}
}

// handleAssocChange processes an SCTP_ASSOC_CHANGE notification.
// C: sctp_handle_assoc_change()
func (l *SCTPListener) handleAssocChange(data []byte) {
	// struct sctp_assoc_change {
	//   uint16_t sac_type;       // 0
	//   uint16_t sac_flags;      // 2
	//   uint32_t sac_length;     // 4
	//   uint16_t sac_state;      // 8
	//   uint16_t sac_error;      // 10
	//   uint16_t sac_outbound_streams;  // 12
	//   uint16_t sac_inbound_streams;   // 14
	//   uint32_t sac_assoc_id;    // 16
	// }
	if len(data) < 20 {
		return
	}
	state := binary.LittleEndian.Uint16(data[8:10])
	assocID := binary.LittleEndian.Uint32(data[16:20])
	switch state {
	case sctpCommUp:
		if l.cfg.AssocTracking {
			l.tracker.track(assocID, nil, assocUpSeen)
		}
		l.mu.Lock()
		l.stats.ConnectionsNo++
		l.stats.TotalConns++
		l.mu.Unlock()
	case sctpCommLost, sctpShutdownComp, sctpCantStrAssoc:
		if l.cfg.AssocTracking {
			l.tracker.track(assocID, nil, assocDownSeen)
		}
		l.mu.Lock()
		if l.stats.ConnectionsNo > 0 {
			l.stats.ConnectionsNo--
		}
		l.mu.Unlock()
	case sctpRestart:
		// No action.
	}
}

// Send sends data to the specified destination.
// C: sctp_msg_send()
func (l *SCTPListener) Send(dst *net.UDPAddr, data []byte) error {
	if len(data) == 0 {
		return ErrEmptyData
	}
	if !l.running.Load() {
		return ErrNotConnected
	}
	l.mu.Lock()
	fd := l.fd
	l.mu.Unlock()
	if fd < 0 {
		return ErrNotConnected
	}
	var addr [4]byte
	copy(addr[:], dst.IP.To4())
	// Build sctp_sndrcvinfo: unordered, default stream 0.
	sinfo := buildSndRcvInfo(0, sctpUnordered, uint32(l.cfg.SendTTL), uint32(l.cfg.SendRetries))
	cmsg := buildSndRcvCmsg(sinfo)
	if err := sendmsg(fd, data, &syscall.SockaddrInet4{Port: int(dst.Port), Addr: addr}, cmsg, msgDontWait); err != nil {
		return fmt.Errorf("sctp: send: %w", err)
	}
	atomic.AddInt64(&l.stats.MsgsSent, 1)
	return nil
}

// Shutdown stops the listener.
func (l *SCTPListener) Shutdown(ctx context.Context) error {
	if !l.running.Load() {
		return nil
	}
	l.running.Store(false)
	l.mu.Lock()
	close(l.stopCh)
	fd := l.fd
	l.fd = -1
	l.mu.Unlock()
	if fd >= 0 {
		syscall.Close(fd)
	}
	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stats returns a snapshot of the listener statistics.
func (l *SCTPListener) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.stats
	s.MsgsReceived = atomic.LoadInt64(&l.stats.MsgsReceived)
	s.MsgsSent = atomic.LoadInt64(&l.stats.MsgsSent)
	s.MsgsSendFailed = atomic.LoadInt64(&l.stats.MsgsSendFailed)
	return s
}

// TrackedAssocs returns the number of currently tracked associations.
func (l *SCTPListener) TrackedAssocs() int {
	return l.tracker.count()
}

// ---------------------------------------------------------------------------
// SCTPModule — high-level module (compatible with the existing API)
// ---------------------------------------------------------------------------

// SCTPModule is the high-level SCTP module. It wraps either a real
// SCTPListener (when kernel SCTP is available) or a loopback simulation
// (when it is not).
type SCTPModule struct {
	mu        sync.Mutex
	addr      string
	connected atomic.Bool
	rx        chan []byte

	// Real listener (nil when running in simulation mode).
	listener *SCTPListener

	// Config applied to the real listener.
	cfg Config
}

// New creates an SCTPModule that is not yet connected.
func New() *SCTPModule {
	return &SCTPModule{cfg: DefaultConfig()}
}

// NewWithConfig creates an SCTPModule with the supplied configuration.
func NewWithConfig(cfg Config) *SCTPModule {
	return &SCTPModule{cfg: cfg}
}

// SetConfig replaces the configuration. Must be called before Init.
func (m *SCTPModule) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// Config returns the current configuration.
func (m *SCTPModule) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Init configures the peer address and starts the module. It first tries
// to create a real SCTP listener; if the kernel SCTP module is not
// available it falls back to the loopback simulation.
//
// The address format is "host:port".
func (m *SCTPModule) Init(addr string) error {
	if addr == "" {
		return errors.New("sctp: empty address")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("sctp: invalid address %q: %w", addr, err)
	}
	port, err := net.LookupPort("udp", portStr) // "udp" works for port lookup
	if err != nil {
		return fmt.Errorf("sctp: invalid port %q: %w", portStr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		addrs, lerr := net.LookupIP(host)
		if lerr != nil || len(addrs) == 0 {
			return fmt.Errorf("sctp: cannot resolve %q: %v", host, lerr)
		}
		ip = addrs[0]
	}

	m.mu.Lock()
	m.addr = addr
	m.rx = make(chan []byte, 64)
	m.mu.Unlock()

	// Try to start a real listener.
	listener := NewSCTPListener(ip, uint16(port), m.cfg, func(data []byte, _ net.Addr, _ uint32) {
		m.mu.Lock()
		rx := m.rx
		m.mu.Unlock()
		if rx != nil {
			select {
			case rx <- data:
			default:
			}
		}
	})
	if err := listener.ListenAndServe(); err != nil {
		if errors.Is(err, ErrSCTPNotAvailable) {
			// Fall back to simulation mode.
			m.mu.Lock()
			m.listener = nil
			m.mu.Unlock()
			m.connected.Store(true)
			return nil
		}
		return err
	}
	m.mu.Lock()
	m.listener = listener
	m.mu.Unlock()
	m.connected.Store(true)
	return nil
}

// IsConnected reports whether Init has been called (and Close not yet).
func (m *SCTPModule) IsConnected() bool {
	return m.connected.Load()
}

// Send sends data. In real mode it uses the SCTP socket; in simulation
// mode it mirrors the data onto the receive channel.
func (m *SCTPModule) Send(data []byte) error {
	if len(data) == 0 {
		return ErrEmptyData
	}
	if !m.connected.Load() {
		return ErrNotConnected
	}
	m.mu.Lock()
	listener := m.listener
	rx := m.rx
	m.mu.Unlock()
	if listener != nil {
		// Real mode: we need a destination. In the module API the
		// destination is derived from Init's address.
		host, portStr, _ := net.SplitHostPort(m.addr)
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.New("sctp: invalid destination")
		}
		// Resolve port string to int.
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)
		return listener.Send(&net.UDPAddr{IP: ip, Port: port}, data)
	}
	// Simulation mode: mirror onto rx.
	cp := make([]byte, len(data))
	copy(cp, data)
	if rx != nil {
		select {
		case rx <- cp:
			return nil
		default:
			return errors.New("sctp: receive buffer full")
		}
	}
	return nil
}

// Receive returns the channel of inbound byte slices.
func (m *SCTPModule) Receive() <-chan []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rx
}

// Close shuts down the module.
func (m *SCTPModule) Close() {
	m.mu.Lock()
	if !m.connected.Load() {
		m.mu.Unlock()
		return
	}
	m.connected.Store(false)
	listener := m.listener
	m.listener = nil
	rx := m.rx
	m.rx = nil
	m.mu.Unlock()
	if listener != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = listener.Shutdown(ctx)
	}
	if rx != nil {
		// Drain and close.
		for {
			select {
			case <-rx:
			default:
				close(rx)
				return
			}
		}
	}
}

// Addr returns the configured peer address.
func (m *SCTPModule) Addr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.addr
}

// IsRealMode reports whether the module is using a real kernel SCTP socket.
func (m *SCTPModule) IsRealMode() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listener != nil
}

// Stats returns the listener statistics (zeros in simulation mode).
func (m *SCTPModule) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listener == nil {
		return Stats{}
	}
	return m.listener.Stats()
}

// TrackedAssocs returns the number of tracked associations (0 in simulation).
func (m *SCTPModule) TrackedAssocs() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listener == nil {
		return 0
	}
	return m.listener.TrackedAssocs()
}

// ---------------------------------------------------------------------------
// Low-level syscall wrappers
// ---------------------------------------------------------------------------

// recvmsg wraps syscall.Recvmsg, returning the ancillary data buffer.
func recvmsg(fd int, buf, cmsg []byte, flags int) (n int, cmsgData []byte, recvFlags int, from syscall.Sockaddr, err error) {
	var oobn int
	n, oobn, recvFlags, from, err = syscall.Recvmsg(fd, buf, cmsg, flags)
	if err != nil {
		return 0, nil, 0, nil, err
	}
	return n, cmsg[:oobn], recvFlags, from, nil
}

// sendmsg wraps syscall.Sendmsg.
func sendmsg(fd int, data []byte, to syscall.Sockaddr, cmsg []byte, flags int) error {
	return syscall.Sendmsg(fd, data, cmsg, to, flags)
}

// extractAssocID reads the sinfo_assoc_id from an SCTP_SNDRCV cmsg.
// The sctp_sndrcvinfo struct has assoc_id at offset 28 (uint32 LE),
// after stream(2)+ssn(2)+flags(2)+pad(2)+ppid(4)+context(4)+ttl(4)+
// tsn(4)+cumtsn(4).
func extractAssocID(cmsg []byte) uint32 {
	msgs, err := syscall.ParseSocketControlMessage(cmsg)
	if err != nil {
		return 0
	}
	for _, m := range msgs {
		if m.Header.Level == SOL_SCTP && m.Header.Type == 0 && len(m.Data) >= 32 {
			// sctp_sndrcvinfo: assoc_id at offset 28 (uint32 LE).
			return binary.LittleEndian.Uint32(m.Data[28:32])
		}
	}
	return 0
}

// buildSndRcvInfo constructs a sctp_sndrcvinfo struct (32 bytes on wire).
// C: struct sctp_sndrcvinfo
type sndRcvInfo struct {
	stream     uint16
	ssn        uint16
	flags      uint16
	ppid       uint32
	context    uint32
	ttl        uint32
	tsn        uint32
	cumtsn     uint32
	assocID    uint32
}

func buildSndRcvInfo(stream, flags, ttl, context uint32) sndRcvInfo {
	return sndRcvInfo{
		stream:  uint16(stream),
		flags:   uint16(flags),
		ttl:     ttl,
		context: context,
	}
}

// buildSndRcvCmsg wraps a sndRcvInfo in a CMSG header.
func buildSndRcvCmsg(sinfo sndRcvInfo) []byte {
	// cmsghdr on 64-bit Linux is 16 bytes (Len:8 + Level:4 + Type:4).
	// sctp_sndrcvinfo is 32 bytes (uint32 fields are 4-byte aligned, so
	// there is 2 bytes of padding after sinfo_flags):
	//   stream(2) + ssn(2) + flags(2) + pad(2) + ppid(4) + context(4) +
	//   ttl(4) + tsn(4) + cumtsn(4) + assoc_id(4) = 32
	// CMSG buffer is padded to 8-byte alignment: 16 + 32 = 48.
	const dataLen = 32
	const cmsgLen = 16 + dataLen
	buf := make([]byte, (cmsgLen+7)&^7)
	// cmsghdr.Len = total cmsg length (header + data, unpadded).
	binary.LittleEndian.PutUint64(buf[0:8], uint64(cmsgLen))
	// cmsghdr.Level (int32) at offset 8.
	binary.LittleEndian.PutUint32(buf[8:12], uint32(SOL_SCTP))
	// cmsghdr.Type (int32) at offset 12 — SCTP_SNDRCV (0).
	binary.LittleEndian.PutUint32(buf[12:16], 0)
	// sctp_sndrcvinfo starts at offset 16 (struct offset 0).
	const base = 16
	binary.LittleEndian.PutUint16(buf[base+0:base+2], sinfo.stream)
	binary.LittleEndian.PutUint16(buf[base+2:base+4], sinfo.ssn)
	binary.LittleEndian.PutUint16(buf[base+4:base+6], sinfo.flags)
	// padding at base+6..base+8
	binary.LittleEndian.PutUint32(buf[base+8:base+12], sinfo.ppid)
	binary.LittleEndian.PutUint32(buf[base+12:base+16], sinfo.context)
	binary.LittleEndian.PutUint32(buf[base+16:base+20], sinfo.ttl)
	binary.LittleEndian.PutUint32(buf[base+20:base+24], sinfo.tsn)
	binary.LittleEndian.PutUint32(buf[base+24:base+28], sinfo.cumtsn)
	binary.LittleEndian.PutUint32(buf[base+28:base+32], sinfo.assocID)
	return buf
}

// ---------------------------------------------------------------------------
// Package-level singleton
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *SCTPModule
)

// DefaultSCTP returns the process-wide module, creating it on first use.
func DefaultSCTP() *SCTPModule {
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

// Init is the package-level (re)initialiser.
func Init(addr string) error { return DefaultSCTP().Init(addr) }

// Send is the package-level wrapper.
func Send(data []byte) error { return DefaultSCTP().Send(data) }

// Receive is the package-level wrapper.
func Receive() <-chan []byte { return DefaultSCTP().Receive() }

// IsConnected is the package-level wrapper.
func IsConnected() bool { return DefaultSCTP().IsConnected() }
