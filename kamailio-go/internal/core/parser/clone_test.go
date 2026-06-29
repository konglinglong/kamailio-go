// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Tests for SIPMsg.Clone() deep copy - matching C sip_msg_clone.c behaviour.
 */

package parser

import (
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/str"
)

// Test messages used across the clone tests.

var cloneTestInvite = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com:5060;branch=z9hG4bK776asdhds;rport\r\n" +
	"Max-Forwards: 70\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var cloneTestResponse = []byte("SIP/2.0 200 OK\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com:5060;branch=z9hG4bK776asdhds;rport\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>;tag=a6c85cf\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:bob@192.0.2.4>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n")

var cloneTestInviteWithBody = []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP pc33.example.com:5060;branch=z9hG4bK776asdhds\r\n" +
	"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
	"To: Bob <sip:bob@example.com>\r\n" +
	"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
	"CSeq: 314159 INVITE\r\n" +
	"Contact: <sip:alice@pc33.example.com>\r\n" +
	"Content-Type: application/sdp\r\n" +
	"Content-Length: 12\r\n" +
	"\r\n" +
	"v=0\r\no=abc\r\n")

// TestCloneBasicRequest clones a basic INVITE request and verifies that all
// scalar fields, the first line, buffer and headers are copied correctly and
// independently.
func TestCloneBasicRequest(t *testing.T) {
	msg, err := ParseMsg(cloneTestInvite)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}
	if clone == nil {
		t.Fatal("clone is nil")
	}

	// Scalar fields
	if clone.ID != msg.ID {
		t.Errorf("ID mismatch: %d != %d", clone.ID, msg.ID)
	}
	if clone.Len != msg.Len {
		t.Errorf("Len mismatch: %d != %d", clone.Len, msg.Len)
	}
	if clone.BufSize != msg.BufSize {
		t.Errorf("BufSize mismatch: %d != %d", clone.BufSize, msg.BufSize)
	}
	if clone.ParsedFlag != msg.ParsedFlag {
		t.Errorf("ParsedFlag mismatch")
	}

	// Buffer independence
	if clone.Buf == nil {
		t.Fatal("clone buffer is nil")
	}
	if &clone.Buf[0] == &msg.Buf[0] {
		t.Error("clone buffer shares memory with original")
	}
	if string(clone.Buf) != string(msg.Buf) {
		t.Error("clone buffer content differs from original")
	}

	// First line
	if clone.FirstLine == nil {
		t.Fatal("clone first line is nil")
	}
	if clone.FirstLine.Type != msg.FirstLine.Type {
		t.Errorf("first line type mismatch")
	}
	if clone.FirstLine.Req == nil {
		t.Fatal("clone request line is nil")
	}
	if clone.FirstLine.Req.Method.String() != msg.FirstLine.Req.Method.String() {
		t.Errorf("method mismatch: %s != %s",
			clone.FirstLine.Req.Method.String(),
			msg.FirstLine.Req.Method.String())
	}
	if clone.FirstLine.Req.URI.String() != msg.FirstLine.Req.URI.String() {
		t.Errorf("URI mismatch: %s != %s",
			clone.FirstLine.Req.URI.String(),
			msg.FirstLine.Req.URI.String())
	}
	if clone.FirstLine.Req.Version.String() != msg.FirstLine.Req.Version.String() {
		t.Errorf("version mismatch")
	}
	if clone.FirstLine.Req.MethodValue != msg.FirstLine.Req.MethodValue {
		t.Errorf("method value mismatch")
	}

	// Headers
	if len(clone.Headers) != len(msg.Headers) {
		t.Fatalf("header count mismatch: %d != %d",
			len(clone.Headers), len(msg.Headers))
	}
	for i, h := range msg.Headers {
		ch := clone.Headers[i]
		if ch == h {
			t.Errorf("header %d shares pointer with original", i)
		}
		if ch.Name.String() != h.Name.String() {
			t.Errorf("header %d name mismatch: %s != %s",
				i, ch.Name.String(), h.Name.String())
		}
		if ch.Body.String() != h.Body.String() {
			t.Errorf("header %d body mismatch: %s != %s",
				i, ch.Body.String(), h.Body.String())
		}
		if ch.Type != h.Type {
			t.Errorf("header %d type mismatch", i)
		}
	}

	// Quick references must be set and point to cloned headers
	if clone.HdrVia1 == nil {
		t.Error("clone HdrVia1 is nil")
	}
	if clone.HdrVia1 == msg.HdrVia1 {
		t.Error("clone HdrVia1 points to original header")
	}
	if clone.From == nil {
		t.Error("clone From is nil")
	}
	if clone.From == msg.From {
		t.Error("clone From points to original header")
	}
	if clone.To == nil {
		t.Error("clone To is nil")
	}
	if clone.CallID == nil {
		t.Error("clone CallID is nil")
	}
	if clone.CSeq == nil {
		t.Error("clone CSeq is nil")
	}
	if clone.Contact == nil {
		t.Error("clone Contact is nil")
	}
	if clone.ContentType == nil {
		t.Error("clone ContentType is nil")
	}
	if clone.ContentLength == nil {
		t.Error("clone ContentLength is nil")
	}
}

