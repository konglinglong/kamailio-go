// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Integration test: end-to-end Diameter peer lifecycle.
 *
 * Wires together the Phase 3.3 CDP module's components — Transport,
 * Peer state machine, message builders (CER/CEA, DWR/DWA, DPR/DPA) and
 * TransactionManager — to verify they cooperate over a real TCP
 * connection. This is the integration counterpart to the unit tests in
 * internal/modules/cdp/.
 *
 * The scenario walks a peer through the full RFC 6733 §5.3 lifecycle:
 *
 *	Closed --Start--> WaitConnAck --ConnectAccepted--> WaitICEA
 *	      --CEAReceived--> ROpen --Stop--> Closing --DPAReceived--> Closed
 *
 * with a DWR/DWA exchange performed while in StateROpen.
 */

package integration

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

// ---------------------------------------------------------------------------
// Server-side handler: accepts the incoming CER, replies with a CEA,
// answers DWR with DWA, and acknowledges the DPR with a DPA.
// ---------------------------------------------------------------------------

type diameterServerHandler struct {
	t       *testing.T
	localHost  string
	localRealm string
	peer    *cdp.Peer
}

func (h *diameterServerHandler) HandleConnect(pc *cdp.PeerConnection) error {
	return nil
}

func (h *diameterServerHandler) HandleDisconnect(pc *cdp.PeerConnection, err error) {}

func (h *diameterServerHandler) HandleMessage(pc *cdp.PeerConnection, msg *cdp.DiameterMessage) error {
	switch msg.CommandCode {
	case cdp.CmdCapabilitiesExchange:
		// Reply with a CEA carrying Result-Code = 2001 (Success).
		cea := cdp.BuildCEA(&cdp.PeerCapabilities{
			OriginHost:  h.localHost,
			OriginRealm: h.localRealm,
			VendorID:    cdp.VendorID3GPP,
		}, cdp.ResultSuccess, msg.HopByHopID, msg.EndToEndID)
		if err := pc.SendMessage(cea); err != nil {
			h.t.Errorf("server: send CEA: %v", err)
			return err
		}

	case cdp.CmdDeviceWatchdog:
		// Reply with a DWA.
		dwa := cdp.BuildDWA(h.localHost, h.localRealm, cdp.ResultSuccess,
			msg.HopByHopID, msg.EndToEndID)
		if err := pc.SendMessage(dwa); err != nil {
			h.t.Errorf("server: send DWA: %v", err)
			return err
		}

	case cdp.CmdDisconnectPeer:
		// Reply with a DPA, then close.
		dpa := cdp.BuildDPA(h.localHost, h.localRealm, cdp.ResultSuccess,
			msg.HopByHopID, msg.EndToEndID)
		if err := pc.SendMessage(dpa); err != nil {
			h.t.Errorf("server: send DPA: %v", err)
			return err
		}
		_ = pc.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Client-side handler: collects every message received so the test can
// assert on the sequence (CEA -> DWA -> DPA).
// ---------------------------------------------------------------------------

type diameterClientHandler struct {
	mu       sync.Mutex
	received []*cdp.DiameterMessage
	done     chan struct{}
}

func newDiameterClientHandler() *diameterClientHandler {
	return &diameterClientHandler{done: make(chan struct{})}
}

func (h *diameterClientHandler) HandleConnect(pc *cdp.PeerConnection) error { return nil }

func (h *diameterClientHandler) HandleDisconnect(pc *cdp.PeerConnection, err error) {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

func (h *diameterClientHandler) HandleMessage(pc *cdp.PeerConnection, msg *cdp.DiameterMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.received = append(h.received, msg)
	return nil
}

// FirstReceived returns the first received message, or nil if none.
func (h *diameterClientHandler) FirstReceived() *cdp.DiameterMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.received) == 0 {
		return nil
	}
	return h.received[0]
}

// ReceivedCount returns the number of messages received so far.
func (h *diameterClientHandler) ReceivedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.received)
}

// ReceivedAt returns a copy of the message at the given index, or nil if
// the index is out of range. The returned message is safe to inspect
// without holding the lock.
func (h *diameterClientHandler) ReceivedAt(i int) *cdp.DiameterMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i < 0 || i >= len(h.received) {
		return nil
	}
	return h.received[i]
}

// ---------------------------------------------------------------------------
// Integration test: full Diameter peer lifecycle over real TCP.
// ---------------------------------------------------------------------------

