// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Tests for NAPTR DNS resolution.
 */

package dns

import (
	"context"
	"testing"
)

// mockNAPTRProvider is a test stub for NAPTRProvider.
type mockNAPTRProvider struct {
	records []NAPTRRecord
	err     error
}

func (m *mockNAPTRProvider) LookupNAPTR(ctx context.Context, domain string) ([]NAPTRRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.records, nil
}

func TestNAPTR_ProtoFromService(t *testing.T) {
	cases := []struct {
		service string
		want    Proto
		ok      bool
	}{
		{"SIP+D2U", ProtoUDP, true},
		{"sip+d2u", ProtoUDP, true}, // case-insensitive
		{"SIP+D2T", ProtoTCP, true},
		{"SIPS+D2T", ProtoTLS, true},
		{"SIP+D2S", ProtoSCTP, true},
		{"SIP+D2W", ProtoWS, true},
		{"SIPS+D2W", ProtoWSS, true},
		{"E2U+SIP", ProtoUDP, false}, // enum-style, not a transport NAPTR
		{"", ProtoUDP, false},
	}
	for _, c := range cases {
		got, ok := naptrProtoFromService(c.service)
		if ok != c.ok {
			t.Errorf("naptrProtoFromService(%q): ok=%v want %v", c.service, ok, c.ok)
			continue
		}
		if ok && got != c.want {
			t.Errorf("naptrProtoFromService(%q): proto=%v want %v", c.service, got, c.want)
		}
	}
}

func TestNAPTR_EncodeQName(t *testing.T) {
	cases := []struct {
		input string
		want  []byte
	}{
		{"example.com.", []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}},
		{"example.com", []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}},
		{"a.b.c", []byte{1, 'a', 1, 'b', 1, 'c', 0}},
	}
	for _, c := range cases {
		got := encodeQName(c.input)
		if len(got) != len(c.want) {
			t.Errorf("encodeQName(%q) len=%d want %d", c.input, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("encodeQName(%q) byte[%d]=%d want %d", c.input, i, got[i], c.want[i])
				break
			}
		}
	}
}

func TestNAPTR_DecodeQName(t *testing.T) {
	// Build a message with a simple domain name.
	msg := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	name, off, err := decodeQName(msg, 0)
	if err != nil {
		t.Fatalf("decodeQName failed: %v", err)
	}
	if name != "example.com" {
		t.Errorf("got name %q, want example.com", name)
	}
	if off != len(msg) {
		t.Errorf("got offset %d, want %d", off, len(msg))
	}
}

func TestNAPTR_DecodeQName_Compressed(t *testing.T) {
	// Build a message where the second name uses a compression pointer
	// to the first name.
	msg := []byte{
		// offset 0: "example.com"
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		// offset 13: "sip" -> pointer to offset 0
		3, 's', 'i', 'p', 0xC0, 0x00,
	}
	name, off, err := decodeQName(msg, 13)
	if err != nil {
		t.Fatalf("decodeQName compressed failed: %v", err)
	}
	if name != "sip.example.com" {
		t.Errorf("got name %q, want sip.example.com", name)
	}
	// When a jump occurs, the returned offset is past the pointer.
	if off != 19 {
		t.Errorf("got offset %d, want 19", off)
	}
}

func TestNAPTR_ParseNAPTRRDATA(t *testing.T) {
	// NAPTR RDATA includes the replacement domain name as part of the
	// RDATA (per RFC 3403). Build: order=10, pref=20, flags="S",
	// service="SIP+D2U", regexp="", replacement="sip.com"
	rdata := []byte{
		0x00, 0x0A, // order=10
		0x00, 0x14, // pref=20
		0x01, 'S',        // flags="S"
		0x07, 'S', 'I', 'P', '+', 'D', '2', 'U', // service="SIP+D2U"
		0x00,             // regexp=""
		// replacement domain name "sip.com"
		3, 's', 'i', 'p', 3, 'c', 'o', 'm', 0,
	}
	// rdata starts at offset 0 in this synthetic fullMsg.
	rec, err := parseNAPTRRDATA(rdata, rdata, 0)
	if err != nil {
		t.Fatalf("parseNAPTRRDATA failed: %v", err)
	}
	if rec.Order != 10 {
		t.Errorf("order=%d want 10", rec.Order)
	}
	if rec.Preference != 20 {
		t.Errorf("preference=%d want 20", rec.Preference)
	}
	if rec.Flags != "S" {
		t.Errorf("flags=%q want S", rec.Flags)
	}
	if rec.Service != "SIP+D2U" {
		t.Errorf("service=%q want SIP+D2U", rec.Service)
	}
	if rec.Regexp != "" {
		t.Errorf("regexp=%q want empty", rec.Regexp)
	}
	if rec.Replacement != "sip.com" {
		t.Errorf("replacement=%q want sip.com", rec.Replacement)
	}
}