// TestCloneWithHeaders clones a message with many headers and verifies
// that every header and quick reference is correctly cloned.
func TestCloneWithHeaders(t *testing.T) {
	raw := []byte("INVITE sip:user@example.com SIP/2.0\r\n" +
		"Via: SIP/2.0/UDP pc33.example.com;branch=z9hG4bK776asdhds\r\n" +
		"Max-Forwards: 70\r\n" +
		"From: Alice <sip:alice@example.com>;tag=1928301774\r\n" +
		"To: Bob <sip:bob@example.com>\r\n" +
		"Call-ID: a84b4c76e66710@pc33.example.com\r\n" +
		"CSeq: 314159 INVITE\r\n" +
		"Contact: <sip:alice@pc33.example.com>\r\n" +
		"Route: <sip:route1@example.com;lr>\r\n" +
		"Record-Route: <sip:rr@example.com;lr>\r\n" +
		"Supported: 100rel\r\n" +
		"Require: 100rel\r\n" +
		"Allow: INVITE, ACK, CANCEL\r\n" +
		"Expires: 3600\r\n" +
		"Content-Type: application/sdp\r\n" +
		"Content-Length: 0\r\n" +
		"User-Agent: Kamailio-Go-Test\r\n" +
		"Server: TestServer\r\n" +
		"\r\n")

	msg, err := ParseMsg(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}

	// Verify header count
	if len(clone.Headers) != len(msg.Headers) {
		t.Fatalf("header count mismatch: %d != %d",
			len(clone.Headers), len(msg.Headers))
	}

	// Verify all quick references are set and point to clones
	checkRefs := []struct {
		name string
		hdr  *HdrField
	}{
		{"HdrVia1", clone.HdrVia1},
		{"From", clone.From},
		{"To", clone.To},
		{"CallID", clone.CallID},
		{"CSeq", clone.CSeq},
		{"Contact", clone.Contact},
		{"MaxForwards", clone.MaxForwards},
		{"Route", clone.Route},
		{"RecordRoute", clone.RecordRoute},
		{"Supported", clone.Supported},
		{"Require", clone.Require},
		{"Allow", clone.Allow},
		{"Expires", clone.Expires},
		{"ContentType", clone.ContentType},
		{"ContentLength", clone.ContentLength},
		{"UserAgent", clone.UserAgent},
		{"Server", clone.Server},
	}
	for _, r := range checkRefs {
		if r.hdr == nil {
			t.Errorf("clone %s is nil", r.name)
		}
	}

	// Verify that quick references point into the cloned header list
	cloneSet := make(map[*HdrField]bool)
	for _, h := range clone.Headers {
		cloneSet[h] = true
	}
	for _, r := range checkRefs {
		if r.hdr != nil && !cloneSet[r.hdr] {
			t.Errorf("clone %s does not point into cloned header list", r.name)
		}
	}

	// Verify LastHeader (only set when AddHeader is used, not by parser)
	if msg.LastHeader != nil {
		if clone.LastHeader == nil {
			t.Error("clone LastHeader is nil")
		}
		if clone.LastHeader == msg.LastHeader {
			t.Error("clone LastHeader shares pointer with original")
		}
		// LastHeader should point into the cloned header list
		found := false
		for _, h := range clone.Headers {
			if h == clone.LastHeader {
				found = true
				break
			}
		}
		if !found {
			t.Error("clone LastHeader does not point into cloned header list")
		}
	}

	// Verify Siblings chain is preserved
	for i := 0; i < len(msg.Headers)-1; i++ {
		if msg.Headers[i].Siblings == nil {
			continue
		}
		if clone.Headers[i].Siblings == nil {
			t.Errorf("clone header %d Siblings is nil", i)
		} else if clone.Headers[i].Siblings != clone.Headers[i+1] {
			t.Errorf("clone header %d Siblings does not point to next header", i)
		}
	}
}

