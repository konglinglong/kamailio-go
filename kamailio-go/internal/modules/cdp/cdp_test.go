// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the CDP (C Diameter Peer) module.
 */

package cdp

import (
	"bytes"
	"sync"
	"testing"
)

func TestAddPeerAndPeers(t *testing.T) {
	m := NewCDPModule()
	n := m.AddPeer(&DiameterPeer{Host: "h1", Realm: "r1", IP: "10.0.0.1", Port: 3868, Connected: true})
	if n != 1 {
		t.Errorf("AddPeer returned %d, want 1", n)
	}
	m.AddPeer(&DiameterPeer{Host: "h2", Realm: "r2", IP: "10.0.0.2", Port: 3868, Connected: false})
	if got := len(m.Peers()); got != 2 {
		t.Errorf("Peers len = %d, want 2", got)
	}
	if m.AddPeer(nil) != -1 {
		t.Errorf("AddPeer(nil) should return -1")
	}
	if m.AddPeer(&DiameterPeer{Host: "", IP: "10.0.0.3"}) != -1 {
		t.Errorf("AddPeer with empty host should return -1")
	}
	// Re-adding the same host replaces the entry.
	m.AddPeer(&DiameterPeer{Host: "h1", Realm: "r1b", IP: "10.0.0.11", Port: 3868, Connected: true})
	if got := len(m.Peers()); got != 2 {
		t.Errorf("Peers len after re-add = %d, want 2", got)
	}
}

func TestRemovePeer(t *testing.T) {
	m := NewCDPModule()
	m.AddPeer(&DiameterPeer{Host: "h1", Connected: true})
	if !m.RemovePeer("h1") {
		t.Errorf("RemovePeer returned false for existing peer")
	}
	if len(m.Peers()) != 0 {
		t.Errorf("Peers len after remove = %d, want 0", len(m.Peers()))
	}
	if m.RemovePeer("h1") {
		t.Errorf("RemovePeer returned true for non-existent peer")
	}
}

func TestIsConnected(t *testing.T) {
	m := NewCDPModule()
	m.AddPeer(&DiameterPeer{Host: "up", Connected: true})
	m.AddPeer(&DiameterPeer{Host: "down", Connected: false})
	if !m.IsConnected("up") {
		t.Errorf("IsConnected(up) = false, want true")
	}
	if m.IsConnected("down") {
		t.Errorf("IsConnected(down) = true, want false")
	}
	if m.IsConnected("missing") {
		t.Errorf("IsConnected(missing) = true, want false")
	}
}

