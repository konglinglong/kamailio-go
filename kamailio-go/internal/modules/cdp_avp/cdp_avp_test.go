// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for the CDP AVP module.
 */

package cdp_avp

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/modules/cdp"
)

func TestBuildAuthSessionState(t *testing.T) {
	avp := BuildAuthSessionState(AuthSessionStateMaintained)
	if avp.Code != CodeAuthSessionState {
		t.Errorf("Code = %d, want %d", avp.Code, CodeAuthSessionState)
	}
	if avp.Flags&cdp.AVPFlagMandatory == 0 {
		t.Errorf("Mandatory flag not set")
	}
	if len(avp.Value) != 4 {
		t.Fatalf("Value len = %d, want 4", len(avp.Value))
	}
	if got := binary.BigEndian.Uint32(avp.Value); got != AuthSessionStateMaintained {
		t.Errorf("Value = %d, want %d", got, AuthSessionStateMaintained)
	}
}

func TestBuildUserName(t *testing.T) {
	avp := BuildUserName("alice@example.com")
	if avp.Code != CodeUserName {
		t.Errorf("Code = %d, want %d", avp.Code, CodeUserName)
	}
	if string(avp.Value) != "alice@example.com" {
		t.Errorf("Value = %q, want alice@example.com", avp.Value)
	}
}

func TestBuildResultCode(t *testing.T) {
	avp := BuildResultCode(2001) // DIAMETER_SUCCESS
	if avp.Code != CodeResultCode {
		t.Errorf("Code = %d, want %d", avp.Code, CodeResultCode)
	}
	if got := binary.BigEndian.Uint32(avp.Value); got != 2001 {
		t.Errorf("Value = %d, want 2001", got)
	}
}