// TestCloneResponse clones a SIP response and verifies the reply line.
func TestCloneResponse(t *testing.T) {
	msg, err := ParseMsg(cloneTestResponse)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}

	if !clone.IsReply() {
		t.Fatal("clone is not a reply")
	}
	if clone.FirstLine == nil || clone.FirstLine.Reply == nil {
		t.Fatal("clone reply line is nil")
	}

	if clone.FirstLine.Reply.StatusCode != msg.FirstLine.Reply.StatusCode {
		t.Errorf("status code mismatch: %d != %d",
			clone.FirstLine.Reply.StatusCode,
			msg.FirstLine.Reply.StatusCode)
	}
	if clone.FirstLine.Reply.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", clone.FirstLine.Reply.StatusCode)
	}
	if clone.FirstLine.Reply.Reason.String() != msg.FirstLine.Reply.Reason.String() {
		t.Errorf("reason mismatch: %s != %s",
			clone.FirstLine.Reply.Reason.String(),
			msg.FirstLine.Reply.Reason.String())
	}
	if clone.FirstLine.Reply.Reason.String() != "OK" {
		t.Errorf("expected reason 'OK', got '%s'",
			clone.FirstLine.Reply.Reason.String())
	}
	if clone.FirstLine.Reply.Version.String() != "SIP/2.0" {
		t.Errorf("expected version 'SIP/2.0', got '%s'",
			clone.FirstLine.Reply.Version.String())
	}

	// Verify To header has tag in the clone
	if clone.To == nil {
		t.Fatal("clone To header is nil")
	}

	// Verify independence of reply fields
	if &clone.FirstLine.Reply.Reason.S[0] == &msg.FirstLine.Reply.Reason.S[0] {
		t.Error("clone reply reason shares memory with original")
	}
}

