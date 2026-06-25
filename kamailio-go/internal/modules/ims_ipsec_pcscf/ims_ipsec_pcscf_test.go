// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the IMS IPsec P-CSCF module (ims_ipsec_pcscf).
 */

package ims_ipsec_pcscf

import (
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

func bytesFromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// makeRegisterWithSecClient builds a REGISTER carrying a Security-Client
// header.
func makeRegisterWithSecClient(secClientBody string) []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776ipsec\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: ipsec-call-1@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Security-Client: " + secClientBody + "\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

// makeRegisterWithoutSecClient builds a REGISTER without Security-Client.
func makeRegisterWithoutSecClient() []byte {
	return []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776noipsec\r\n" +
		"From: Alice <sip:alice@example.com>;tag=ftag1\r\n" +
		"To: Alice <sip:alice@example.com>\r\n" +
		"Call-ID: ipsec-call-2@10.0.0.1\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:alice@10.0.0.1:5060>\r\n" +
		"Expires: 3600\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
}

const secClientBody = "ipsec-3gpp;alg=hmac-sha-1-96;prot=esp;mod=trans;" +
	"ealg=aes-cbc;spi-c=12345;spi-s=12346;port-c=5060;port-s=5061"

const wwwAuthBody = `Digest realm="ims.example.com", nonce="abc123", ` +
	`algorithm=AKAv1-MD5, opaque="xyz", ` +
	`ck="00112233445566778899001122334455", ` +
	`ik="99887766554433221100998877665544"`

const ckHex = "00112233445566778899001122334455"
const ikHex = "99887766554433221100998877665544"

// parseSecParams extracts key=value pairs from a Security-Server/Verify header.
func parseSecParams(header string) map[string]string {
	out := map[string]string{}
	body := strings.TrimSpace(header)
	if idx := strings.IndexByte(body, ';'); idx >= 0 {
		// Keep the mechanism name as the body before the first ';'.
		body = body[idx+1:]
	}
	for _, part := range strings.Split(body, ";") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(kv[0]))
		val := strings.TrimSpace(kv[1])
		out[key] = val
	}
	return out
}

// mockTunnel records CreateTunnel/DestroyTunnel/CleanAll calls.
type mockTunnel struct {
	mu          sync.Mutex
	creates     int
	destroys    int
	cleanAlls   int
	failCreate  bool
}

func (t *mockTunnel) CreateTunnel(string, *IPsecSA) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.creates++
	if t.failCreate {
		return errors.New("mock tunnel create failure")
	}
	return nil
}

func (t *mockTunnel) DestroyTunnel(string, *IPsecSA) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.destroys++
	return nil
}

func (t *mockTunnel) CleanAll() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cleanAlls++
	return nil
}

func (t *mockTunnel) stats() (int, int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.creates, t.destroys, t.cleanAlls
}

// ---------------------------------------------------------------------------
// ParseSecurityClient
// ---------------------------------------------------------------------------

func TestParseSecurityClient_IPsec3gpp(t *testing.T) {
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	hdr := msg.GetHeaderByType(parser.HdrSecurityClient)
	if hdr == nil {
		t.Fatal("expected Security-Client header")
	}
	sa, err := ParseSecurityClient(hdr)
	if err != nil {
		t.Fatalf("ParseSecurityClient error: %v", err)
	}
	if sa.Type != SecurityIPsec {
		t.Errorf("type = %d, want SecurityIPsec", sa.Type)
	}
	if sa.IPsec == nil {
		t.Fatal("expected non-nil IPsec SA")
	}
	if sa.IPsec.SPIUC != 12345 {
		t.Errorf("spi_uc = %d, want 12345", sa.IPsec.SPIUC)
	}
	if sa.IPsec.SPIUS != 12346 {
		t.Errorf("spi_us = %d, want 12346", sa.IPsec.SPIUS)
	}
	if sa.IPsec.PortUC != 5060 {
		t.Errorf("port_uc = %d, want 5060", sa.IPsec.PortUC)
	}
	if sa.IPsec.PortUS != 5061 {
		t.Errorf("port_us = %d, want 5061", sa.IPsec.PortUS)
	}
	if sa.IPsec.RAlg != "hmac-sha-1-96" {
		t.Errorf("r_alg = %q, want hmac-sha-1-96", sa.IPsec.RAlg)
	}
	if sa.IPsec.REAlg != "aes-cbc" {
		t.Errorf("r_ealg = %q, want aes-cbc", sa.IPsec.REAlg)
	}
	if sa.IPsec.Prot != "esp" {
		t.Errorf("prot = %q, want esp", sa.IPsec.Prot)
	}
	if sa.IPsec.Mod != "trans" {
		t.Errorf("mod = %q, want trans", sa.IPsec.Mod)
	}
}

