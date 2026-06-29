// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * WebSocket handshake layer (RFC 6455 §1.3, §4; RFC 7118).
 * Port of the kamailio ws_handshake.c — Sec-WebSocket-Accept computation,
 * the server-side opening handshake handler and helpers for the client-side
 * outbound handshake.
 *
 * The handshake is decoupled from the I/O layer: server-side callers pass
 * an http.ResponseWriter / http.Request and receive a hijacked net.Conn
 * plus the negotiated subprotocol; client-side callers build the request
 * bytes and parse the response bytes.
 */

package websocket

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ---------------------------------------------------------------------------
// Sec-WebSocket-Accept (RFC 6455 §1.3)
// ---------------------------------------------------------------------------

// AcceptGUID is the magic GUID appended to the client's
// Sec-WebSocket-Key when computing Sec-WebSocket-Accept (RFC 6455 §1.3).
const AcceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ComputeAccept computes the value of the Sec-WebSocket-Accept response
// header from the client's Sec-WebSocket-Key (RFC 6455 §1.3):
//
//	accept = base64(sha1(key + AcceptGUID))
//
//	C: ws_compute_accept()
func ComputeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write([]byte(AcceptGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// GenerateKey returns a freshly generated Sec-WebSocket-Key value: 16
// random bytes encoded with base64 (RFC 6455 §4.1, §1.3).
//
//	C: ws_connect() — Sec-WebSocket-Key generation
func GenerateKey() (string, error) {
	var buf [16]byte
	if _, err := readRandom(buf[:]); err != nil {
		return "", fmt.Errorf("websocket: generate key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf[:]), nil
}

// ---------------------------------------------------------------------------
// Subprotocol negotiation (RFC 6455 §1.3; RFC 7118 §3)
// ---------------------------------------------------------------------------

// DefaultSubprotocols is the list of subprotocols offered by default
// (RFC 7118 §3 defines "sip"; RFC 7692bis defines "msrp").
var DefaultSubprotocols = []string{"sip", "msrp"}

// SelectSubprotocol returns the first subprotocol from the client's
// offered list that the server also supports. Returns "" when no common
// subprotocol exists.
//
//	C: ws_parse_sub_protocol()
func SelectSubprotocol(offered, supported []string) string {
	if len(supported) == 0 {
		supported = DefaultSubprotocols
	}
	for _, want := range offered {
		for _, have := range supported {
			if strings.EqualFold(want, have) {
				return have
			}
		}
	}
	return ""
}

// ParseSubprotocolList splits a comma-separated Sec-WebSocket-Protocol
// header value into individual subprotocols, trimming whitespace.
//
//	C: ws_parse_sub_protocol() — header parsing
func ParseSubprotocolList(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// CORS mode (mirrors C ws_cors_mode)
// ---------------------------------------------------------------------------

// CorsMode controls how (or whether) the Access-Control-Allow-Origin
// header is sent in the handshake response.
type CorsMode int

const (
	// CorsNone disables CORS (no Access-Control-Allow-Origin header).
	CorsNone CorsMode = iota
	// CorsAny sends "Access-Control-Allow-Origin: *".
	CorsAny
	// CorsEchoOrigin echoes the request's Origin header verbatim.
	CorsEchoOrigin
)

// String returns a human-readable CORS mode name.
func (m CorsMode) String() string {
	switch m {
	case CorsNone:
		return "none"
	case CorsAny:
		return "any"
	case CorsEchoOrigin:
		return "origin"
	default:
		return fmt.Sprintf("cors(%d)", int(m))
	}
}

// ---------------------------------------------------------------------------
// Origin validation
// ---------------------------------------------------------------------------

// OriginAllowed reports whether the given Origin header value is allowed
// by the supplied allow-list. An empty allow-list allows all origins.
//
//	C: ws_handle_handshake() — Origin check
func OriginAllowed(allowed []string, origin string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Handshake result (server side)
// ---------------------------------------------------------------------------

// HandshakeResult is returned by HandleHandshake on success. It contains
// the hijacked connection, the negotiated subprotocol (or "") and the
// originating remote address.
type HandshakeResult struct {
	Conn        net.Conn
	Subprotocol string
	Origin      string
	Key         string
	RemoteAddr  string
}

// ---------------------------------------------------------------------------
// Handshake errors
// ---------------------------------------------------------------------------

var (
	// ErrBadRequest indicates a malformed handshake request.
	ErrBadRequest = errors.New("websocket: bad request")
	// ErrUpgradeRequired indicates the request was valid but did not
	// carry the required Upgrade / Connection headers.
	ErrUpgradeRequired = errors.New("websocket: upgrade required")
	// ErrUnsupportedVersion indicates Sec-WebSocket-Version was not 13.
	ErrUnsupportedVersion = errors.New("websocket: unsupported version")
	// ErrOriginDenied indicates the Origin was rejected by the allow-list.
	ErrOriginDenied = errors.New("websocket: origin denied")
	// ErrNoSubprotocol indicates no common subprotocol was negotiated.
	ErrNoSubprotocol = errors.New("websocket: no subprotocol")
	// ErrHijackUnsupported indicates the http.ResponseWriter does not
	// support connection hijacking.
	ErrHijackUnsupported = errors.New("websocket: hijack unsupported")
)

// ---------------------------------------------------------------------------
// Server-side handshake (RFC 6455 §4.1; RFC 7118 §3)
// ---------------------------------------------------------------------------

// HandshakeConfig carries the parameters that govern a single server-side
// handshake. A zero-value HandshakeConfig negotiates "sip" by default and
// performs no Origin check.
type HandshakeConfig struct {
	// Subprotocols lists the subprotocols the server is willing to
	// negotiate. Defaults to DefaultSubprotocols when empty.
	Subprotocols []string
	// Origins is the optional Origin allow-list (empty = allow all).
	Origins []string
	// Cors controls the Access-Control-Allow-Origin response header.
	Cors CorsMode
	// RequireSubprotocol, when true, rejects handshakes that do not
	// negotiate a subprotocol (RFC 7118 §3 REQUIRES "sip").
	RequireSubprotocol bool
}

// HandleHandshake performs the RFC 6455 §4.1 server-side handshake on the
// supplied request/response pair. On success the underlying TCP
// connection is hijacked and returned; the caller owns its lifetime from
// that point onward. On failure the response has already been written with
// the appropriate HTTP error code (400 / 426 / 403).
//
//	C: ws_handle_handshake()
func HandleHandshake(w http.ResponseWriter, r *http.Request, cfg HandshakeConfig) (*HandshakeResult, error) {
	// Method must be GET (RFC 6455 §4.1).
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, fmt.Errorf("%w: method %s", ErrBadRequest, r.Method)
	}
	// Upgrade header must contain "websocket".
	if !headerContains(r.Header, "Upgrade", "websocket") {
		http.Error(w, "Upgrade header missing or invalid", http.StatusBadRequest)
		return nil, fmt.Errorf("%w: Upgrade header", ErrBadRequest)
	}
	// Connection header must contain "Upgrade".
	if !headerContains(r.Header, "Connection", "upgrade") {
		http.Error(w, "Connection header missing Upgrade", http.StatusBadRequest)
		return nil, fmt.Errorf("%w: Connection header", ErrBadRequest)
	}
	// Sec-WebSocket-Version must be 13.
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "Unsupported WebSocket version", http.StatusUpgradeRequired)
		return nil, fmt.Errorf("%w: version %q", ErrUnsupportedVersion, r.Header.Get("Sec-WebSocket-Version"))
	}
	// Sec-WebSocket-Key is mandatory and must be present.
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "Sec-WebSocket-Key missing", http.StatusBadRequest)
		return nil, fmt.Errorf("%w: missing Sec-WebSocket-Key", ErrBadRequest)
	}
	// Origin allow-list (RFC 6455 §10.2).
	origin := r.Header.Get("Origin")
	if !OriginAllowed(cfg.Origins, origin) {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return nil, fmt.Errorf("%w: %q", ErrOriginDenied, origin)
	}
	// Subprotocol negotiation.
	supported := cfg.Subprotocols
	if len(supported) == 0 {
		supported = DefaultSubprotocols
	}
	sub := SelectSubprotocol(ParseSubprotocolList(r.Header.Get("Sec-WebSocket-Protocol")), supported)
	if sub == "" && cfg.RequireSubprotocol {
		http.Error(w, "No supported subprotocol", http.StatusBadRequest)
		return nil, fmt.Errorf("%w: client offered %q", ErrNoSubprotocol, r.Header.Get("Sec-WebSocket-Protocol"))
	}

	// Hijack the connection (RFC 6455 §4.1 step 4 — switch protocols).
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijack unsupported", http.StatusInternalServerError)
		return nil, ErrHijackUnsupported
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, "Hijack failed", http.StatusInternalServerError)
		return nil, fmt.Errorf("websocket: hijack: %w", err)
	}

	// Build the 101 Switching Protocols response (RFC 6455 §1.3, §4.2.2).
	resp := http.Response{
		ProtoMajor: 1,
		ProtoMinor: 1,
		Status:     "101 Switching Protocols",
		StatusCode: 101,
		Header:     make(http.Header),
	}
	resp.Header.Set("Upgrade", "websocket")
	resp.Header.Set("Connection", "Upgrade")
	resp.Header.Set("Sec-WebSocket-Accept", ComputeAccept(key))
	if sub != "" {
		resp.Header.Set("Sec-WebSocket-Protocol", sub)
	}
	// CORS (RFC 6455 §10.8 / W3C WebSocket API).
	switch cfg.Cors {
	case CorsAny:
		resp.Header.Set("Access-Control-Allow-Origin", "*")
	case CorsEchoOrigin:
		if origin != "" {
			resp.Header.Set("Access-Control-Allow-Origin", origin)
		}
	}
	if err := resp.Write(bufrw); err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket: write 101: %w", err)
	}
	if err := bufrw.Flush(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket: flush 101: %w", err)
	}

	return &HandshakeResult{
		Conn:        conn,
		Subprotocol: sub,
		Origin:      origin,
		Key:         key,
		RemoteAddr:  r.RemoteAddr,
	}, nil
}