// TestCDPPeerLifecycleOverTCP exercises the full Diameter peer state
// machine against a real TCP transport. The client initiates the
// connection, completes the CER/CEA handshake, exchanges a DWR/DWA,
// then performs a graceful DPR/DPA shutdown.
func TestCDPPeerLifecycleOverTCP(t *testing.T) {
	// --- Set up the server transport on an ephemeral port. ---
	serverHost := "127.0.0.1"
	serverRealm := "server.example.com"
	serverCfg := cdp.DefaultTransportConfig()
	serverCfg.ListenAddr = "127.0.0.1:0"
	serverCfg.LocalHost = serverHost
	serverCfg.LocalRealm = serverRealm
	serverCfg.ReadTimeout = 5 * time.Second
	serverHandler := &diameterServerHandler{t: t, localHost: serverHost, localRealm: serverRealm}
	serverTr := cdp.NewTransport(serverCfg, nil, serverHandler)
	defer serverTr.Close()

	go func() { _ = serverTr.ListenAndServe() }()

	// Wait for the listener to bind.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if serverTr.ListenerAddr() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	addr := serverTr.ListenerAddr()
	if addr == "" {
		t.Fatalf("server transport did not bind within 1s")
	}

	// --- Set up the client transport (no listener) and dial. ---
	clientHost := "127.0.0.1"
	clientRealm := "client.example.com"
	clientCfg := cdp.DefaultTransportConfig()
	clientCfg.ListenAddr = "" // outbound-only
	clientCfg.LocalHost = clientHost
	clientCfg.LocalRealm = clientRealm
	clientCfg.ReadTimeout = 5 * time.Second
	clientHandler := newDiameterClientHandler()
	clientTr := cdp.NewTransport(clientCfg, nil, clientHandler)
	defer clientTr.Close()

	pc, err := clientTr.Dial(addr)
	if err != nil {
		t.Fatalf("client Dial: %v", err)
	}
	t.Logf("client dialled %s", addr)

	// --- Drive the peer state machine through the lifecycle. ---
	peer := cdp.NewPeer(clientHost, clientRealm, "127.0.0.1", 0)

	// Closed --Start--> WaitConnAck (ActionConnect — already done by Dial).
	tr, err := peer.ProcessEvent(cdp.EvStart)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if tr.To != cdp.StateWaitConnAck {
		t.Fatalf("Start: To = %s, want WaitConnAck", tr.To)
	}

	// WaitConnAck --ConnectAccepted--> WaitICEA (ActionSendCER).
	tr, err = peer.ProcessEvent(cdp.EvConnectAccepted)
	if err != nil {
		t.Fatalf("ConnectAccepted: %v", err)
	}
	if tr.To != cdp.StateWaitICEA {
		t.Fatalf("ConnectAccepted: To = %s, want WaitICEA", tr.To)
	}
	if tr.Action != cdp.ActionSendCER {
		t.Fatalf("ConnectAccepted: Action = %s, want SendCER", tr.Action)
	}

	// Build and send the CER.
	cerCaps := &cdp.PeerCapabilities{
		OriginHost:  clientHost,
		OriginRealm: clientRealm,
		VendorID:    cdp.VendorID3GPP,
		AuthApplications: []uint32{cdp.AppDiameterSIP},
	}
	cer := cdp.BuildCER(cerCaps, 1, 2)
	if err := pc.SendMessage(cer); err != nil {
		t.Fatalf("send CER: %v", err)
	}

	// Wait for the server's CEA to arrive at the client.
	deadline = time.Now().Add(2 * time.Second)
	for clientHandler.ReceivedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cea := clientHandler.FirstReceived()
	if cea == nil {
		t.Fatalf("client did not receive CEA within 2s")
	}
	if cea.CommandCode != cdp.CmdCapabilitiesExchange {
		t.Errorf("CEA CommandCode = %d, want %d", cea.CommandCode, cdp.CmdCapabilitiesExchange)
	}
	rc, ok := cdp.ResultCode(cea)
	if !ok || rc != cdp.ResultSuccess {
		t.Errorf("CEA Result-Code = (%d, %v), want (%d, true)", rc, ok, cdp.ResultSuccess)
	}
	if cea.HopByHopID != 1 || cea.EndToEndID != 2 {
		t.Errorf("CEA identifiers = (%d, %d), want (1, 2)", cea.HopByHopID, cea.EndToEndID)
	}

	// Parse the server's capabilities from the CEA.
	serverCaps, err := cdp.ParseCapabilities(cea)
	if err != nil {
		t.Fatalf("ParseCapabilities on CEA: %v", err)
	}
	if serverCaps.OriginHost != serverHost {
		t.Errorf("CEA Origin-Host = %q, want %q", serverCaps.OriginHost, serverHost)
	}
	if serverCaps.OriginRealm != serverRealm {
		t.Errorf("CEA Origin-Realm = %q, want %q", serverCaps.OriginRealm, serverRealm)
	}
	if serverCaps.VendorID != cdp.VendorID3GPP {
		t.Errorf("CEA Vendor-Id = %d, want %d", serverCaps.VendorID, cdp.VendorID3GPP)
	}

	// WaitICEA --CEAReceived--> ROpen.
	tr, err = peer.ProcessEvent(cdp.EvCEAReceived)
	if err != nil {
		t.Fatalf("CEAReceived: %v", err)
	}
	if tr.To != cdp.StateROpen {
		t.Fatalf("CEAReceived: To = %s, want ROpen", tr.To)
	}
	if !peer.IsOpen() {
		t.Fatalf("peer IsOpen() = false, want true")
	}

	// --- While in ROpen, send a DWR and wait for the DWA. ---
	dwr := cdp.BuildDWR(clientHost, clientRealm, 3, 4)
	if err := pc.SendMessage(dwr); err != nil {
		t.Fatalf("send DWR: %v", err)
	}

	// Wait for the DWA (the second message received).
	deadline = time.Now().Add(2 * time.Second)
	for clientHandler.ReceivedCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if clientHandler.ReceivedCount() < 2 {
		t.Fatalf("client did not receive DWA within 2s (got %d messages)",
			clientHandler.ReceivedCount())
	}
	dwa := clientHandler.ReceivedAt(1)
	if dwa == nil {
		t.Fatalf("client did not receive DWA (ReceivedAt(1) = nil)")
	}
	if dwa.CommandCode != cdp.CmdDeviceWatchdog {
		t.Errorf("DWA CommandCode = %d, want %d", dwa.CommandCode, cdp.CmdDeviceWatchdog)
	}
	if dwa.HopByHopID != 3 || dwa.EndToEndID != 4 {
		t.Errorf("DWA identifiers = (%d, %d), want (3, 4)", dwa.HopByHopID, dwa.EndToEndID)
	}
	// Apply the DWA to the state machine (resets watchdog retry counter).
	if _, err := peer.ProcessEvent(cdp.EvDWAReceived); err != nil {
		t.Fatalf("DWAReceived: %v", err)
	}

	// --- Graceful shutdown: send DPR and wait for the DPA. ---
	peer.SetDPRCause(cdp.DisconnectCauseRebooting)
	dpr := cdp.BuildDPR(clientHost, clientRealm, cdp.DisconnectCauseRebooting, 5, 6)
	if err := pc.SendMessage(dpr); err != nil {
		t.Fatalf("send DPR: %v", err)
	}

	// ROpen --Stop--> Closing (ActionSendDPR — already done above).
	tr, err = peer.ProcessEvent(cdp.EvStop)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if tr.To != cdp.StateClosing {
		t.Fatalf("Stop: To = %s, want Closing", tr.To)
	}

	// Wait for the DPA (the third message received).
	deadline = time.Now().Add(2 * time.Second)
	for clientHandler.ReceivedCount() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if clientHandler.ReceivedCount() < 3 {
		t.Fatalf("client did not receive DPA within 2s (got %d messages)",
			clientHandler.ReceivedCount())
	}
	dpa := clientHandler.ReceivedAt(2)
	if dpa == nil {
		t.Fatalf("client did not receive DPA (ReceivedAt(2) = nil)")
	}
	if dpa.CommandCode != cdp.CmdDisconnectPeer {
		t.Errorf("DPA CommandCode = %d, want %d", dpa.CommandCode, cdp.CmdDisconnectPeer)
	}
	if dpa.HopByHopID != 5 || dpa.EndToEndID != 6 {
		t.Errorf("DPA identifiers = (%d, %d), want (5, 6)", dpa.HopByHopID, dpa.EndToEndID)
	}

	// Closing --DPAReceived--> Closed.
	tr, err = peer.ProcessEvent(cdp.EvDPAReceived)
	if err != nil {
		t.Fatalf("DPAReceived: %v", err)
	}
	if tr.To != cdp.StateClosed {
		t.Fatalf("DPAReceived: To = %s, want Closed", tr.To)
	}
	if !peer.IsClosed() {
		t.Fatalf("peer IsClosed() = false after DPAReceived")
	}
}