func TestParseSecurityClient_CommaSeparated(t *testing.T) {
	// Parameters separated by commas instead of semicolons.
	body := "ipsec-3gpp,alg=hmac-sha-1-96,prot=esp,mod=trans,spi-c=100,spi-s=101,port-c=5000,port-s=5001"
	msg := mustParseMsg(t, makeRegisterWithSecClient(body))
	hdr := msg.GetHeaderByType(parser.HdrSecurityClient)
	sa, err := ParseSecurityClient(hdr)
	if err != nil {
		t.Fatalf("ParseSecurityClient error: %v", err)
	}
	if sa.IPsec.SPIUC != 100 {
		t.Errorf("spi_uc = %d, want 100", sa.IPsec.SPIUC)
	}
	if sa.IPsec.PortUS != 5001 {
		t.Errorf("port_us = %d, want 5001", sa.IPsec.PortUS)
	}
}

func TestParseSecurityClient_TLS(t *testing.T) {
	body := "tls"
	msg := mustParseMsg(t, makeRegisterWithSecClient(body))
	hdr := msg.GetHeaderByType(parser.HdrSecurityClient)
	sa, err := ParseSecurityClient(hdr)
	if err != nil {
		t.Fatalf("ParseSecurityClient error: %v", err)
	}
	if sa.Type != SecurityTLS {
		t.Errorf("type = %d, want SecurityTLS", sa.Type)
	}
	if sa.IPsec != nil {
		t.Error("expected nil IPsec for TLS")
	}
}

func TestParseSecurityClient_UnsupportedMechanism(t *testing.T) {
	body := "digest"
	msg := mustParseMsg(t, makeRegisterWithSecClient(body))
	hdr := msg.GetHeaderByType(parser.HdrSecurityClient)
	_, err := ParseSecurityClient(hdr)
	if err == nil {
		t.Error("expected error for unsupported mechanism")
	}
}

func TestParseSecurityClient_NilHeader(t *testing.T) {
	_, err := ParseSecurityClient(nil)
	if err == nil {
		t.Error("expected error for nil header")
	}
}

// ---------------------------------------------------------------------------
// BuildSecurityServer / BuildSecurityVerify
// ---------------------------------------------------------------------------

func TestBuildSecurityServer(t *testing.T) {
	sa := &IPsecSA{
		SPIPC:  200, SPIPS: 201,
		PortPC: 5062, PortPS: 5063,
		Prot: "esp", Mod: "trans",
		RAlg: "hmac-sha-1-96", REAlg: "aes-cbc",
	}
	val := BuildSecurityServer(sa).String()
	if val == "" {
		t.Fatal("expected non-empty Security-Server")
	}
	if !strings.HasPrefix(val, "ipsec-3gpp;") {
		t.Errorf("expected ipsec-3gpp prefix, got %q", val)
	}
	params := parseSecParams(val)
	if params["spi-c"] != "200" {
		t.Errorf("spi-c = %q, want 200 (P-CSCF spi_pc)", params["spi-c"])
	}
	if params["spi-s"] != "201" {
		t.Errorf("spi-s = %q, want 201 (P-CSCF spi_ps)", params["spi-s"])
	}
	if params["port-c"] != "5062" {
		t.Errorf("port-c = %q, want 5062 (P-CSCF port_pc)", params["port-c"])
	}
	if params["port-s"] != "5063" {
		t.Errorf("port-s = %q, want 5063 (P-CSCF port_ps)", params["port-s"])
	}
	if params["alg"] != "hmac-sha-1-96" {
		t.Errorf("alg = %q, want hmac-sha-1-96", params["alg"])
	}
	if params["ealg"] != "aes-cbc" {
		t.Errorf("ealg = %q, want aes-cbc", params["ealg"])
	}
}

