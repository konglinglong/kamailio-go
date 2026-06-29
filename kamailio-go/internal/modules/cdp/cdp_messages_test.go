// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the Diameter message builders (CER/CEA, DWR/DWA, DPR/DPA).
 */

package cdp

import (
	"net"
	"testing"
)

func TestBuildCERMinimal(t *testing.T) {
	caps := &PeerCapabilities{
		OriginHost:  "host.example.com",
		OriginRealm: "example.com",
		VendorID:    10415,
	}
	msg := BuildCER(caps, 1, 2)
	if msg.CommandCode != CmdCapabilitiesExchange {
		t.Errorf("CommandCode = %d, want %d", msg.CommandCode, CmdCapabilitiesExchange)
	}
	if msg.Flags&CmdFlagRequest == 0 {
		t.Errorf("Flags = 0x%02X, want Request bit set", msg.Flags)
	}
	if msg.ApplicationID != AppDiameterCommonMessages {
		t.Errorf("ApplicationID = %d, want %d", msg.ApplicationID, AppDiameterCommonMessages)
	}
	if msg.HopByHopID != 1 || msg.EndToEndID != 2 {
		t.Errorf("identifiers = (%d, %d), want (1, 2)", msg.HopByHopID, msg.EndToEndID)
	}
	// At least Origin-Host, Origin-Realm, Vendor-Id must be present.
	codes := avpCodes(msg)
	if !contains(codes, AVPCodeOriginHost) {
		t.Errorf("CER missing Origin-Host AVP")
	}
	if !contains(codes, AVPCodeOriginRealm) {
		t.Errorf("CER missing Origin-Realm AVP")
	}
	if !contains(codes, AVPCodeVendorID) {
		t.Errorf("CER missing Vendor-Id AVP")
	}
}

func TestBuildCERFull(t *testing.T) {
	caps := &PeerCapabilities{
		OriginHost:       "host.example.com",
		OriginRealm:      "example.com",
		HostIPAddresses:  []net.IP{net.IPv4(10, 0, 0, 1), net.ParseIP("2001:db8::1")},
		VendorID:         10415,
		ProductName:      "kamailio-go",
		OriginStateID:    0xCAFEBABE,
		SupportedVendors: []uint32{10415, 13019},
		AuthApplications: []uint32{AppDiameterSIP},
		AcctApplications: []uint32{AppDiameterBaseAccounting},
		VendorSpecificApps: []VendorSpecificApp{
			{VendorID: 10415, AuthApplicationID: App3GPPCx},
			{VendorID: 10415, AcctApplicationID: 0x10000},
		},
		FirmwareRevision: 5000,
	}
	msg := BuildCER(caps, 0, 0)
	codes := avpCodes(msg)
	want := []uint32{
		AVPCodeOriginHost, AVPCodeOriginRealm, AVPCodeHostIPAddress,
		AVPCodeHostIPAddress, AVPCodeVendorID, AVPCodeProductName,
		AVPCodeOriginStateID, AVPCodeSupportedVendorID, AVPCodeSupportedVendorID,
		AVPCodeAuthApplicationID, AVPCodeAcctApplicationID,
		AVPCodeVendorSpecificApplicationID, AVPCodeVendorSpecificApplicationID,
		AVPCodeFirmwareRevision,
	}
	for _, c := range want {
		if !contains(codes, c) {
			t.Errorf("CER missing AVP code %d", c)
		}
	}
}

func TestBuildCEAResultCode(t *testing.T) {
	msg := BuildCEA(&PeerCapabilities{OriginHost: "h", OriginRealm: "r"}, ResultSuccess, 1, 2)
	if msg.Flags&CmdFlagRequest != 0 {
		t.Errorf("CEA Flags = 0x%02X, want Request bit clear", msg.Flags)
	}
	rc, ok := ResultCode(msg)
	if !ok {
		t.Fatalf("CEA missing Result-Code AVP")
	}
	if rc != ResultSuccess {
		t.Errorf("CEA Result-Code = %d, want %d", rc, ResultSuccess)
	}
}

