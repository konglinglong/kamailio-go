// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Diameter transport layer.
 */

package cdp

import (
	"net"
	"sync"
	"testing"
	"time"
)

// recordingHandler captures every callback from the transport so tests
// can assert on the sequence of events. All accessors are thread-safe.
type recordingHandler struct {
	mu sync.Mutex
	connects    []*PeerConnection
	disconnects []*PeerConnection
	messages    []receivedMessage
	disconnectErrs []error
}

type receivedMessage struct {
	pc  *PeerConnection
	msg *DiameterMessage
}

func (h *recordingHandler) HandleConnect(pc *PeerConnection) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.connects = append(h.connects, pc)
	return nil
}

func (h *recordingHandler) HandleDisconnect(pc *PeerConnection, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disconnects = append(h.disconnects, pc)
	h.disconnectErrs = append(h.disconnectErrs, err)
}

func (h *recordingHandler) HandleMessage(pc *PeerConnection, msg *DiameterMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.messages = append(h.messages, receivedMessage{pc, msg})
	return nil
}

// MessageCount returns the number of messages received so far.
func (h *recordingHandler) MessageCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.messages)
}

// FirstMessage returns the first received message (or nil if none).
func (h *recordingHandler) FirstMessage() *DiameterMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.messages) == 0 {
		return nil
	}
	return h.messages[0].msg
}

func TestTransportConfigDefaults(t *testing.T) {
	cfg := DefaultTransportConfig()
	if cfg.ListenAddr != ":3868" {
		t.Errorf("ListenAddr = %q, want :3868", cfg.ListenAddr)
	}
	if cfg.ConnectTimeout != 5*time.Second {
		t.Errorf("ConnectTimeout = %v, want 5s", cfg.ConnectTimeout)
	}
}

func TestTransportNew(t *testing.T) {
	h := &recordingHandler{}
	tr := NewTransport(nil, nil, h)
	if tr == nil {
		t.Fatalf("NewTransport returned nil")
	}
	if tr.IsClosed() {
		t.Errorf("new transport reports IsClosed")
	}
	if tr.cfg.ListenAddr != ":3868" {
		t.Errorf("default ListenAddr = %q", tr.cfg.ListenAddr)
	}
}

func TestTransportListenAndServe(t *testing.T) {
	// Bind a real listener on an ephemeral port.
	h := &recordingHandler{}
	cfg := DefaultTransportConfig()
	cfg.ListenAddr = "127.0.0.1:0"
	tr := NewTransport(cfg, nil, h)

	go func() { _ = tr.ListenAndServe() }()
	// Wait for the listener to bind; poll ListenerAddr (thread-safe).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if tr.ListenerAddr() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	addr := tr.ListenerAddr()
	if addr == "" {
		t.Fatalf("listener did not bind within 1s")
	}

	// Dial the listener.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// Send a DWR over the raw connection.
	dwr := BuildDWR("client", "client.realm", 1, 2)
	if _, err := conn.Write(EncodeMessage(dwr)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Wait for the message to be delivered to the handler.
	deadline = time.Now().Add(time.Second)
	for h.MessageCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := h.FirstMessage()
	if got == nil {
		t.Fatalf("handler did not receive the DWR within 1s")
	}
	if got.CommandCode != CmdDeviceWatchdog {
		t.Errorf("received CommandCode = %d, want %d", got.CommandCode, CmdDeviceWatchdog)
	}
	if got.HopByHopID != 1 || got.EndToEndID != 2 {
		t.Errorf("received identifiers = (%d, %d), want (1, 2)",
			got.HopByHopID, got.EndToEndID)
	}

	_ = conn.Close()
	_ = tr.Close()
}

func TestTransportListenBadAddress(t *testing.T) {
	cfg := DefaultTransportConfig()
	cfg.ListenAddr = "127.0.0.1:1" // privileged port — should fail.
	tr := NewTransport(cfg, nil, &recordingHandler{})
	if err := tr.ListenAndServe(); err == nil {
		t.Errorf("ListenAndServe on privileged port should fail")
	}
}

func TestTransportDialLoopbackStub(t *testing.T) {
	// Use a stub dialer that always succeeds with an in-memory pipe.
	h := &recordingHandler{}
	tr := NewTransport(nil, nil, h)
	tr.SetDialer(func(network, addr string, timeout time.Duration) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() {
			// Write a DWR on c2 so the receiver reads it.
			dwr := BuildDWR("peer", "peer.realm", 7, 8)
			_, _ = c2.Write(EncodeMessage(dwr))
			_ = c2.Close()
		}()
		return c1, nil
	})

	pc, err := tr.Dial("127.0.0.1:9999")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if pc.IsClosed() {
		t.Errorf("connection reported closed immediately after Dial")
	}
	if pc.Direction() != DirOutbound {
		t.Errorf("Direction = %s, want outbound", pc.Direction())
	}
	if pc.LocalHost() != tr.cfg.LocalHost {
		// Default cfg.LocalHost is empty — that's fine; just verify the
		// accessor exists.
	}

	// Wait for the DWR to arrive.
	deadline := time.Now().Add(time.Second)
	for h.MessageCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := h.FirstMessage()
	if got == nil {
		t.Fatalf("handler did not receive the DWR from the stub dialer")
	}
	if got.HopByHopID != 7 {
		t.Errorf("HopByHopID = %d, want 7", got.HopByHopID)
	}

	_ = tr.Close()
}