func TestBuildSecurityServer_NilSA(t *testing.T) {
	if val := BuildSecurityServer(nil); val.String() != "" {
		t.Errorf("expected empty for nil SA, got %q", val.String())
	}
}

func TestBuildSecurityVerify(t *testing.T) {
	sa := &IPsecSA{
		SPIUC:  12345, SPIUS: 12346,
		PortUC: 5060, PortUS: 5061,
		Prot: "esp", Mod: "trans",
		RAlg: "hmac-sha-1-96", REAlg: "aes-cbc",
	}
	val := BuildSecurityVerify(sa).String()
	if val == "" {
		t.Fatal("expected non-empty Security-Verify")
	}
	params := parseSecParams(val)
	if params["spi-c"] != "12345" {
		t.Errorf("spi-c = %q, want 12345 (UE spi_uc)", params["spi-c"])
	}
	if params["spi-s"] != "12346" {
		t.Errorf("spi-s = %q, want 12346 (UE spi_us)", params["spi-s"])
	}
	if params["port-c"] != "5060" {
		t.Errorf("port-c = %q, want 5060 (UE port_uc)", params["port-c"])
	}
}

func TestBuildSecurityVerify_NilSA(t *testing.T) {
	if val := BuildSecurityVerify(nil); val.String() != "" {
		t.Errorf("expected empty for nil SA, got %q", val.String())
	}
}

// ---------------------------------------------------------------------------
// ExtractCKIK
// ---------------------------------------------------------------------------

func TestExtractCKIK_Valid(t *testing.T) {
	ck, ik, err := ExtractCKIK(wwwAuthBody)
	if err != nil {
		t.Fatalf("ExtractCKIK error: %v", err)
	}
	if !bytesEqual(ck, bytesFromHex(ckHex)) {
		t.Errorf("ck mismatch")
	}
	if !bytesEqual(ik, bytesFromHex(ikHex)) {
		t.Errorf("ik mismatch")
	}
}

func TestExtractCKIK_MissingCK(t *testing.T) {
	body := `Digest realm="ims.example.com", nonce="x", ik="99887766554433221100998877665544"`
	_, _, err := ExtractCKIK(body)
	if err == nil {
		t.Error("expected error for missing ck")
	}
}

func TestExtractCKIK_MissingIK(t *testing.T) {
	body := `Digest realm="ims.example.com", nonce="x", ck="00112233445566778899001122334455"`
	_, _, err := ExtractCKIK(body)
	if err == nil {
		t.Error("expected error for missing ik")
	}
}

