// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * TextOpsX module tests.
 */
package textopsx

import (
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// buildMsg assembles a SIP request with the given headers and body, setting
// Content-Length correctly so the parser can extract the body.
func buildMsg(headers, body string) []byte {
	hdr := headers
	if !strings.HasSuffix(hdr, "\r\n") {
		hdr += "\r\n"
	}
	hdr += "Content-Length: " + itoa(len(body)) + "\r\n"
	return []byte("INVITE sip:bob@example.com SIP/2.0\r\n" + hdr + "\r\n" + body)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func mustParseMsg(t *testing.T, raw []byte) *parser.SIPMsg {
	t.Helper()
	msg, err := parser.ParseMsg(raw)
	if err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	return msg
}

const sdpAudioBody = "v=0\r\n" +
	"o=- 0 0 IN IP4 10.0.0.1\r\n" +
	"s=-\r\n" +
	"c=IN IP4 10.0.0.1\r\n" +
	"t=0 0\r\n" +
	"m=audio 5004 RTP/AVP 0\r\n" +
	"a=rtpmap:0 PCMU/8000\r\n"

const sdpVideoBody = "v=0\r\n" +
	"o=- 0 0 IN IP4 10.0.0.1\r\n" +
	"s=-\r\n" +
	"c=IN IP4 10.0.0.1\r\n" +
	"t=0 0\r\n" +
	"m=video 5006 RTP/AVP 31\r\n"

func TestSearchReAndSubstRe(t *testing.T) {
	m := NewTextOpsXModule()
	msg := mustParseMsg(t, buildMsg("Content-Type: application/sdp\r\n", sdpAudioBody))

	if !m.SearchRe(msg, "m=audio") {
		t.Error("expected SearchRe to find m=audio")
	}
	if m.SearchRe(msg, "m=video") {
		t.Error("expected SearchRe not to find m=video")
	}
	// Invalid pattern never matches.
	if m.SearchRe(msg, "(") {
		t.Error("expected invalid pattern to not match")
	}

	count := m.SubstRe(msg, "PCMU/8000", "PCMA/8000")
	if count != 1 {
		t.Errorf("SubstRe count = %d, want 1", count)
	}
	if m.SearchRe(msg, "PCMU/8000") {
		t.Error("expected PCMU/8000 replaced")
	}
	if !m.SearchRe(msg, "PCMA/8000") {
		t.Error("expected PCMA/8000 present after substitution")
	}
	// No match yields 0.
	if got := m.SubstRe(msg, "nope", "x"); got != 0 {
		t.Errorf("SubstRe no-match count = %d, want 0", got)
	}
}

func TestIsAudioVideoApplication(t *testing.T) {
	m := NewTextOpsXModule()

	audioMsg := mustParseMsg(t, buildMsg("Content-Type: application/sdp\r\n", sdpAudioBody))
	if !m.IsAudio(audioMsg) {
		t.Error("expected IsAudio for SDP with m=audio")
	}
	if m.IsVideo(audioMsg) {
		t.Error("expected not IsVideo for audio-only SDP")
	}
	if !m.IsApplication(audioMsg) {
		t.Error("expected IsApplication for application/sdp")
	}

	videoMsg := mustParseMsg(t, buildMsg("Content-Type: application/sdp\r\n", sdpVideoBody))
	if !m.IsVideo(videoMsg) {
		t.Error("expected IsVideo for SDP with m=video")
	}
	if m.IsAudio(videoMsg) {
		t.Error("expected not IsAudio for video-only SDP")
	}

	// Non-SDP content types resolved by prefix.
	rawAudio := mustParseMsg(t, buildMsg("Content-Type: audio/mpeg\r\n", "rawbytes"))
	if !m.IsAudio(rawAudio) {
		t.Error("expected IsAudio for audio/mpeg")
	}
	rawVideo := mustParseMsg(t, buildMsg("Content-Type: video/mp4\r\n", "rawbytes"))
	if !m.IsVideo(rawVideo) {
		t.Error("expected IsVideo for video/mp4")
	}
	if m.IsApplication(rawVideo) {
		t.Error("expected not IsApplication for video/mp4")
	}
}

func TestNilAndEmptyMessage(t *testing.T) {
	m := NewTextOpsXModule()
	if m.SearchRe(nil, "x") {
		t.Error("SearchRe(nil) should be false")
	}
	if m.SubstRe(nil, "x", "y") != 0 {
		t.Error("SubstRe(nil) should be 0")
	}
	if m.IsAudio(nil) || m.IsVideo(nil) || m.IsApplication(nil) {
		t.Error("IsAudio/Video/Application(nil) should be false")
	}
	// Message with no Content-Type and no body.
	plain := mustParseMsg(t, buildMsg("", ""))
	if m.IsAudio(plain) || m.IsVideo(plain) || m.IsApplication(plain) {
		t.Error("expected all false for plain message with no content-type")
	}
}