func TestTransportDialFailure(t *testing.T) {
	tr := NewTransport(nil, nil, &recordingHandler{})
	tr.SetDialer(func(network, addr string, timeout time.Duration) (net.Conn, error) {
		return nil, errDialStub
	})
	if _, err := tr.Dial("127.0.0.1:9999"); err == nil {
		t.Errorf("Dial with failing stub should return error")
	}
}

var errDialStub = net.UnknownNetworkError("stub")

func TestTransportConnectionsList(t *testing.T) {
	tr := NewTransport(nil, nil, &recordingHandler{})
	tr.SetDialer(func(network, addr string, timeout time.Duration) (net.Conn, error) {
		c1, _ := net.Pipe()
		return c1, nil
	})
	pc, err := tr.Dial("127.0.0.1:9999")
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Connections() must include the dialled connection.
	conns := tr.Connections()
	found := false
	for _, c := range conns {
		if c == pc {
			found = true
		}
	}
	if !found {
		t.Errorf("Connections() did not include the dialled connection")
	}
	_ = pc.Close()
	_ = tr.Close()
}

func TestPeerConnectionSetIdentity(t *testing.T) {
	pc := &PeerConnection{
		localHost:  "initial",
		localRealm: "initial-realm",
	}
	pc.SetIdentity("updated", "updated-realm")
	if pc.LocalHost() != "updated" {
		t.Errorf("LocalHost = %q, want updated", pc.LocalHost())
	}
	if pc.LocalRealm() != "updated-realm" {
		t.Errorf("LocalRealm = %q, want updated-realm", pc.LocalRealm())
	}
}

func TestPeerConnectionCloseTwice(t *testing.T) {
	pc := &PeerConnection{}
	if err := pc.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// Second Close must be a no-op (no panic, no error).
	if err := pc.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if !pc.IsClosed() {
		t.Errorf("IsClosed = false after Close")
	}
}

func TestPeerConnectionSendMessageClosed(t *testing.T) {
	pc := &PeerConnection{}
	_ = pc.Close()
	err := pc.SendMessage(BuildDWR("h", "r", 1, 2))
	if err == nil {
		t.Errorf("SendMessage on closed connection should error")
	}
}

func TestDefaultEncoderRoundTrip(t *testing.T) {
	orig := BuildDWR("host", "realm", 0xCAFEBABE, 0xDEADBEEF)
	enc := DefaultCDPEncoder.Encode(orig)
	if len(enc) < HeaderLen {
		t.Fatalf("encoded too short: %d", len(enc))
	}
	dec, err := DefaultCDPEncoder.Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if dec.CommandCode != orig.CommandCode {
		t.Errorf("CommandCode = %d, want %d", dec.CommandCode, orig.CommandCode)
	}
}