// headerContains reports whether the named header value contains the
// supplied token (case-insensitive, comma-separated list semantics).
func headerContains(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Client-side handshake (RFC 6455 §4.1 client requirements)
// ---------------------------------------------------------------------------

// ClientHandshakeRequest builds the HTTP request bytes for a client-side
// outbound WebSocket handshake to the given URL. The caller writes the
// returned bytes to the underlying TCP connection and reads back the
// 101 response, validating it with ValidateServerResponse.
//
//	C: ws_connect()
func ClientHandshakeRequest(url, host, path, subprotocol string) ([]byte, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&b, "Host: %s\r\n", host)
	b.WriteString("Upgrade: websocket\r\n")
	b.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&b, "Sec-WebSocket-Key: %s\r\n", key)
	b.WriteString("Sec-WebSocket-Version: 13\r\n")
	if subprotocol != "" {
		fmt.Fprintf(&b, "Sec-WebSocket-Protocol: %s\r\n", subprotocol)
	}
	b.WriteString("\r\n")
	return []byte(b.String()), nil
}

// ValidateServerResponse validates a server's 101 response against the
// client's previously-sent Sec-WebSocket-Key. It returns the negotiated
// subprotocol (or "") on success.
//
//	C: ws_handle_handshake_response()
func ValidateServerResponse(resp *http.Response, clientKey string) (string, error) {
	if resp.StatusCode != 101 {
		return "", fmt.Errorf("websocket: expected 101, got %d", resp.StatusCode)
	}
	if !headerContains(resp.Header, "Upgrade", "websocket") {
		return "", fmt.Errorf("websocket: server Upgrade header missing")
	}
	if !headerContains(resp.Header, "Connection", "upgrade") {
		return "", fmt.Errorf("websocket: server Connection header missing Upgrade")
	}
	got := resp.Header.Get("Sec-WebSocket-Accept")
	if want := ComputeAccept(clientKey); got != want {
		return "", fmt.Errorf("websocket: Sec-WebSocket-Accept mismatch (got %q, want %q)", got, want)
	}
	return resp.Header.Get("Sec-WebSocket-Protocol"), nil
}

// ---------------------------------------------------------------------------
// readRandom — kept here to keep ws_frame.go free of crypto/rand import.
// ---------------------------------------------------------------------------

// readRandom fills b with cryptographically secure random bytes.
// It is a thin wrapper around crypto/rand so callers in this package do
// not need to import it directly.
func readRandom(b []byte) (int, error) {
	return rand.Read(b)
}

// readerFromConn returns a *bufio.Reader backed by the supplied net.Conn.
// Exposed so package callers can build the buffered reader expected by
// DecodeFrame when reading directly from a hijacked connection.
func readerFromConn(c net.Conn) *bufio.Reader {
	return bufio.NewReader(c)
}