func TestParseCapabilitiesRoundTrip(t *testing.T) {
	orig := &PeerCapabilities{
		OriginHost:       "host.example.com",
		OriginRealm:      "example.com",
		HostIPAddresses:  []net.IP{net.IPv4(10, 0, 0, 1), net.ParseIP("2001:db8::1")},
		VendorID:         10415,
		ProductName:      "kamailio-go",
		OriginStateID:    0xCAFEBABE,
		SupportedVendors: []uint32{10415, 13019},
		AuthApplications: []uint32{AppDiameterSIP},
		AcctApplications: []uint32{AppDiameterBaseAccounting},
		VendorSpecificApps: []VendorSpecificApp{
			{VendorID: 10415, AuthApplicationID: App3GPPCx},
		},
		FirmwareRevision: 5000,
	}
	msg := BuildCER(orig, 1, 2)
	caps, err := ParseCapabilities(msg)
	if err != nil {
		t.Fatalf("ParseCapabilities: %v", err)
	}
	if caps.OriginHost != orig.OriginHost {
		t.Errorf("OriginHost = %q, want %q", caps.OriginHost, orig.OriginHost)
	}
	if caps.OriginRealm != orig.OriginRealm {
		t.Errorf("OriginRealm = %q, want %q", caps.OriginRealm, orig.OriginRealm)
	}
	if caps.VendorID != orig.VendorID {
		t.Errorf("VendorID = %d, want %d", caps.VendorID, orig.VendorID)
	}
	if caps.ProductName != orig.ProductName {
		t.Errorf("ProductName = %q, want %q", caps.ProductName, orig.ProductName)
	}
	if caps.OriginStateID != orig.OriginStateID {
		t.Errorf("OriginStateID = 0x%X, want 0x%X", caps.OriginStateID, orig.OriginStateID)
	}
	if len(caps.HostIPAddresses) != 2 {
		t.Fatalf("HostIPAddresses len = %d, want 2", len(caps.HostIPAddresses))
	}
	if !caps.HostIPAddresses[0].Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("HostIPAddresses[0] = %s, want 10.0.0.1", caps.HostIPAddresses[0])
	}
	if !caps.HostIPAddresses[1].Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("HostIPAddresses[1] = %s, want 2001:db8::1", caps.HostIPAddresses[1])
	}
	if len(caps.SupportedVendors) != 2 || caps.SupportedVendors[0] != 10415 {
		t.Errorf("SupportedVendors = %v, want [10415 13019]", caps.SupportedVendors)
	}
	if len(caps.AuthApplications) != 1 || caps.AuthApplications[0] != AppDiameterSIP {
		t.Errorf("AuthApplications = %v", caps.AuthApplications)
	}
	if len(caps.AcctApplications) != 1 || caps.AcctApplications[0] != AppDiameterBaseAccounting {
		t.Errorf("AcctApplications = %v", caps.AcctApplications)
	}
	if len(caps.VendorSpecificApps) != 1 {
		t.Fatalf("VendorSpecificApps len = %d, want 1", len(caps.VendorSpecificApps))
	}
	vsa := caps.VendorSpecificApps[0]
	if vsa.VendorID != 10415 || vsa.AuthApplicationID != App3GPPCx {
		t.Errorf("VendorSpecificApp = %+v", vsa)
	}
	if caps.FirmwareRevision != 5000 {
		t.Errorf("FirmwareRevision = %d, want 5000", caps.FirmwareRevision)
	}
}

