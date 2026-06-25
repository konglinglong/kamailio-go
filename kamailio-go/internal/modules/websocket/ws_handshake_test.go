// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - WebSocket handshake layer tests (RFC 6455 §1.3, §4).
 */

package websocket

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ComputeAccept (RFC 6455 §1.3 — RFC 4231 test vector)
// ---------------------------------------------------------------------------

func TestComputeAccept_RFCVector(t *testing.T) {
	// RFC 6455 §4.2.2 example: input key "dGhlIHNhbXBsZSBub25jZQ=="
	// yields accept "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=".
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := ComputeAccept(key); got != want {
		t.Errorf("ComputeAccept(%q) = %q, want %q", key, got, want)
	}
}

func TestComputeAccept_EmptyKey(t *testing.T) {
	// Even an empty key should produce a stable hash.
	got := ComputeAccept("")
	if got == "" {
		t.Error("expected non-empty accept for empty key")
	}
	// Verify determinism.
	if got2 := ComputeAccept(""); got2 != got {
		t.Errorf("ComputeAccept not deterministic: %q vs %q", got, got2)
	}
}

// ---------------------------------------------------------------------------
// GenerateKey
// ---------------------------------------------------------------------------

func TestGenerateKey(t *testing.T) {
	k1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if k1 == "" {
		t.Fatal("expected non-empty key")
	}
	// Keys must be unique.
	k2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey again: %v", err)
	}
	if k1 == k2 {
		t.Error("expected unique keys")
	}
	// A valid key is 16 random bytes → base64 → 24 chars.
	if len(k1) != 24 {
		t.Errorf("key length = %d, want 24", len(k1))
	}
}

func TestGenerateKey_RoundTripWithAccept(t *testing.T) {
	// Generate a key and verify ComputeAccept accepts it.
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	accept := ComputeAccept(key)
	if accept == "" {
		t.Fatal("expected non-empty accept")
	}
	// Verify the accept value is base64 of 20 bytes (SHA-1 digest length).
	if len(accept) != 28 {
		t.Errorf("accept length = %d, want 28", len(accept))
	}
}

// ---------------------------------------------------------------------------
// Subprotocol negotiation
// ---------------------------------------------------------------------------

func TestSelectSubprotocol_SIPChosen(t *testing.T) {
	got := SelectSubprotocol([]string{"sip"}, DefaultSubprotocols)
	if got != "sip" {
		t.Errorf("SelectSubprotocol = %q, want sip", got)
	}
}

func TestSelectSubprotocol_MSRP(t *testing.T) {
	got := SelectSubprotocol([]string{"msrp"}, DefaultSubprotocols)
	if got != "msrp" {
		t.Errorf("SelectSubprotocol = %q, want msrp", got)
	}
}

func TestSelectSubprotocol_NoCommon(t *testing.T) {
	got := SelectSubprotocol([]string{"foo", "bar"}, DefaultSubprotocols)
	if got != "" {
		t.Errorf("SelectSubprotocol = %q, want empty", got)
	}
}

func TestSelectSubprotocol_FirstWins(t *testing.T) {
	// When both subprotocols are offered, the first one in the client list wins.
	got := SelectSubprotocol([]string{"sip", "msrp"}, DefaultSubprotocols)
	if got != "sip" {
		t.Errorf("SelectSubprotocol = %q, want sip", got)
	}
}

func TestSelectSubprotocol_CaseInsensitive(t *testing.T) {
	got := SelectSubprotocol([]string{"SIP", "MSRP"}, DefaultSubprotocols)
	if got != "sip" {
		t.Errorf("SelectSubprotocol = %q, want sip (case-insensitive)", got)
	}
}

func TestSelectSubprotocol_CustomServerList(t *testing.T) {
	got := SelectSubprotocol([]string{"custom"}, []string{"custom"})
	if got != "custom" {
		t.Errorf("SelectSubprotocol = %q, want custom", got)
	}
}