func TestExtractCKIK_InvalidHex(t *testing.T) {
	body := `Digest ck="ZZZZ", ik="99887766554433221100998877665544"`
	_, _, err := ExtractCKIK(body)
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestExtractCKIK_Empty(t *testing.T) {
	_, _, err := ExtractCKIK("")
	if err == nil {
		t.Error("expected error for empty body")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// SPIGenerator
// ---------------------------------------------------------------------------

func TestSPIGenerator_Acquire(t *testing.T) {
	g := NewSPIGenerator(100, 20, 5063, 5062, 5)
	spiCID, spiSID, cport, sport, err := g.Acquire()
	if err != nil {
		t.Fatalf("Acquire error: %v", err)
	}
	if spiCID != 100 {
		t.Errorf("spi_cid = %d, want 100", spiCID)
	}
	if spiSID != 101 {
		t.Errorf("spi_sid = %d, want 101", spiSID)
	}
	if cport != 5062 {
		t.Errorf("cport = %d, want 5062", cport)
	}
	if sport != 5063 {
		t.Errorf("sport = %d, want 5063", sport)
	}
	if g.InUse() != 1 {
		t.Errorf("InUse = %d, want 1", g.InUse())
	}
}

func TestSPIGenerator_Acquire_Exhaustion(t *testing.T) {
	// spiRange=4 → 2 pairs; portRange=1 → all pairs share the same ports.
	g := NewSPIGenerator(100, 4, 5063, 5062, 1)
	_, _, _, _, err1 := g.Acquire()
	if err1 != nil {
		t.Fatalf("first Acquire error: %v", err1)
	}
	_, _, _, _, err2 := g.Acquire()
	if err2 != nil {
		t.Fatalf("second Acquire error: %v", err2)
	}
	_, _, _, _, err3 := g.Acquire()
	if !errors.Is(err3, ErrSPIExhausted) {
		t.Errorf("third Acquire: expected ErrSPIExhausted, got %v", err3)
	}
}

func TestSPIGenerator_Release(t *testing.T) {
	g := NewSPIGenerator(100, 10, 5063, 5062, 5)
	spiCID, spiSID, cport, sport, _ := g.Acquire()
	if g.InUse() != 1 || g.FreeCount() != 4 {
		t.Fatalf("InUse=%d FreeCount=%d, want 1/4", g.InUse(), g.FreeCount())
	}
	g.Release(spiCID, spiSID, cport, sport)
	if g.InUse() != 0 {
		t.Errorf("InUse = %d, want 0 after release", g.InUse())
	}
	if g.FreeCount() != 5 {
		t.Errorf("FreeCount = %d, want 5 after release", g.FreeCount())
	}
	// Released tuple should be re-usable.
	_, _, _, _, err := g.Acquire()
	if err != nil {
		t.Errorf("Acquire after release error: %v", err)
	}
}

func TestSPIGenerator_Release_NotInUse(t *testing.T) {
	g := NewSPIGenerator(100, 10, 5063, 5062, 5)
	// Release an SPI that was never acquired → no-op.
	g.Release(999, 1000, 9999, 9999)
	if g.InUse() != 0 {
		t.Errorf("InUse = %d, want 0", g.InUse())
	}
}

func TestSPIGenerator_Clean(t *testing.T) {
	g := NewSPIGenerator(100, 10, 5063, 5062, 5)
	g.Acquire()
	g.Acquire()
	if g.InUse() != 2 {
		t.Fatalf("InUse = %d, want 2", g.InUse())
	}
	g.Clean()
	if g.InUse() != 0 {
		t.Errorf("InUse = %d, want 0 after Clean", g.InUse())
	}
	if g.FreeCount() != 5 {
		t.Errorf("FreeCount = %d, want 5 after Clean", g.FreeCount())
	}
}

// ---------------------------------------------------------------------------
// IPsecModule - CreateSA
// ---------------------------------------------------------------------------

func TestCreateSA_Success(t *testing.T) {
	m := NewIPsecModule()
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	sa, err := m.CreateSA(msg, wwwAuthBody)
	if err != nil {
		t.Fatalf("CreateSA error: %v", err)
	}
	// UE-side values from Security-Client.
	if sa.SPIUC != 12345 {
		t.Errorf("spi_uc = %d, want 12345", sa.SPIUC)
	}
	if sa.PortUC != 5060 {
		t.Errorf("port_uc = %d, want 5060", sa.PortUC)
	}
	// P-CSCF-side values allocated.
	if sa.SPIPC == 0 {
		t.Error("expected non-zero spi_pc")
	}
	if sa.SPIPS == 0 {
		t.Error("expected non-zero spi_ps")
	}
	if sa.PortPC == 0 {
		t.Error("expected non-zero port_pc")
	}
	if sa.PortPS == 0 {
		t.Error("expected non-zero port_ps")
	}
	// CK/IK extracted.
	if !bytesEqual(sa.CK, bytesFromHex(ckHex)) {
		t.Error("ck mismatch")
	}
	if !bytesEqual(sa.IK, bytesFromHex(ikHex)) {
		t.Error("ik mismatch")
	}
	if m.SACount() != 1 {
		t.Errorf("SACount = %d, want 1", m.SACount())
	}
	// SA stored in table.
	found := m.FindSA("10.0.0.1")
	if found == nil {
		t.Fatal("expected SA in table for 10.0.0.1")
	}
	if found.SPIPC != sa.SPIPC {
		t.Error("FindSA returned different SA")
	}
}

func TestCreateSA_NoSecurityClient(t *testing.T) {
	m := NewIPsecModule()
	msg := mustParseMsg(t, makeRegisterWithoutSecClient())
	_, err := m.CreateSA(msg, wwwAuthBody)
	if err == nil {
		t.Error("expected error for missing Security-Client")
	}
}

func TestCreateSA_NilMessage(t *testing.T) {
	m := NewIPsecModule()
	_, err := m.CreateSA(nil, wwwAuthBody)
	if err == nil {
		t.Error("expected error for nil message")
	}
}

func TestCreateSA_NoWWWAuth_AllowsNoKeys(t *testing.T) {
	// An empty WWW-Authenticate body is valid: the SA is created without
	// CK/IK (they are only needed when encryption is actually used).
	m := NewIPsecModule()
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	sa, err := m.CreateSA(msg, "")
	if err != nil {
		t.Fatalf("expected success with empty WWW-Auth, got: %v", err)
	}
	if sa.CK != nil || sa.IK != nil {
		t.Error("expected nil CK/IK when no WWW-Auth provided")
	}
}

func TestCreateSA_TunnelFailure_ReleasesSPI(t *testing.T) {
	m := NewIPsecModule()
	mt := &mockTunnel{failCreate: true}
	m.SetTunnelManager(mt)
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	_, err := m.CreateSA(msg, wwwAuthBody)
	if err == nil {
		t.Fatal("expected error when tunnel creation fails")
	}
	// The allocated SPI must have been released back.
	if m.spiGen.InUse() != 0 {
		t.Errorf("SPI InUse = %d, want 0 after tunnel failure", m.spiGen.InUse())
	}
	if m.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after failure", m.SACount())
	}
	creates, _, _ := mt.stats()
	if creates != 1 {
		t.Errorf("tunnel creates = %d, want 1", creates)
	}
}

// ---------------------------------------------------------------------------
// IPsecModule - DestroySA / FindSA / CleanAll
// ---------------------------------------------------------------------------

func TestDestroySA_Success(t *testing.T) {
	m := NewIPsecModule()
	mt := &mockTunnel{}
	m.SetTunnelManager(mt)
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	sa, _ := m.CreateSA(msg, wwwAuthBody)
	if m.SACount() != 1 {
		t.Fatalf("SACount = %d, want 1", m.SACount())
	}
	if err := m.DestroySA("10.0.0.1"); err != nil {
		t.Fatalf("DestroySA error: %v", err)
	}
	if m.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after destroy", m.SACount())
	}
	if m.FindSA("10.0.0.1") != nil {
		t.Error("expected nil after destroy")
	}
	if m.spiGen.InUse() != 0 {
		t.Errorf("SPI InUse = %d, want 0 after destroy", m.spiGen.InUse())
	}
	_, destroys, _ := mt.stats()
	if destroys != 1 {
		t.Errorf("tunnel destroys = %d, want 1", destroys)
	}
	// SA's SPI should be back in the free pool.
	if sa.SPIPC == 0 {
		t.Error("spi_pc was zero")
	}
}

func TestDestroySA_Unknown(t *testing.T) {
	m := NewIPsecModule()
	err := m.DestroySA("192.168.99.99")
	if err == nil {
		t.Error("expected error for unknown SA")
	}
}

func TestFindSA_Expired(t *testing.T) {
	m := NewIPsecModuleWithConfig(Config{
		ClientPort:      5062,
		ServerPort:      5063,
		MaxConnections:  5,
		SPIStart:        100,
		SPIRange:        100,
		SATTL:           20 * time.Millisecond,
	})
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	_, err := m.CreateSA(msg, wwwAuthBody)
	if err != nil {
		t.Fatalf("CreateSA error: %v", err)
	}
	if m.FindSA("10.0.0.1") == nil {
		t.Fatal("expected SA before expiry")
	}
	time.Sleep(40 * time.Millisecond)
	if m.FindSA("10.0.0.1") != nil {
		t.Error("expected nil for expired SA")
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewIPsecModuleWithConfig(Config{
		ClientPort:      5062,
		ServerPort:      5063,
		MaxConnections:  5,
		SPIStart:        100,
		SPIRange:        100,
		SATTL:           20 * time.Millisecond,
	})
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	m.CreateSA(msg, wwwAuthBody)
	time.Sleep(40 * time.Millisecond)
	removed := m.CleanupExpired()
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if m.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after cleanup", m.SACount())
	}
	if m.spiGen.InUse() != 0 {
		t.Errorf("SPI InUse = %d, want 0 after cleanup", m.spiGen.InUse())
	}
}

func TestCleanAll(t *testing.T) {
	m := NewIPsecModule()
	mt := &mockTunnel{}
	m.SetTunnelManager(mt)
	// Use two different UE hosts so both SAs are stored.
	msg1 := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	m.CreateSA(msg1, wwwAuthBody)
	msg2 := mustParseMsg(t, []byte("REGISTER sip:example.com SIP/2.0\r\n"+
		"Via: SIP/2.0/UDP 10.0.0.2:5060;branch=z9hG4bK776ipsec2\r\n"+
		"From: Bob <sip:bob@example.com>;tag=ftag2\r\n"+
		"To: Bob <sip:bob@example.com>\r\n"+
		"Call-ID: ipsec-call-2@10.0.0.2\r\n"+
		"CSeq: 1 REGISTER\r\n"+
		"Contact: <sip:bob@10.0.0.2:5060>\r\n"+
		"Security-Client: "+secClientBody+"\r\n"+
		"Expires: 3600\r\n"+
		"Content-Length: 0\r\n"+
		"\r\n"))
	m.CreateSA(msg2, wwwAuthBody)
	if m.SACount() != 2 {
		t.Fatalf("SACount = %d, want 2", m.SACount())
	}
	m.CleanAll()
	if m.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after CleanAll", m.SACount())
	}
	if m.spiGen.InUse() != 0 {
		t.Errorf("SPI InUse = %d, want 0 after CleanAll", m.spiGen.InUse())
	}
	_, destroys, cleanAlls := mt.stats()
	if destroys != 2 {
		t.Errorf("tunnel destroys = %d, want 2", destroys)
	}
	if cleanAlls != 1 {
		t.Errorf("cleanAlls = %d, want 1", cleanAlls)
	}
}