func TestNAPTR_BuildQuery(t *testing.T) {
	q := buildNAPTRQuery("example.com.")
	// Header is 12 bytes, QNAME is 13 bytes (example.com + null),
	// QTYPE 2 bytes, QCLASS 2 bytes. Total = 29.
	if len(q) != 12+13+4 {
		t.Fatalf("query length=%d, want 29", len(q))
	}
	// Check QDCOUNT = 1.
	if q[4] != 0 || q[5] != 1 {
		t.Errorf("QDCOUNT=%d want 1", uint16(q[4])<<8|uint16(q[5]))
	}
	// Check QTYPE = NAPTR (35) at offset 25.
	qtype := uint16(q[25])<<8 | uint16(q[26])
	if qtype != 35 {
		t.Errorf("QTYPE=%d want 35", qtype)
	}
}

func TestNAPTR_ParseResponse_NoAnswers(t *testing.T) {
	// Build a minimal response with ANCOUNT=0.
	id := uint16(0x1234)
	msg := []byte{
		byte(id >> 8), byte(id),
		0x81, 0x00, // QR=1, RD=1, RA=1, rcode=0
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x00, // ANCOUNT=0
		0x00, 0x00, 0x00, 0x00,
		// Question: example.com NAPTR IN
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0x00, 0x23, // QTYPE=35
		0x00, 0x01, // QCLASS=IN
	}
	recs, err := parseNAPTRResponse(msg)
	if err == nil {
		t.Fatalf("expected error, got records: %v", recs)
	}
}

func TestNAPTR_ParseResponse_BadRCode(t *testing.T) {
	msg := []byte{
		0x12, 0x34,
		0x81, 0x03, // rcode=3 (NXDOMAIN)
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	_, err := parseNAPTRResponse(msg)
	if err == nil {
		t.Fatal("expected error for NXDOMAIN")
	}
}

func TestNAPTR_MockProvider_LookupNAPTR(t *testing.T) {
	mock := &mockNAPTRProvider{
		records: []NAPTRRecord{
			{Order: 100, Preference: 10, Flags: "S", Service: "SIP+D2U", Replacement: "_sip._udp.example.com"},
			{Order: 100, Preference: 20, Flags: "S", Service: "SIP+D2T", Replacement: "_sip._tcp.example.com"},
		},
	}
	recs, err := mock.LookupNAPTR(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
}

func TestNAPTR_MockProvider_Error(t *testing.T) {
	mock := &mockNAPTRProvider{err: ErrNoNAPTR}
	_, err := mock.LookupNAPTR(context.Background(), "example.com")
	if err != ErrNoNAPTR {
		t.Fatalf("expected ErrNoNAPTR, got %v", err)
	}
}

func TestNAPTR_SetDefaultProvider(t *testing.T) {
	original := DefaultNAPTRProvider()
	defer SetDefaultNAPTRProvider(original)

	mock := &mockNAPTRProvider{
		records: []NAPTRRecord{
			{Order: 50, Preference: 10, Flags: "S", Service: "SIP+D2U", Replacement: "_sip._udp.example.com"},
		},
	}
	SetDefaultNAPTRProvider(mock)

	got := DefaultNAPTRProvider()
	if got != mock {
		t.Fatal("SetDefaultNAPTRProvider did not replace the provider")
	}

	recs, err := got.LookupNAPTR(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 || recs[0].Service != "SIP+D2U" {
		t.Fatalf("unexpected records: %+v", recs)
	}
}

func TestNAPTR_ResolveNAPTRRecords(t *testing.T) {
	original := DefaultNAPTRProvider()
	defer SetDefaultNAPTRProvider(original)

	mock := &mockNAPTRProvider{
		records: []NAPTRRecord{
			{Order: 50, Preference: 10, Flags: "S", Service: "SIP+D2U"},
		},
	}
	SetDefaultNAPTRProvider(mock)

	r := NewResolver()
	recs, err := r.ResolveNAPTRRecords(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("ResolveNAPTRRecords failed: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
}

func TestNAPTR_ResolveNAPTRRecords_NoRecords(t *testing.T) {
	original := DefaultNAPTRProvider()
	defer SetDefaultNAPTRProvider(original)

	SetDefaultNAPTRProvider(&mockNAPTRProvider{err: ErrNoNAPTR})

	r := NewResolver()
	_, err := r.ResolveNAPTRRecords(context.Background(), "example.com")
	if err != ErrNoNAPTR {
		t.Fatalf("expected ErrNoNAPTR, got %v", err)
	}
}

func TestNAPTR_ReadResolvConf_Fallback(t *testing.T) {
	// This just verifies the fallback path returns a server.
	servers := readResolvConf()
	if len(servers) == 0 {
		t.Fatal("readResolvConf returned no servers")
	}
	// Each entry should be host:53 format.
	for _, s := range servers {
		if s == "" {
			t.Errorf("empty server entry")
		}
	}
}

func TestNAPTR_SystemProvider_EmptyDomain(t *testing.T) {
	p := &systemNAPTRProvider{servers: []string{"127.0.0.1:53"}, timeout: 1}
	_, err := p.LookupNAPTR(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty domain")
	}
}