func TestParseSubprotocolList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"sip", []string{"sip"}},
		{"sip, msrp", []string{"sip", "msrp"}},
		{"sip,msrp", []string{"sip", "msrp"}},
		{" sip , msrp ", []string{"sip", "msrp"}},
		{",sip,", []string{"sip"}},
	}
	for _, c := range cases {
		got := ParseSubprotocolList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ParseSubprotocolList(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseSubprotocolList(%q)[%d] = %q, want %q",
					c.in, i, got[i], c.want[i])
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CORS mode
// ---------------------------------------------------------------------------

func TestCorsMode_String(t *testing.T) {
	cases := []struct {
		mode CorsMode
		want string
	}{
		{CorsNone, "none"},
		{CorsAny, "any"},
		{CorsEchoOrigin, "origin"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("%d.String() = %q, want %q", c.mode, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Origin validation
// ---------------------------------------------------------------------------

func TestOriginAllowed_EmptyAllowList(t *testing.T) {
	if !OriginAllowed(nil, "https://example.com") {
		t.Error("empty allow-list should allow all origins")
	}
	if !OriginAllowed([]string{}, "https://example.com") {
		t.Error("empty allow-list should allow all origins")
	}
}

func TestOriginAllowed_Match(t *testing.T) {
	if !OriginAllowed([]string{"https://example.com"}, "https://example.com") {
		t.Error("expected match")
	}
}

func TestOriginAllowed_NoMatch(t *testing.T) {
	if OriginAllowed([]string{"https://example.com"}, "https://evil.com") {
		t.Error("expected no match")
	}
}

func TestOriginAllowed_CaseInsensitive(t *testing.T) {
	if !OriginAllowed([]string{"https://EXAMPLE.com"}, "https://example.com") {
		t.Error("expected case-insensitive match")
	}
}

// ---------------------------------------------------------------------------
// HandleHandshake error paths — these do not reach Hijack and can be
// tested with httptest.NewRecorder.
// ---------------------------------------------------------------------------

func newHandshakeRequest(t *testing.T, key, subprotocol, origin string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	if key != "" {
		req.Header.Set("Sec-WebSocket-Key", key)
	}
	if subprotocol != "" {
		req.Header.Set("Sec-WebSocket-Protocol", subprotocol)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func TestHandleHandshake_BadMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ws", nil)
	rr := httptest.NewRecorder()
	_, err := HandleHandshake(rr, req, HandshakeConfig{})
	if err == nil || !errors.Is(err, ErrBadRequest) {
		t.Errorf("expected ErrBadRequest, got %v", err)
	}
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleHandshake_MissingUpgradeHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Sec-WebSocket-Version", "13")
	rr := httptest.NewRecorder()
	_, err := HandleHandshake(rr, req, HandshakeConfig{})
	if err == nil || !errors.Is(err, ErrBadRequest) {
		t.Errorf("expected ErrBadRequest, got %v", err)
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleHandshake_BadVersion(t *testing.T) {
	req := newHandshakeRequest(t, "key", "", "")
	req.Header.Set("Sec-WebSocket-Version", "8")
	rr := httptest.NewRecorder()
	_, err := HandleHandshake(rr, req, HandshakeConfig{})
	if err == nil || !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got %v", err)
	}
	if rr.Code != http.StatusUpgradeRequired {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUpgradeRequired)
	}
	if got := rr.Header().Get("Sec-WebSocket-Version"); got != "13" {
		t.Errorf("response Sec-WebSocket-Version = %q, want 13", got)
	}
}

func TestHandleHandshake_MissingKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	rr := httptest.NewRecorder()
	_, err := HandleHandshake(rr, req, HandshakeConfig{})
	if err == nil || !errors.Is(err, ErrBadRequest) {
		t.Errorf("expected ErrBadRequest, got %v", err)
	}
}

func TestHandleHandshake_OriginDenied(t *testing.T) {
	req := newHandshakeRequest(t, "key", "sip", "https://evil.com")
	rr := httptest.NewRecorder()
	_, err := HandleHandshake(rr, req, HandshakeConfig{
		Origins: []string{"https://example.com"},
	})
	if err == nil || !errors.Is(err, ErrOriginDenied) {
		t.Errorf("expected ErrOriginDenied, got %v", err)
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestHandleHandshake_RequireSubprotocol_NoOffer(t *testing.T) {
	req := newHandshakeRequest(t, "key", "", "")
	rr := httptest.NewRecorder()
	_, err := HandleHandshake(rr, req, HandshakeConfig{RequireSubprotocol: true})
	if err == nil || !errors.Is(err, ErrNoSubprotocol) {
		t.Errorf("expected ErrNoSubprotocol, got %v", err)
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// HandleHandshake success path — needs a real TCP server because the
// response must be hijacked. We use httptest.NewServer and a raw TCP
// client to validate the response.
// ---------------------------------------------------------------------------

// startHandshakeServer starts an httptest server that delegates to
// HandleHandshake, and returns (server, cleanup). The caller is
// responsible for invoking cleanup.
func startHandshakeServer(t *testing.T, cfg HandshakeConfig) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := HandleHandshake(w, r, cfg)
		if err != nil {
			// The error response has already been written by HandleHandshake.
			return
		}
		// On success, HandleHandshake has hijacked the connection.
		// We must not write to w any more — the test owns the conn now.
		// Keep the goroutine alive briefly so the connection isn't closed
		// by the server framework before the client reads the response.
		select {}
	}))
	return srv, func() { srv.Close() }
}

// dialHandshake opens a TCP connection to the test server, sends a
// raw HTTP Upgrade request, and returns the bufio.Reader over the
// response.
func dialHandshake(t *testing.T, srv *httptest.Server, key, subproto, origin string) (*bufio.Reader, net.Conn) {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var req strings.Builder
	req.WriteString("GET /ws HTTP/1.1\r\n")
	req.WriteString("Host: " + srv.Listener.Addr().String() + "\r\n")
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	if key != "" {
		req.WriteString("Sec-WebSocket-Key: " + key + "\r\n")
	}
	if subproto != "" {
		req.WriteString("Sec-WebSocket-Protocol: " + subproto + "\r\n")
	}
	if origin != "" {
		req.WriteString("Origin: " + origin + "\r\n")
	}
	req.WriteString("\r\n")
	if _, err := conn.Write([]byte(req.String())); err != nil {
		conn.Close()
		t.Fatalf("write request: %v", err)
	}
	return bufio.NewReader(conn), conn
}

func TestHandleHandshake_Success(t *testing.T) {
	srv, cleanup := startHandshakeServer(t, HandshakeConfig{})
	defer cleanup()
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	r, conn := dialHandshake(t, srv, key, "sip", "")
	defer conn.Close()
	// Read the HTTP response status line and headers.
	statusLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Errorf("status = %q, want 101", statusLine)
	}
	// Read headers and look for the required ones.
	headers := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		headers[strings.ToLower(name)] = val
	}
	if headers["upgrade"] != "websocket" {
		t.Errorf("Upgrade = %q, want websocket", headers["upgrade"])
	}
	if headers["connection"] != "Upgrade" {
		t.Errorf("Connection = %q, want Upgrade", headers["connection"])
	}
	if want := ComputeAccept(key); headers["sec-websocket-accept"] != want {
		t.Errorf("Accept = %q, want %q", headers["sec-websocket-accept"], want)
	}
	if headers["sec-websocket-protocol"] != "sip" {
		t.Errorf("Protocol = %q, want sip", headers["sec-websocket-protocol"])
	}
}

func TestHandleHandshake_CorsAny(t *testing.T) {
	srv, cleanup := startHandshakeServer(t, HandshakeConfig{Cors: CorsAny})
	defer cleanup()
	r, conn := dialHandshake(t, srv, "dGhlIHNhbXBsZSBub25jZQ==", "sip", "https://example.com")
	defer conn.Close()
	// Drain the response.
	for i := 0; i < 50; i++ {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.HasPrefix(strings.ToLower(line), "access-control-allow-origin:") {
			val := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			val = strings.TrimRight(val, "\r\n")
			if val != "*" {
				t.Errorf("CORS = %q, want *", val)
			}
			return
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	t.Error("Access-Control-Allow-Origin header not found")
}

func TestHandleHandshake_CorsEchoOrigin(t *testing.T) {
	srv, cleanup := startHandshakeServer(t, HandshakeConfig{Cors: CorsEchoOrigin})
	defer cleanup()
	r, conn := dialHandshake(t, srv, "dGhlIHNhbXBsZSBub25jZQ==", "sip", "https://example.com")
	defer conn.Close()
	for i := 0; i < 50; i++ {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.HasPrefix(strings.ToLower(line), "access-control-allow-origin:") {
			val := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			val = strings.TrimRight(val, "\r\n")
			if val != "https://example.com" {
				t.Errorf("CORS = %q, want https://example.com", val)
			}
			return
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}
	t.Error("Access-Control-Allow-Origin header not found")
}

func TestHandleHandshake_OriginAllowed(t *testing.T) {
	srv, cleanup := startHandshakeServer(t, HandshakeConfig{
		Origins: []string{"https://example.com"},
	})
	defer cleanup()
	r, conn := dialHandshake(t, srv, "dGhlIHNhbXBsZSBub25jZQ==", "sip", "https://example.com")
	defer conn.Close()
	statusLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Errorf("status = %q, want 101 (origin allowed)", statusLine)
	}
}

// ---------------------------------------------------------------------------
// Client-side handshake
// ---------------------------------------------------------------------------

func TestClientHandshakeRequest(t *testing.T) {
	bytes, err := ClientHandshakeRequest("ws://example.com:80/ws", "example.com:80", "/ws", "sip")
	if err != nil {
		t.Fatalf("ClientHandshakeRequest: %v", err)
	}
	s := string(bytes)
	if !strings.HasPrefix(s, "GET /ws HTTP/1.1\r\n") {
		t.Errorf("missing request line: %q", s)
	}
	if !strings.Contains(s, "Host: example.com:80\r\n") {
		t.Errorf("missing Host header: %q", s)
	}
	if !strings.Contains(s, "Upgrade: websocket\r\n") {
		t.Errorf("missing Upgrade header: %q", s)
	}
	if !strings.Contains(s, "Sec-WebSocket-Version: 13\r\n") {
		t.Errorf("missing Version header: %q", s)
	}
	if !strings.Contains(s, "Sec-WebSocket-Protocol: sip\r\n") {
		t.Errorf("missing Protocol header: %q", s)
	}
	// Verify the Sec-WebSocket-Key is present and non-empty.
	if !strings.Contains(s, "Sec-WebSocket-Key: ") {
		t.Errorf("missing Key header: %q", s)
	}
}

func TestClientHandshakeRequest_NoSubprotocol(t *testing.T) {
	bytes, err := ClientHandshakeRequest("ws://example.com/ws", "example.com", "/ws", "")
	if err != nil {
		t.Fatalf("ClientHandshakeRequest: %v", err)
	}
	s := string(bytes)
	if strings.Contains(s, "Sec-WebSocket-Protocol:") {
		t.Errorf("should not include Protocol header when none requested")
	}
}

func TestValidateServerResponse_Success(t *testing.T) {
	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	resp := &http.Response{
		StatusCode: 101,
		Header:     make(http.Header),
	}
	resp.Header.Set("Upgrade", "websocket")
	resp.Header.Set("Connection", "Upgrade")
	resp.Header.Set("Sec-WebSocket-Accept", ComputeAccept(clientKey))
	resp.Header.Set("Sec-WebSocket-Protocol", "sip")
	sub, err := ValidateServerResponse(resp, clientKey)
	if err != nil {
		t.Fatalf("ValidateServerResponse: %v", err)
	}
	if sub != "sip" {
		t.Errorf("subprotocol = %q, want sip", sub)
	}
}

func TestValidateServerResponse_BadStatus(t *testing.T) {
	resp := &http.Response{
		StatusCode: 400,
		Header:     make(http.Header),
	}
	_, err := ValidateServerResponse(resp, "key")
	if err == nil {
		t.Fatal("expected error for bad status")
	}
}

func TestValidateServerResponse_AcceptMismatch(t *testing.T) {
	resp := &http.Response{
		StatusCode: 101,
		Header:     make(http.Header),
	}
	resp.Header.Set("Upgrade", "websocket")
	resp.Header.Set("Connection", "Upgrade")
	resp.Header.Set("Sec-WebSocket-Accept", "wrong-accept-value-here=")
	_, err := ValidateServerResponse(resp, "key")
	if err == nil {
		t.Fatal("expected error for accept mismatch")
	}
}

func TestValidateServerResponse_MissingUpgrade(t *testing.T) {
	resp := &http.Response{
		StatusCode: 101,
		Header:     make(http.Header),
	}
	resp.Header.Set("Sec-WebSocket-Accept", ComputeAccept("key"))
	_, err := ValidateServerResponse(resp, "key")
	if err == nil {
		t.Fatal("expected error for missing Upgrade header")
	}
}