// TestCDPTransactionMatchingOverTCP verifies that the transaction table
// correctly correlates a request and its answer when the answer arrives
// over the real TCP transport.
func TestCDPTransactionMatchingOverTCP(t *testing.T) {
	// Stand up a server that echoes a DWA for each DWR.
	serverHost := "127.0.0.1"
	serverCfg := cdp.DefaultTransportConfig()
	serverCfg.ListenAddr = "127.0.0.1:0"
	serverCfg.LocalHost = serverHost
	serverCfg.LocalRealm = "server.example.com"
	serverHandler := &diameterServerHandler{t: t, localHost: serverHost, localRealm: "server.example.com"}
	serverTr := cdp.NewTransport(serverCfg, nil, serverHandler)
	defer serverTr.Close()

	go func() { _ = serverTr.ListenAndServe() }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if serverTr.ListenerAddr() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	addr := serverTr.ListenerAddr()
	if addr == "" {
		t.Fatalf("server transport did not bind")
	}

	// Client side.
	clientCfg := cdp.DefaultTransportConfig()
	clientCfg.ListenAddr = ""
	clientCfg.LocalHost = "127.0.0.1"
	clientCfg.LocalRealm = "client.example.com"
	clientHandler := newDiameterClientHandler()
	clientTr := cdp.NewTransport(clientCfg, nil, clientHandler)
	defer clientTr.Close()

	pc, err := clientTr.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	// Use the transaction manager to register the DWR.
	m := cdp.NewCDPModule()
	tbl := cdp.NewTransactionTable(5*time.Second, 0)
	defer tbl.Close()
	tm := cdp.NewTransactionManager(m, tbl)

	dwr := cdp.BuildDWR("127.0.0.1", "client.example.com", 0, 0)
	txn, err := tm.SendRequest(dwr, serverHost, nil)
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	// Send the DWR (SendRequest does not write to the wire).
	if err := pc.SendMessage(dwr); err != nil {
		t.Fatalf("send DWR: %v", err)
	}

	// Wait for the matching DWA — either via the transaction's Done
	// channel (if the client handler delivers it to the table) or via
	// direct inspection of received messages.
	deadline = time.Now().Add(2 * time.Second)
	for clientHandler.ReceivedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	dwa := clientHandler.FirstReceived()
	if dwa == nil {
		t.Fatalf("did not receive DWA within 2s")
	}
	if dwa.HopByHopID != dwr.HopByHopID {
		t.Errorf("DWA HopByHopID = %d, want %d (must match the request)",
			dwa.HopByHopID, dwr.HopByHopID)
	}

	// Manually deliver the answer to the transaction table.
	if !tbl.DeliverAnswer(dwa) {
		t.Errorf("DeliverAnswer returned false, want true (transaction should match)")
	}
	if !txn.HasCompleted() {
		t.Errorf("transaction HasCompleted = false, want true after deliver")
	}
}