func TestReconfig(t *testing.T) {
	m := NewIPsecModule()
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	m.CreateSA(msg, wwwAuthBody)
	if m.SACount() != 1 {
		t.Fatalf("SACount = %d, want 1", m.SACount())
	}
	m.Reconfig()
	if m.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after Reconfig", m.SACount())
	}
	if m.spiGen.InUse() != 0 {
		t.Errorf("SPI InUse = %d, want 0 after Reconfig", m.spiGen.InUse())
	}
}

// ---------------------------------------------------------------------------
// Config / singleton
// ---------------------------------------------------------------------------

func TestSetConfig(t *testing.T) {
	m := NewIPsecModule()
	m.SetConfig(Config{
		ClientPort:      7000,
		ServerPort:      7001,
		MaxConnections:  10,
		SPIStart:        200,
		SPIRange:        500,
	})
	cfg := m.Config()
	if cfg.ClientPort != 7000 {
		t.Errorf("ClientPort = %d, want 7000", cfg.ClientPort)
	}
	if cfg.SPIStart != 200 {
		t.Errorf("SPIStart = %d, want 200", cfg.SPIStart)
	}
	// SPI generator should reflect new config.
	spiCID, _, _, _, _ := m.spiGen.Acquire()
	if spiCID != 200 {
		t.Errorf("first SPI = %d, want 200 (new SPIStart)", spiCID)
	}
}

