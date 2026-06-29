// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for msg_translator package
 */

package msgtranslator

import (
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

func TestBuildReqBufFromSIPReq(t *testing.T) {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:  str.Mk("INVITE"),
			URI:     str.Mk("sip:alice@example.com"),
			Version: str.Mk("SIP/2.0"),
		},
	}

	buf := BuildReqBufFromSIPReq(msg, nil, 0)
	if len(buf) == 0 {
		t.Error("BuildReqBufFromSIPReq returned empty buffer")
	}
}

func TestBuildResBufFromSIPRes(t *testing.T) {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgReply,
		Reply: &parser.ReplyLine{
			Version:    str.Mk("SIP/2.0"),
			Status:     str.Mk("200"),
			Reason:     str.Mk("OK"),
			StatusCode: 200,
		},
	}

	buf := BuildResBufFromSIPRes(msg)
	if len(buf) == 0 {
		t.Error("BuildResBufFromSIPRes returned empty buffer")
	}
}

func TestBuildResBufFromSIPReq(t *testing.T) {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:  str.Mk("INVITE"),
			URI:     str.Mk("sip:alice@example.com"),
			Version: str.Mk("SIP/2.0"),
		},
	}

	buf, bmark := BuildResBufFromSIPReq(200, "OK", "tag123", msg)
	if len(buf) == 0 {
		t.Error("BuildResBufFromSIPReq returned empty buffer")
	}
	if bmark == nil {
		t.Error("Bookmark is nil")
	}
}

func TestViaBuilder(t *testing.T) {
	host := str.Mk("192.168.1.1")
	port := str.Mk("5060")
	hp := &HostPort{
		Host: &host,
		Port: &port,
	}
	branch := str.Mk("z9hG4bK.12345")

	result, err := ViaBuilder(nil, nil, branch, str.Str{}, hp)
	if err != nil {
		t.Fatalf("ViaBuilder failed: %v", err)
	}
	if result.IsEmpty() {
		t.Error("ViaBuilder returned empty result")
	}
	resultStr := result.String()
	if len(resultStr) == 0 {
		t.Error("Via result is empty string")
	}
}

func TestViaBuilderInvalid(t *testing.T) {
	_, err := ViaBuilder(nil, nil, str.Str{}, str.Str{}, nil)
	if err == nil {
		t.Error("ViaBuilder with nil hostport should return error")
	}
}

func TestCreateViaHF(t *testing.T) {
	msg := &parser.SIPMsg{}
	branch := str.Mk("z9hG4bK.test")

	result, err := CreateViaHF(msg, nil, branch, nil)
	if err != nil {
		t.Fatalf("CreateViaHF failed: %v", err)
	}
	if result.IsEmpty() {
		t.Error("CreateViaHF returned empty result")
	}
}

func TestBranchBuilder(t *testing.T) {
	result := BranchBuilder(12345, 0, "", str.Str{}, 1)
	if result.IsEmpty() {
		t.Error("BranchBuilder returned empty result")
	}
	if !result.HasPrefixString("z9hG4bK.") {
		t.Errorf("Branch should start with z9hG4bK., got %s", result.String())
	}
}

func TestIDBuilder(t *testing.T) {
	msg := &parser.SIPMsg{}
	result := IDBuilder(msg)
	if !result.IsEmpty() {
		t.Error("IDBuilder with empty msg should return empty")
	}
}

func TestReceivedTest(t *testing.T) {
	msg := &parser.SIPMsg{}
	result := ReceivedTest(msg)
	if result != 0 {
		t.Errorf("ReceivedTest with no via should return 0, got %d", result)
	}
}

func TestReceivedViaTest(t *testing.T) {
	msg := &parser.SIPMsg{}
	result := ReceivedViaTest(msg)
	if result != 0 {
		t.Errorf("ReceivedViaTest with no via should return 0, got %d", result)
	}
}

func TestBuildOnlyHeaders(t *testing.T) {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:  str.Mk("INVITE"),
			URI:     str.Mk("sip:test@example.com"),
			Version: str.Mk("SIP/2.0"),
		},
	}

	buf, err := BuildOnlyHeaders(msg, 0)
	if err != nil {
		t.Fatalf("BuildOnlyHeaders failed: %v", err)
	}
	if len(buf) == 0 {
		t.Error("BuildOnlyHeaders returned empty buffer")
	}

	buf2, err := BuildOnlyHeaders(msg, 1)
	if err != nil {
		t.Fatalf("BuildOnlyHeaders(skip=1) failed: %v", err)
	}
	if len(buf2) >= len(buf) {
		t.Error("BuildOnlyHeaders with skip should be shorter")
	}
}

func TestBuildBody(t *testing.T) {
	msg := &parser.SIPMsg{}
	body := BuildBody(msg)
	if body != nil {
		t.Error("BuildBody with empty msg should return nil")
	}
}

func TestBuildAll(t *testing.T) {
	msg := &parser.SIPMsg{}
	msg.FirstLine = &parser.MsgStart{
		Type: parser.MsgRequest,
		Req: &parser.RequestLine{
			Method:  str.Mk("INVITE"),
			URI:     str.Mk("sip:test@example.com"),
			Version: str.Mk("SIP/2.0"),
		},
	}

	buf, err := BuildAll(msg, 1)
	if err != nil {
		t.Fatalf("BuildAll failed: %v", err)
	}
	if len(buf) == 0 {
		t.Error("BuildAll returned empty buffer")
	}
}

func TestBuildAllNilMsg(t *testing.T) {
	_, err := BuildAll(nil, 0)
	if err == nil {
		t.Error("BuildAll with nil msg should return error")
	}
}

func TestNewMsgTranslator(t *testing.T) {
	mt := NewMsgTranslator()
	if mt == nil {
		t.Fatal("NewMsgTranslator returned nil")
	}
}

func TestGlobalMsgTranslator(t *testing.T) {
	mt := GlobalMsgTranslator()
	if mt == nil {
		t.Fatal("GlobalMsgTranslator returned nil")
	}
}

func TestComputeHash(t *testing.T) {
	callID := str.Mk("call-123@example.com")
	cseq := str.Mk("1 INVITE")

	h := ComputeHash(callID, cseq)
	if h == 0 {
		t.Error("ComputeHash returned 0")
	}

	h2 := ComputeHash(callID, cseq)
	if h != h2 {
		t.Error("ComputeHash should be deterministic")
	}
}

func TestViaBranchParser(t *testing.T) {
	vb := &ViaBranch{}
	result := ViaBranchParser(str.Str{}, vb)
	if result != -1 {
		t.Error("ViaBranchParser with empty branch should return -1")
	}
}

func TestGetBoundary(t *testing.T) {
	boundary := str.Str{}
	result := GetBoundary(nil, &boundary)
	if result != -1 {
		t.Error("GetBoundary with nil msg should return -1")
	}
}

func TestBuildSIPMsgFromBuf(t *testing.T) {
	msg := &parser.SIPMsg{}
	result := BuildSIPMsgFromBuf(msg, []byte("test"), 1)
	if result != 0 {
		t.Errorf("BuildSIPMsgFromBuf returned %d, expected 0", result)
	}
}