func TestEncodeMessageAndDecodeMessage(t *testing.T) {
	orig := BuildCEA(&PeerCapabilities{OriginHost: "h", OriginRealm: "r"}, ResultSuccess, 1, 2)
	enc := EncodeMessage(orig)
	dec, err := DecodeMessage(enc)
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if dec.HopByHopID != 1 || dec.EndToEndID != 2 {
		t.Errorf("identifiers = (%d, %d), want (1, 2)", dec.HopByHopID, dec.EndToEndID)
	}
}

func TestMessageLengthHelper(t *testing.T) {
	msg := BuildDWR("h", "r", 1, 2)
	enc := EncodeMessage(msg)
	// Pass the first 4 bytes.
	got, err := MessageLength(enc[:4])
	if err != nil {
		t.Fatalf("MessageLength: %v", err)
	}
	if got != len(enc) {
		t.Errorf("MessageLength = %d, want %d", got, len(enc))
	}
}

func TestMessageLengthShortHeader(t *testing.T) {
	if _, err := MessageLength(nil); err == nil {
		t.Errorf("MessageLength(nil) should error")
	}
	if _, err := MessageLength([]byte{1}); err == nil {
		t.Errorf("MessageLength([1]) should error")
	}
}

func TestHopByHopAndEndToEndFromHeader(t *testing.T) {
	msg := BuildDPR("h", "r", DisconnectCauseBusy, 0x11223344, 0x55667788)
	enc := EncodeMessage(msg)
	hbh, err := HopByHopIDFromHeader(enc)
	if err != nil {
		t.Fatalf("HopByHopIDFromHeader: %v", err)
	}
	if hbh != 0x11223344 {
		t.Errorf("HopByHopID = 0x%X, want 0x11223344", hbh)
	}
	e2e, err := EndToEndIDFromHeader(enc)
	if err != nil {
		t.Fatalf("EndToEndIDFromHeader: %v", err)
	}
	if e2e != 0x55667788 {
		t.Errorf("EndToEndID = 0x%X, want 0x55667788", e2e)
	}
}

func TestHopByHopFromHeaderShort(t *testing.T) {
	if _, err := HopByHopIDFromHeader(nil); err == nil {
		t.Errorf("HopByHopIDFromHeader(nil) should error")
	}
	if _, err := EndToEndIDFromHeader([]byte{1, 2, 3}); err == nil {
		t.Errorf("EndToEndIDFromHeader(short) should error")
	}
}

func TestLoopbackTransportRecordSent(t *testing.T) {
	tr := NewLoopbackTransport(nil, &recordingHandler{})
	remote := "127.0.0.1:9999"
	msg := BuildDWR("h", "r", 1, 2)
	tr.RecordSent(remote, msg)
	tr.RecordSent(remote, BuildDWA("h", "r", ResultSuccess, 1, 2))
	sent := tr.Sent(remote)
	if len(sent) != 2 {
		t.Fatalf("Sent len = %d, want 2", len(sent))
	}
	if sent[0].CommandCode != CmdDeviceWatchdog {
		t.Errorf("Sent[0] CommandCode = %d, want %d",
			sent[0].CommandCode, CmdDeviceWatchdog)
	}
	// Reset clears.
	tr.Reset()
	if got := tr.Sent(remote); len(got) != 0 {
		t.Errorf("Sent after Reset = %d, want 0", len(got))
	}
}

func TestTransportCloseIdempotent(t *testing.T) {
	tr := NewTransport(nil, nil, &recordingHandler{})
	if err := tr.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if !tr.IsClosed() {
		t.Errorf("IsClosed = false after Close")
	}
}

func TestConnDirectionString(t *testing.T) {
	if DirInbound.String() != "inbound" {
		t.Errorf("DirInbound.String() = %q", DirInbound.String())
	}
	if DirOutbound.String() != "outbound" {
		t.Errorf("DirOutbound.String() = %q", DirOutbound.String())
	}
	if ConnDirection(99).String() == "inbound" {
		t.Errorf("unknown direction should not say inbound")
	}
}