func TestBuildSubscriptionID(t *testing.T) {
	avp := BuildSubscriptionID("1234567890", "END_USER_IMSI")
	if avp.Code != CodeSubscriptionID {
		t.Errorf("Code = %d, want %d", avp.Code, CodeSubscriptionID)
	}
	// The grouped value contains two sub-AVPs.
	subType, err := ParseAVP(avp.Value)
	if err != nil {
		t.Fatalf("parse sub-type: %v", err)
	}
	if subType.Code != CodeSubscriptionIDType {
		t.Errorf("sub-type Code = %d, want %d", subType.Code, CodeSubscriptionIDType)
	}
	if got := binary.BigEndian.Uint32(subType.Value); got != SubIDEndUserIMSI {
		t.Errorf("sub-type Value = %d, want %d", got, SubIDEndUserIMSI)
	}
	subData, err := ParseAVP(avp.Value[len(EncodeAVP(subType)):])
	if err != nil {
		t.Fatalf("parse sub-data: %v", err)
	}
	if subData.Code != CodeSubscriptionIDData {
		t.Errorf("sub-data Code = %d, want %d", subData.Code, CodeSubscriptionIDData)
	}
	if string(subData.Value) != "1234567890" {
		t.Errorf("sub-data Value = %q, want 1234567890", subData.Value)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []*cdp.DiameterAVP{
		BuildUserName("alice@example.com"),
		BuildResultCode(2001),
		BuildAuthSessionState(1),
		{Code: 999, Flags: cdp.AVPFlagMandatory | cdp.AVPFlagVendor, VendorID: 10415, Value: []byte{0, 0, 0, 1}},
		// Odd-length value exercises padding.
		{Code: 100, Flags: cdp.AVPFlagMandatory, Value: []byte("odd")},
	}
	for i, orig := range cases {
		enc := EncodeAVP(orig)
		if len(enc) < cdp.AVPHeaderLen {
			t.Fatalf("case %d: encoded too short", i)
		}
		dec, err := ParseAVP(enc)
		if err != nil {
			t.Fatalf("case %d: ParseAVP failed: %v", i, err)
		}
		if dec.Code != orig.Code {
			t.Errorf("case %d: Code = %d, want %d", i, dec.Code, orig.Code)
		}
		if dec.Flags != orig.Flags {
			t.Errorf("case %d: Flags = %d, want %d", i, dec.Flags, orig.Flags)
		}
		if dec.Flags&cdp.AVPFlagVendor != 0 && dec.VendorID != orig.VendorID {
			t.Errorf("case %d: VendorID = %d, want %d", i, dec.VendorID, orig.VendorID)
		}
		if !bytes.Equal(dec.Value, orig.Value) {
			t.Errorf("case %d: Value = %q, want %q", i, dec.Value, orig.Value)
		}
	}
}

func TestParseAVPErrors(t *testing.T) {
	if _, err := ParseAVP(nil); err == nil {
		t.Errorf("ParseAVP(nil) should error")
	}
	if _, err := ParseAVP([]byte{1, 2, 3}); err == nil {
		t.Errorf("ParseAVP short buffer should error")
	}
	// Bad length field: set it below the minimum AVP header length.
	bad := EncodeAVP(BuildUserName("x"))
	bad[7] = 3 // length too small (< AVPHeaderLen)
	if _, err := ParseAVP(bad); err == nil {
		t.Errorf("ParseAVP with bad length should error")
	}
}

func TestEncodeAVPNil(t *testing.T) {
	if enc := EncodeAVP(nil); enc != nil {
		t.Errorf("EncodeAVP(nil) should return nil, got %v", enc)
	}
}

func TestGetAVPByCode(t *testing.T) {
	msg := &cdp.DiameterMessage{
		AVPs: []cdp.DiameterAVP{
			*BuildUserName("alice@example.com"),
			*BuildResultCode(2001),
		},
	}
	if avp := GetAVPByCode(msg, CodeResultCode); avp == nil {
		t.Errorf("GetAVPByCode(ResultCode) returned nil")
	} else if got := binary.BigEndian.Uint32(avp.Value); got != 2001 {
		t.Errorf("Result-Code value = %d, want 2001", got)
	}
	if avp := GetAVPByCode(msg, CodeUserName); avp == nil {
		t.Errorf("GetAVPByCode(UserName) returned nil")
	}
	if avp := GetAVPByCode(msg, 99999); avp != nil {
		t.Errorf("GetAVPByCode(unknown) should return nil, got %v", avp)
	}
	if avp := GetAVPByCode(nil, 1); avp != nil {
		t.Errorf("GetAVPByCode(nil) should return nil")
	}
}

func TestAVPBuilderAndParser(t *testing.T) {
	b := NewAVPBuilder()
	p := NewAVPParser()

	avp := b.UserName("bob@example.com")
	enc := p.Encode(avp)
	dec, err := p.Parse(enc)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if dec.Code != CodeUserName {
		t.Errorf("Code = %d, want %d", dec.Code, CodeUserName)
	}
	if string(dec.Value) != "bob@example.com" {
		t.Errorf("Value = %q, want bob@example.com", dec.Value)
	}

	// Verify the builder wrappers delegate correctly.
	if b.AuthSessionState(1).Code != CodeAuthSessionState {
		t.Errorf("AuthSessionState builder wrong code")
	}
	if b.ResultCode(2001).Code != CodeResultCode {
		t.Errorf("ResultCode builder wrong code")
	}
	if b.SubscriptionID("x", "END_USER_E164").Code != CodeSubscriptionID {
		t.Errorf("SubscriptionID builder wrong code")
	}
}

func TestSubscriptionIDTypeMapping(t *testing.T) {
	cases := map[string]int{
		"END_USER_E164":    SubIDEndUserE164,
		"END_USER_IMSI":    SubIDEndUserIMSI,
		"END_USER_SIP_URI": SubIDEndUserSIPURI,
		"END_USER_NAI":     SubIDEndUserNAI,
		"unknown":          SubIDEndUserE164, // default
	}
	for name, want := range cases {
		avp := BuildSubscriptionID("id", name)
		subType, err := ParseAVP(avp.Value)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if got := binary.BigEndian.Uint32(subType.Value); got != uint32(want) {
			t.Errorf("type %s: value = %d, want %d", name, got, want)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			avp := BuildUserName("alice@example.com")
			enc := EncodeAVP(avp)
			ParseAVP(enc)
			msg := &cdp.DiameterMessage{AVPs: []cdp.DiameterAVP{*BuildResultCode(2001)}}
			GetAVPByCode(msg, CodeResultCode)
		}()
	}
	wg.Wait()
}