func TestSetTunnelManager(t *testing.T) {
	m := NewIPsecModule()
	if _, ok := m.TunnelManager().(NoopTunnelManager); !ok {
		t.Error("expected NoopTunnelManager by default")
	}
	mt := &mockTunnel{}
	m.SetTunnelManager(mt)
	if m.TunnelManager() != TunnelManager(mt) {
		t.Error("expected injected tunnel manager")
	}
	m.SetTunnelManager(nil)
	if _, ok := m.TunnelManager().(NoopTunnelManager); !ok {
		t.Error("expected NoopTunnelManager after nil injection")
	}
}

func TestDefaultIPsec_Singleton(t *testing.T) {
	a := DefaultIPsec()
	b := DefaultIPsec()
	if a != b {
		t.Error("DefaultIPsec must return the same instance")
	}
}

func TestInit_Reset(t *testing.T) {
	a := DefaultIPsec()
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	a.CreateSA(msg, wwwAuthBody)
	if a.SACount() != 1 {
		t.Fatalf("SACount = %d, want 1", a.SACount())
	}
	Init()
	b := DefaultIPsec()
	if b.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after Init", b.SACount())
	}
}

// ---------------------------------------------------------------------------
// extractUEAddress
// ---------------------------------------------------------------------------

func TestExtractUEAddress_ViaHostPort(t *testing.T) {
	msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
	host, port := extractUEAddress(msg)
	if host != "10.0.0.1" {
		t.Errorf("host = %q, want 10.0.0.1", host)
	}
	if port != 5060 {
		t.Errorf("port = %d, want 5060", port)
	}
}