func TestSend(t *testing.T) {
	m := NewCDPModule()
	m.AddPeer(&DiameterPeer{Host: "h1", Connected: true})
	req := &DiameterMessage{
		Flags:         CmdFlagRequest,
		CommandCode:   318, // Device-Watchdog
		ApplicationID: 0,
		HopByHopID:    1,
		EndToEndID:    2,
		AVPs:          []DiameterAVP{{Code: 1, Flags: AVPFlagMandatory, Value: []byte("h1")}},
	}
	resp, err := m.Send("h1", req)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if resp.Flags&CmdFlagRequest != 0 {
		t.Errorf("response should clear request flag")
	}
	if resp.HopByHopID != req.HopByHopID || resp.EndToEndID != req.EndToEndID {
		t.Errorf("response identifiers mismatch")
	}
	if len(resp.AVPs) != 1 || !bytes.Equal(resp.AVPs[0].Value, []byte("h1")) {
		t.Errorf("response AVPs mismatch: %+v", resp.AVPs)
	}
	// Unknown peer.
	if _, err := m.Send("nope", req); err == nil {
		t.Errorf("Send to unknown peer should error")
	}
	// Disconnected peer.
	m.AddPeer(&DiameterPeer{Host: "down", Connected: false})
	if _, err := m.Send("down", req); err == nil {
		t.Errorf("Send to disconnected peer should error")
	}
	// Nil message.
	if _, err := m.Send("h1", nil); err == nil {
		t.Errorf("Send(nil) should error")
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := NewCDPModule()
	orig := &DiameterMessage{
		Version:       Version,
		Flags:         CmdFlagRequest | CmdFlagProxy,
		CommandCode:   318,
		ApplicationID: 16777216,
		HopByHopID:    0x11223344,
		EndToEndID:    0x55667788,
		AVPs: []DiameterAVP{
			{Code: 1, Flags: AVPFlagMandatory, Value: []byte("host.example.com")},
			{Code: 2, Flags: AVPFlagMandatory, Value: []byte("realm.example.com")},
			{Code: 258, Flags: AVPFlagMandatory | AVPFlagVendor, VendorID: 10415, Value: []byte{0, 0, 0, 1}},
			// An AVP whose value length is not a multiple of 4 (padding).
			{Code: 263, Flags: AVPFlagMandatory, Value: []byte("session-id-123")},
		},
	}
	enc := m.Encode(orig)
	if len(enc) < HeaderLen {
		t.Fatalf("encoded buffer too short: %d", len(enc))
	}
	// Message length field must match the buffer length.
	if got := int(getUint24(enc[1:4])); got != len(enc) {
		t.Errorf("encoded message length = %d, want %d", got, len(enc))
	}
	dec, err := m.Decode(enc)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if dec.Version != orig.Version {
		t.Errorf("Version = %d, want %d", dec.Version, orig.Version)
	}
	if dec.Flags != orig.Flags {
		t.Errorf("Flags = %d, want %d", dec.Flags, orig.Flags)
	}
	if dec.CommandCode != orig.CommandCode {
		t.Errorf("CommandCode = %d, want %d", dec.CommandCode, orig.CommandCode)
	}
	if dec.ApplicationID != orig.ApplicationID {
		t.Errorf("ApplicationID = %d, want %d", dec.ApplicationID, orig.ApplicationID)
	}
	if dec.HopByHopID != orig.HopByHopID || dec.EndToEndID != orig.EndToEndID {
		t.Errorf("identifiers mismatch: h2h=%d e2e=%d", dec.HopByHopID, dec.EndToEndID)
	}
	if len(dec.AVPs) != len(orig.AVPs) {
		t.Fatalf("AVP count = %d, want %d", len(dec.AVPs), len(orig.AVPs))
	}
	for i, a := range orig.AVPs {
		got := dec.AVPs[i]
		if got.Code != a.Code {
			t.Errorf("AVP %d Code = %d, want %d", i, got.Code, a.Code)
		}
		if got.Flags != a.Flags {
			t.Errorf("AVP %d Flags = %d, want %d", i, got.Flags, a.Flags)
		}
		if got.Flags&AVPFlagVendor != 0 && got.VendorID != a.VendorID {
			t.Errorf("AVP %d VendorID = %d, want %d", i, got.VendorID, a.VendorID)
		}
		if !bytes.Equal(got.Value, a.Value) {
			t.Errorf("AVP %d Value = %q, want %q", i, got.Value, a.Value)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	m := NewCDPModule()
	if _, err := m.Decode([]byte{1, 2, 3}); err == nil {
		t.Errorf("Decode of short buffer should error")
	}
	// Bad version.
	bad := m.Encode(&DiameterMessage{CommandCode: 1})
	bad[0] = 9
	if _, err := m.Decode(bad); err == nil {
		t.Errorf("Decode with bad version should error")
	}
	// Inconsistent length field: set it below the minimum header length.
	bad2 := m.Encode(&DiameterMessage{CommandCode: 1, AVPs: []DiameterAVP{{Code: 1, Value: []byte("x")}}})
	bad2[3] = 5 // length field too small (< HeaderLen)
	if _, err := m.Decode(bad2); err == nil {
		t.Errorf("Decode with bad length should error")
	}
}

func TestEncodeNil(t *testing.T) {
	m := NewCDPModule()
	if enc := m.Encode(nil); enc != nil {
		t.Errorf("Encode(nil) should return nil, got %v", enc)
	}
}

func TestNextHopByHop(t *testing.T) {
	m := NewCDPModule()
	a := m.NextHopByHop()
	b := m.NextHopByHop()
	if a == 0 {
		t.Errorf("first NextHopByHop = 0, want non-zero")
	}
	if b != a+1 {
		t.Errorf("NextHopByHop not incrementing: %d then %d", a, b)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewCDPModule()
	m.AddPeer(&DiameterPeer{Host: "h1", Connected: true})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.AddPeer(&DiameterPeer{Host: "h1", Connected: true})
			m.IsConnected("h1")
			m.Peers()
			m.Send("h1", &DiameterMessage{Flags: CmdFlagRequest, CommandCode: 318})
			enc := m.Encode(&DiameterMessage{CommandCode: 1, AVPs: []DiameterAVP{{Code: 1, Value: []byte("x")}}})
			m.Decode(enc)
			m.NextHopByHop()
		}()
	}
	wg.Wait()
}

func TestGlobalFunctions(t *testing.T) {
	Init()
	c := DefaultCDP()
	if c == nil {
		t.Fatal("expected non-nil default CDP module")
	}
	if len(c.Peers()) != 0 {
		t.Errorf("Peers len = %d, want 0 after Init", len(c.Peers()))
	}
}