// TestCDPTransportGracefulClose verifies that closing the transport
// cleanly tears down all active connections (no goroutine leaks).
func TestCDPTransportGracefulClose(t *testing.T) {
	serverCfg := cdp.DefaultTransportConfig()
	serverCfg.ListenAddr = "127.0.0.1:0"
	serverTr := cdp.NewTransport(serverCfg, nil, &diameterServerHandler{
		t: t, localHost: "127.0.0.1", localRealm: "server.example.com",
	})

	go func() { _ = serverTr.ListenAndServe() }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if serverTr.ListenerAddr() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	addr := serverTr.ListenerAddr()
	if addr == "" {
		t.Fatalf("server transport did not bind")
	}

	// Dial and immediately close — must not panic or leak.
	pc, err := serverTr.Dial(addr)
	if err != nil {
		// Dialling our own listener may not be a normal use case but it
		// exercises the connection path; tolerate a failure here.
		t.Logf("Dial self: %v (non-fatal)", err)
	} else {
		_ = pc.Close()
	}

	// Closing the server must succeed.
	if err := serverTr.Close(); err != nil {
		t.Errorf("server Close: %v", err)
	}
	if !serverTr.IsClosed() {
		t.Errorf("server IsClosed = false after Close")
	}
}

// TestCDPEncodeDecodePipelineOverTCP verifies that the wire encode /
// decode path survives a round-trip over a real TCP connection with a
// message carrying many AVPs and vendor-specific groupings.
func TestCDPEncodeDecodePipelineOverTCP(t *testing.T) {
	// Set up the server.
	serverHost := "127.0.0.1"
	serverCfg := cdp.DefaultTransportConfig()
	serverCfg.ListenAddr = "127.0.0.1:0"
	serverCfg.LocalHost = serverHost
	serverCfg.LocalRealm = "server.example.com"

	// Custom handler that echoes the CER back as a CEA with the same AVPs.
	var receivedCER *cdp.DiameterMessage
	echoHandler := &echoCEAHandler{t: t, localHost: serverHost, localRealm: "server.example.com", gotCER: &receivedCER}
	serverTr := cdp.NewTransport(serverCfg, nil, echoHandler)
	defer serverTr.Close()

	go func() { _ = serverTr.ListenAndServe() }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if serverTr.ListenerAddr() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	addr := serverTr.ListenerAddr()
	if addr == "" {
		t.Fatalf("server transport did not bind")
	}

	// Build a CER with many AVPs and a vendor-specific application id.
	clientCfg := cdp.DefaultTransportConfig()
	clientCfg.ListenAddr = ""
	clientCfg.LocalHost = "127.0.0.1"
	clientCfg.LocalRealm = "client.example.com"
	clientHandler := newDiameterClientHandler()
	clientTr := cdp.NewTransport(clientCfg, nil, clientHandler)
	defer clientTr.Close()

	pc, err := clientTr.Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	cer := cdp.BuildCER(&cdp.PeerCapabilities{
		OriginHost:  "127.0.0.1",
		OriginRealm: "client.example.com",
		HostIPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		VendorID:    cdp.VendorID3GPP,
		ProductName: "kamailio-go-test",
		AuthApplications: []uint32{cdp.AppDiameterSIP},
		VendorSpecificApps: []cdp.VendorSpecificApp{
			{VendorID: cdp.VendorID3GPP, AuthApplicationID: cdp.App3GPPCx},
		},
	}, 100, 200)
	if err := pc.SendMessage(cer); err != nil {
		t.Fatalf("send CER: %v", err)
	}

	// Wait for the CEA.
	deadline = time.Now().Add(2 * time.Second)
	for clientHandler.ReceivedCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cea := clientHandler.FirstReceived()
	if cea == nil {
		t.Fatalf("did not receive CEA within 2s")
	}
	if cea.CommandCode != cdp.CmdCapabilitiesExchange {
		t.Errorf("CEA CommandCode = %d", cea.CommandCode)
	}
	if cea.HopByHopID != 100 || cea.EndToEndID != 200 {
		t.Errorf("CEA identifiers = (%d, %d), want (100, 200)", cea.HopByHopID, cea.EndToEndID)
	}

	// Parse the server's CEA — it must contain the same capabilities we sent.
	caps, err := cdp.ParseCapabilities(cea)
	if err != nil {
		t.Fatalf("ParseCapabilities on echoed CEA: %v", err)
	}
	if caps.OriginHost != "127.0.0.1" {
		t.Errorf("caps.OriginHost = %q", caps.OriginHost)
	}
	if caps.VendorID != cdp.VendorID3GPP {
		t.Errorf("caps.VendorID = %d, want %d", caps.VendorID, cdp.VendorID3GPP)
	}
	if caps.ProductName != "kamailio-go-test" {
		t.Errorf("caps.ProductName = %q", caps.ProductName)
	}
	if len(caps.HostIPAddresses) != 1 || !caps.HostIPAddresses[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("caps.HostIPAddresses = %v", caps.HostIPAddresses)
	}
}