// TestCloneIndependence modifies the clone and verifies the original is
// unaffected, proving that no memory is shared.
func TestCloneIndependence(t *testing.T) {
	msg, err := ParseMsg(cloneTestInvite)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}

	// Save original values
	origBufFirst := msg.Buf[0]
	origMethodFirst := msg.FirstLine.Req.Method.S[0]
	origURIFirst := msg.FirstLine.Req.URI.S[0]
	origHdrBodyFirst := msg.Headers[0].Body.S[0]
	origHdrNameFirst := msg.Headers[0].Name.S[0]

	// Modify clone's buffer
	clone.Buf[0] = 'X'
	if msg.Buf[0] != origBufFirst {
		t.Error("original buffer modified by clone change")
	}

	// Modify clone's first line method
	if clone.FirstLine.Req.Method.Len > 0 {
		clone.FirstLine.Req.Method.S[0] = 'X'
		if msg.FirstLine.Req.Method.S[0] != origMethodFirst {
			t.Error("original method modified by clone change")
		}
	}

	// Modify clone's first line URI
	if clone.FirstLine.Req.URI.Len > 0 {
		clone.FirstLine.Req.URI.S[0] = 'X'
		if msg.FirstLine.Req.URI.S[0] != origURIFirst {
			t.Error("original URI modified by clone change")
		}
	}

	// Modify clone's header body
	if clone.Headers[0].Body.Len > 0 {
		clone.Headers[0].Body.S[0] = 'X'
		if msg.Headers[0].Body.S[0] != origHdrBodyFirst {
			t.Error("original header body modified by clone change")
		}
	}

	// Modify clone's header name
	if clone.Headers[0].Name.Len > 0 {
		clone.Headers[0].Name.S[0] = 'X'
		if msg.Headers[0].Name.S[0] != origHdrNameFirst {
			t.Error("original header name modified by clone change")
		}
	}

	// Modify clone's NewURI
	clone.NewURI = str.Mk("sip:modified@example.com")
	if msg.NewURI.Len != 0 {
		t.Error("original NewURI was not empty before modification")
	}

	// Modify clone's DstURI
	clone.DstURI = str.Mk("sip:dst@example.com")
	if msg.DstURI.Len != 0 {
		t.Error("original DstURI was not empty before modification")
	}
}

// TestCloneVia clones a message with a parsed Via header and verifies the
// ViaBody structure including parameters and shortcut pointers.
func TestCloneVia(t *testing.T) {
	msg, err := ParseMsg(cloneTestInvite)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if msg.Via1 == nil {
		t.Fatal("original Via1 is nil - parser did not parse Via")
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}

	// Via1 must be cloned
	if clone.Via1 == nil {
		t.Fatal("clone Via1 is nil")
	}
	if clone.Via1 == msg.Via1 {
		t.Error("clone Via1 shares pointer with original")
	}

	// Verify via body fields
	if clone.Via1.Name.String() != msg.Via1.Name.String() {
		t.Errorf("via name mismatch: %s != %s",
			clone.Via1.Name.String(), msg.Via1.Name.String())
	}
	if clone.Via1.Version.String() != msg.Via1.Version.String() {
		t.Errorf("via version mismatch")
	}
	if clone.Via1.Transport.String() != msg.Via1.Transport.String() {
		t.Errorf("via transport mismatch: %s != %s",
			clone.Via1.Transport.String(), msg.Via1.Transport.String())
	}
	if clone.Via1.Host.String() != msg.Via1.Host.String() {
		t.Errorf("via host mismatch: %s != %s",
			clone.Via1.Host.String(), msg.Via1.Host.String())
	}
	if clone.Via1.PortStr.String() != msg.Via1.PortStr.String() {
		t.Errorf("via port_str mismatch: %s != %s",
			clone.Via1.PortStr.String(), msg.Via1.PortStr.String())
	}
	if clone.Via1.Proto != msg.Via1.Proto {
		t.Errorf("via proto mismatch")
	}

	// Verify branch parameter
	if msg.Via1.Branch != nil {
		if clone.Via1.Branch == nil {
			t.Fatal("clone Via1.Branch is nil")
		}
		if clone.Via1.Branch == msg.Via1.Branch {
			t.Error("clone Via1.Branch shares pointer with original")
		}
		if clone.Via1.Branch.Value.String() != msg.Via1.Branch.Value.String() {
			t.Errorf("branch value mismatch: %s != %s",
				clone.Via1.Branch.Value.String(),
				msg.Via1.Branch.Value.String())
		}
	}

	// Verify rport parameter
	if msg.Via1.RPort != nil {
		if clone.Via1.RPort == nil {
			t.Fatal("clone Via1.RPort is nil")
		}
		if clone.Via1.RPort == msg.Via1.RPort {
			t.Error("clone Via1.RPort shares pointer with original")
		}
	}

	// Verify parameter list
	if msg.Via1.ParamList != nil {
		if clone.Via1.ParamList == nil {
			t.Fatal("clone Via1.ParamList is nil")
		}
		if clone.Via1.ParamList == msg.Via1.ParamList {
			t.Error("clone Via1.ParamList shares pointer with original")
		}

		// Count params in original and clone
		origCount := 0
		for p := msg.Via1.ParamList; p != nil; p = p.Next {
			origCount++
		}
		cloneCount := 0
		for p := clone.Via1.ParamList; p != nil; p = p.Next {
			cloneCount++
		}
		if cloneCount != origCount {
			t.Errorf("param count mismatch: %d != %d", cloneCount, origCount)
		}

		// Verify LastParam
		if clone.Via1.LastParam == nil {
			t.Error("clone Via1.LastParam is nil")
		} else {
			// LastParam should be the last in the list
			last := clone.Via1.ParamList
			for last.Next != nil {
				last = last.Next
			}
			if clone.Via1.LastParam != last {
				t.Error("clone Via1.LastParam does not match last param in list")
			}
		}
	}

	// Verify that the branch shortcut points into the param list
	if clone.Via1.Branch != nil && clone.Via1.ParamList != nil {
		found := false
		for p := clone.Via1.ParamList; p != nil; p = p.Next {
			if p == clone.Via1.Branch {
				found = true
				break
			}
		}
		if !found {
			t.Error("clone Via1.Branch does not point into cloned ParamList")
		}
	}

	// Verify via body independence
	if clone.Via1.Host.Len > 0 {
		origHostFirst := msg.Via1.Host.S[0]
		clone.Via1.Host.S[0] = 'X'
		if msg.Via1.Host.S[0] != origHostFirst {
			t.Error("original via host modified by clone change")
		}
	}

	// Verify HdrVia1.Parsed points to the same ViaBody as Via1
	if clone.HdrVia1 != nil {
		if pvb, ok := clone.HdrVia1.Parsed.(*ViaBody); ok {
			if pvb != clone.Via1 {
				t.Error("clone HdrVia1.Parsed does not match clone Via1")
			}
		}
	}
}