func TestExtractUEAddress_ReceivedRPort(t *testing.T) {
	raw := []byte("REGISTER sip:example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP 10.0.0.1:5060;branch=z9hG4bK776recv;received=192.168.1.100;rport=54321\r\n" +
		"From: <sip:a@b.com>\r\n" +
		"To: <sip:a@b.com>\r\n" +
		"Call-ID: x@y\r\n" +
		"CSeq: 1 REGISTER\r\n" +
		"Contact: <sip:a@10.0.0.1>\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n")
	msg := mustParseMsg(t, raw)
	host, port := extractUEAddress(msg)
	if host != "192.168.1.100" {
		t.Errorf("host = %q, want 192.168.1.100", host)
	}
	if port != 54321 {
		t.Errorf("port = %d, want 54321", port)
	}
}

func TestExtractUEAddress_NilMessage(t *testing.T) {
	host, port := extractUEAddress(nil)
	if host != "" || port != 0 {
		t.Errorf("expected empty for nil message, got %q:%d", host, port)
	}
}

// ---------------------------------------------------------------------------
// Concurrent access
// ---------------------------------------------------------------------------

func TestConcurrentAccess(t *testing.T) {
	m := NewIPsecModule()
	mt := &mockTunnel{}
	m.SetTunnelManager(mt)

	const n = 30
	var wg sync.WaitGroup
	// Concurrent CreateSA.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := mustParseMsg(t, makeRegisterWithSecClient(secClientBody))
			_, _ = m.CreateSA(msg, wwwAuthBody)
		}()
	}
	// Concurrent reads.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.SACount()
			_ = m.FindSA("10.0.0.1")
			_ = m.spiGen.InUse()
		}()
	}
	wg.Wait()
	m.CleanAll()
	if m.SACount() != 0 {
		t.Errorf("SACount = %d, want 0 after CleanAll", m.SACount())
	}
}