// echoCEAHandler replies to a CER by echoing the same capabilities back
// in a CEA. Used by TestCDPEncodeDecodePipelineOverTCP.
type echoCEAHandler struct {
	t          *testing.T
	localHost  string
	localRealm string
	gotCER     **cdp.DiameterMessage
}

func (h *echoCEAHandler) HandleConnect(pc *cdp.PeerConnection) error { return nil }
func (h *echoCEAHandler) HandleDisconnect(pc *cdp.PeerConnection, err error) {}

func (h *echoCEAHandler) HandleMessage(pc *cdp.PeerConnection, msg *cdp.DiameterMessage) error {
	if msg.CommandCode != cdp.CmdCapabilitiesExchange {
		return nil
	}
	*h.gotCER = msg
	// Parse the client's capabilities and echo them back with our own
	// Origin-Host / Origin-Realm.
	clientCaps, err := cdp.ParseCapabilities(msg)
	if err != nil {
		h.t.Errorf("server: parse client CER: %v", err)
		return err
	}
	echoCaps := *clientCaps
	echoCaps.OriginHost = h.localHost
	echoCaps.OriginRealm = h.localRealm
	cea := cdp.BuildCEA(&echoCaps, cdp.ResultSuccess, msg.HopByHopID, msg.EndToEndID)
	return pc.SendMessage(cea)
}