// TestCloneWithBody clones a message containing a body and verifies the
// body is independently copied.
func TestCloneWithBody(t *testing.T) {
	msg, err := ParseMsg(cloneTestInviteWithBody)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	origBody, ok := msg.Body.([]byte)
	if !ok {
		t.Fatalf("original body is not []byte, got %T", msg.Body)
	}
	if len(origBody) == 0 {
		t.Fatal("original body is empty")
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}

	cloneBody, ok := clone.Body.([]byte)
	if !ok {
		t.Fatalf("clone body is not []byte, got %T", clone.Body)
	}
	if len(cloneBody) != len(origBody) {
		t.Fatalf("body length mismatch: %d != %d", len(cloneBody), len(origBody))
	}
	if string(cloneBody) != string(origBody) {
		t.Errorf("body content mismatch: %q != %q", string(cloneBody), string(origBody))
	}

	// Verify independence
	if &cloneBody[0] == &origBody[0] {
		t.Error("clone body shares memory with original")
	}

	// Modify clone body
	origFirst := origBody[0]
	cloneBody[0] = 'X'
	if origBody[0] != origFirst {
		t.Error("original body modified by clone change")
	}
}

// TestCloneNilFields clones a message with many nil fields and verifies
// no panic occurs and nil fields remain nil.
func TestCloneNilFields(t *testing.T) {
	msg := &SIPMsg{
		ID: 42,
		FirstLine: &MsgStart{
			Type: MsgRequest,
			Req: &RequestLine{
				Method:      str.Mk("INVITE"),
				URI:         str.Mk("sip:test@example.com"),
				Version:     str.Mk("SIP/2.0"),
				MethodValue: MethodInvite,
			},
		},
		// Headers, Via1, Via2, Body, ParsedURI, etc. are all nil
	}

	clone, err := msg.Clone()
	if err != nil {
		t.Fatalf("clone error: %v", err)
	}
	if clone == nil {
		t.Fatal("clone is nil")
	}

	// Verify scalar fields
	if clone.ID != 42 {
		t.Errorf("ID mismatch: %d", clone.ID)
	}

	// Verify first line
	if clone.FirstLine == nil {
		t.Fatal("clone first line is nil")
	}
	if clone.FirstLine.Req == nil {
		t.Fatal("clone request line is nil")
	}
	if clone.FirstLine.Req.Method.String() != "INVITE" {
		t.Errorf("method mismatch: %s", clone.FirstLine.Req.Method.String())
	}
	if clone.FirstLine.Req.URI.String() != "sip:test@example.com" {
		t.Errorf("URI mismatch: %s", clone.FirstLine.Req.URI.String())
	}

	// Verify nil fields remain nil
	if clone.Headers != nil {
		t.Error("clone Headers should be nil")
	}
	if clone.Via1 != nil {
		t.Error("clone Via1 should be nil")
	}
	if clone.Via2 != nil {
		t.Error("clone Via2 should be nil")
	}
	if clone.Body != nil {
		t.Error("clone Body should be nil")
	}
	if clone.ParsedURI != nil {
		t.Error("clone ParsedURI should be nil")
	}
	if clone.ParsedOrigRURI != nil {
		t.Error("clone ParsedOrigRURI should be nil")
	}
	if clone.HdrVia1 != nil {
		t.Error("clone HdrVia1 should be nil")
	}
	if clone.From != nil {
		t.Error("clone From should be nil")
	}
	if clone.To != nil {
		t.Error("clone To should be nil")
	}
	if clone.Buf != nil {
		t.Error("clone Buf should be nil")
	}
	if clone.LastHeader != nil {
		t.Error("clone LastHeader should be nil")
	}

	// Verify independence of first line
	if clone.FirstLine.Req.Method.Len > 0 {
		origFirst := msg.FirstLine.Req.Method.S[0]
		clone.FirstLine.Req.Method.S[0] = 'X'
		if msg.FirstLine.Req.Method.S[0] != origFirst {
			t.Error("original method modified by clone change")
		}
	}
}