func TestParseCapabilitiesErrors(t *testing.T) {
	if _, err := ParseCapabilities(nil); err == nil {
		t.Errorf("ParseCapabilities(nil) should error")
	}
	// Wrong command code.
	msg := BuildDWR("h", "r", 1, 2)
	if _, err := ParseCapabilities(msg); err == nil {
		t.Errorf("ParseCapabilities on DWR should error")
	}
	// Missing Origin-Host.
	bad := &DiameterMessage{
		CommandCode:   CmdCapabilitiesExchange,
		ApplicationID: AppDiameterCommonMessages,
		AVPs: []DiameterAVP{
			{Code: AVPCodeOriginRealm, Flags: AVPFlagMandatory, Value: []byte("r")},
		},
	}
	if _, err := ParseCapabilities(bad); err == nil {
		t.Errorf("ParseCapabilities without Origin-Host should error")
	}
	// Missing Origin-Realm.
	bad2 := &DiameterMessage{
		CommandCode:   CmdCapabilitiesExchange,
		ApplicationID: AppDiameterCommonMessages,
		AVPs: []DiameterAVP{
			{Code: AVPCodeOriginHost, Flags: AVPFlagMandatory, Value: []byte("h")},
		},
	}
	if _, err := ParseCapabilities(bad2); err == nil {
		t.Errorf("ParseCapabilities without Origin-Realm should error")
	}
}

func TestBuildDWR(t *testing.T) {
	msg := BuildDWR("host.example.com", "example.com", 1, 2)
	if msg.CommandCode != CmdDeviceWatchdog {
		t.Errorf("CommandCode = %d, want %d", msg.CommandCode, CmdDeviceWatchdog)
	}
	if msg.Flags&CmdFlagRequest == 0 {
		t.Errorf("DWR should have Request flag set")
	}
	if h, ok := OriginHost(msg); !ok || h != "host.example.com" {
		t.Errorf("DWR Origin-Host = %q, ok=%v", h, ok)
	}
}

func TestBuildDWAResultCode(t *testing.T) {
	msg := BuildDWA("host.example.com", "example.com", ResultSuccess, 1, 2)
	if msg.Flags&CmdFlagRequest != 0 {
		t.Errorf("DWA should have Request flag clear")
	}
	rc, ok := ResultCode(msg)
	if !ok || rc != ResultSuccess {
		t.Errorf("DWA Result-Code = (%d, %v), want (%d, true)", rc, ok, ResultSuccess)
	}
}

func TestBuildDPR(t *testing.T) {
	msg := BuildDPR("h", "r", DisconnectCauseRebooting, 1, 2)
	if msg.CommandCode != CmdDisconnectPeer {
		t.Errorf("DPR CommandCode = %d, want %d", msg.CommandCode, CmdDisconnectPeer)
	}
	cause, ok := DisconnectCause(msg)
	if !ok || cause != DisconnectCauseRebooting {
		t.Errorf("DPR Disconnect-Cause = (%d, %v), want (%d, true)",
			cause, ok, DisconnectCauseRebooting)
	}
}

func TestBuildDPA(t *testing.T) {
	msg := BuildDPA("h", "r", ResultSuccess, 1, 2)
	if msg.CommandCode != CmdDisconnectPeer {
		t.Errorf("DPA CommandCode = %d, want %d", msg.CommandCode, CmdDisconnectPeer)
	}
	rc, ok := ResultCode(msg)
	if !ok || rc != ResultSuccess {
		t.Errorf("DPA Result-Code = (%d, %v), want (%d, true)", rc, ok, ResultSuccess)
	}
}

func TestEncodeDecodeCER(t *testing.T) {
	caps := &PeerCapabilities{
		OriginHost:  "host.example.com",
		OriginRealm: "example.com",
		VendorID:    10415,
		AuthApplications: []uint32{AppDiameterSIP},
	}
	msg := BuildCER(caps, 0x11223344, 0x55667788)
	enc := DefaultCDPEncoder.Encode(msg)
	if len(enc) < HeaderLen {
		t.Fatalf("encoded buffer too short: %d", len(enc))
	}
	dec, err := DefaultCDPEncoder.Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if dec.CommandCode != CmdCapabilitiesExchange {
		t.Errorf("decoded CommandCode = %d", dec.CommandCode)
	}
	if dec.HopByHopID != msg.HopByHopID || dec.EndToEndID != msg.EndToEndID {
		t.Errorf("decoded identifiers mismatch")
	}
}

func TestIPConversionRoundTrip(t *testing.T) {
	cases := []net.IP{
		net.IPv4(10, 0, 0, 1),
		net.IPv4(255, 255, 255, 255),
		net.ParseIP("2001:db8::1"),
		net.ParseIP("::1"),
	}
	for _, ip := range cases {
		b := ipToBytes(ip)
		got := bytesToIP(b)
		if !got.Equal(ip) {
			t.Errorf("ipToBytes/bytesToIP round-trip failed: %s -> %s", ip, got)
		}
	}
}

func TestBytesToIPMalformed(t *testing.T) {
	if got := bytesToIP(nil); got != nil {
		t.Errorf("bytesToIP(nil) = %v, want nil", got)
	}
	if got := bytesToIP([]byte{0}); got != nil {
		t.Errorf("bytesToIP([0]) = %v, want nil", got)
	}
	if got := bytesToIP([]byte{0, 0, 0}); got != nil {
		// AF=0, no further bytes — unknown family, should return nil.
		t.Errorf("bytesToIP([0 0 0]) = %v, want nil", got)
	}
	if got := bytesToIP([]byte{0, 1, 1, 2, 3}); got != nil {
		// AF=1 (IPv4) but only 3 address bytes — should be nil.
		t.Errorf("bytesToIP(short IPv4) = %v, want nil", got)
	}
}

func TestEncodeUint32RoundTrip(t *testing.T) {
	for _, v := range []uint32{0, 1, 0x100, 0x10000, 0xFFFFFFFF, 0xCAFEBABE} {
		b := encodeUint32(v)
		if got := decodeUint32(b); got != v {
			t.Errorf("encode/decode uint32 %d -> %d", v, got)
		}
	}
}

func TestDecodeUint32Short(t *testing.T) {
	if got := decodeUint32(nil); got != 0 {
		t.Errorf("decodeUint32(nil) = %d, want 0", got)
	}
	if got := decodeUint32([]byte{1, 2, 3}); got != 0 {
		t.Errorf("decodeUint32(short) = %d, want 0", got)
	}
}

func TestOriginHostAndRealmAbsent(t *testing.T) {
	msg := &DiameterMessage{CommandCode: CmdDeviceWatchdog}
	if _, ok := OriginHost(msg); ok {
		t.Errorf("OriginHost() on empty message should return ok=false")
	}
	if _, ok := OriginRealm(msg); ok {
		t.Errorf("OriginRealm() on empty message should return ok=false")
	}
	if _, ok := OriginHost(nil); ok {
		t.Errorf("OriginHost(nil) should return ok=false")
	}
}

func TestDisconnectCauseAbsent(t *testing.T) {
	if _, ok := DisconnectCause(nil); ok {
		t.Errorf("DisconnectCause(nil) should return ok=false")
	}
	if _, ok := DisconnectCause(&DiameterMessage{}); ok {
		t.Errorf("DisconnectCause(empty) should return ok=false")
	}
}

func TestResultCodeAbsent(t *testing.T) {
	if _, ok := ResultCode(nil); ok {
		t.Errorf("ResultCode(nil) should return ok=false")
	}
	if _, ok := ResultCode(&DiameterMessage{}); ok {
		t.Errorf("ResultCode(empty) should return ok=false")
	}
}

func TestPeerCapabilitiesString(t *testing.T) {
	caps := PeerCapabilities{
		OriginHost:  "host",
		OriginRealm: "realm",
		VendorID:    10415,
		ProductName: "kamailio-go",
		AuthApplications: []uint32{AppDiameterSIP},
		AcctApplications: []uint32{AppDiameterBaseAccounting},
		VendorSpecificApps: []VendorSpecificApp{{VendorID: 10415}},
	}
	s := caps.String()
	if s == "" {
		t.Errorf("String() returned empty")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func avpCodes(msg *DiameterMessage) []uint32 {
	out := make([]uint32, 0, len(msg.AVPs))
	for i := range msg.AVPs {
		out = append(out, msg.AVPs[i].Code)
	}
	return out
}

func contains(s []uint32, v uint32) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