// TestCloneConcurrent runs many goroutines that clone the same message
// concurrently and modify their clones, verifying thread safety under the
// race detector.
func TestCloneConcurrent(t *testing.T) {
	msg, err := ParseMsg(cloneTestInvite)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			clone, err := msg.Clone()
			if err != nil {
				t.Errorf("clone error: %v", err)
				return
			}
			if clone == nil {
				t.Error("clone is nil")
				return
			}

			// Modify the clone to verify independence
			if clone.Buf != nil && len(clone.Buf) > 0 {
				clone.Buf[0] = 'X'
			}
			if clone.FirstLine != nil && clone.FirstLine.Req != nil &&
				clone.FirstLine.Req.Method.Len > 0 {
				clone.FirstLine.Req.Method.S[0] = 'X'
			}
			if clone.Headers != nil && len(clone.Headers) > 0 &&
				clone.Headers[0].Body.Len > 0 {
				clone.Headers[0].Body.S[0] = 'X'
			}
			if clone.Via1 != nil && clone.Via1.Host.Len > 0 {
				clone.Via1.Host.S[0] = 'X'
			}
		}()
	}
	wg.Wait()

	// Verify original is unchanged after all concurrent clones
	if msg.FirstLine == nil || msg.FirstLine.Req == nil {
		t.Fatal("original first line is nil after concurrent clones")
	}
	if msg.FirstLine.Req.Method.Len > 0 && msg.FirstLine.Req.Method.S[0] != 'I' {
		t.Error("original method was modified by concurrent clones")
	}
	if msg.Buf != nil && len(msg.Buf) > 0 && msg.Buf[0] != 'I' {
		t.Error("original buffer was modified by concurrent clones")
	}
	if msg.Via1 != nil && msg.Via1.Host.Len > 0 && msg.Via1.Host.S[0] != 'p' {
		t.Error("original via host was modified by concurrent clones")
	}
}
