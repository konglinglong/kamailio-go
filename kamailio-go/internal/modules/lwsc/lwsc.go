// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * lwsc module - lightweight WebSocket client.
 * Port of the kamailio lwsc module (src/modules/lwsc).
 *
 * The lwsc module lets the Kamailio script act as a WebSocket client:
 * connect to a server, exchange text/binary frames, and receive messages
 * through a handler callback. This Go counterpart implements the RFC 6455
 * client handshake and framing on top of net, and exposes a dialer hook so
 * the connection can be replaced by a mock in tests.
 *
 * It is safe for concurrent use.
 */

package lwsc

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// WebSocket message opcodes (RFC 6455).
const (
	TextMessage   = 1
	BinaryMessage = 2
	CloseMessage  = 8
	PingMessage   = 9
	PongMessage   = 10
)

// wsGUID is the magic GUID appended to the client key to compute the
// Sec-WebSocket-Accept value (RFC 6455 §1.3).
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// LWSCConfig holds the configuration for an LWSCModule, mirroring the
// modparams of the C lwsc module (ws_url, ws_origin, ws_timeout).
type LWSCConfig struct {
	URL     string
	Origin  string
	Headers map[string]string
	Timeout time.Duration
}

// LWSCMessage is a single WebSocket frame exchanged with the server.
// Type is 1 for text and 2 for binary (RFC 6455 opcodes).
type LWSCMessage struct {
	Type int
	Data []byte
}

// wsConn is the transport interface used by LWSCModule. It is satisfied by
// the built-in netConn dialer and by test mocks.
type wsConn interface {
	WriteMessage(msgType int, data []byte) error
	ReadMessage() (int, []byte, error)
	Close() error
}

// LWSCModule is a lightweight WebSocket client. It is the Go counterpart
// of the C lwsc module.
type LWSCModule struct {
	mu        sync.RWMutex
	cfg       *LWSCConfig
	conn      wsConn
	connected bool
	handler   func(*LWSCMessage)
	dialer    func(*LWSCConfig) (wsConn, error)
}

// New creates an LWSCModule using the default (real) dialer.
func New() *LWSCModule {
	return &LWSCModule{dialer: defaultDialer}
}

// Init configures the module from cfg. A nil cfg applies empty defaults.
//
//	C: mod_init()
func (m *LWSCModule) Init(cfg *LWSCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &LWSCConfig{}
	}
	m.cfg = cfg
	return nil
}

// Connect opens a WebSocket connection to the configured URL. Any
// previously open connection is closed first.
//
//	C: ws_connect()
func (m *LWSCModule) Connect() error {
	m.mu.Lock()
	dialer := m.dialer
	if dialer == nil {
		dialer = defaultDialer
	}
	cfg := m.cfg
	oldConn := m.conn
	m.conn = nil
	m.connected = false
	m.mu.Unlock()

	if oldConn != nil {
		oldConn.Close()
	}
	if cfg == nil {
		cfg = &LWSCConfig{}
	}

	conn, err := dialer(cfg)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.conn = conn
	m.connected = true
	m.mu.Unlock()
	return nil
}

// Send transmits a single WebSocket message. It is an error to call Send
// while not connected.
//
//	C: ws_send()
func (m *LWSCModule) Send(message *LWSCMessage) error {
	if message == nil {
		return errors.New("lwsc: nil message")
	}
	m.mu.RLock()
	conn := m.conn
	connected := m.connected
	m.mu.RUnlock()
	if !connected || conn == nil {
		return errors.New("lwsc: not connected")
	}
	return conn.WriteMessage(message.Type, message.Data)
}

// Receive reads a single WebSocket message. When a handler is set it is
// invoked with the received message in addition to returning it. It is an
// error to call Receive while not connected.
//
//	C: ws_receive()
func (m *LWSCModule) Receive() (*LWSCMessage, error) {
	m.mu.RLock()
	conn := m.conn
	connected := m.connected
	handler := m.handler
	m.mu.RUnlock()
	if !connected || conn == nil {
		return nil, errors.New("lwsc: not connected")
	}
	msgType, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	msg := &LWSCMessage{Type: msgType, Data: data}
	if handler != nil {
		handler(msg)
	}
	return msg, nil
}

// Close closes the underlying WebSocket connection. It is idempotent.
//
//	C: ws_close()
func (m *LWSCModule) Close() {
	m.mu.Lock()
	conn := m.conn
	m.conn = nil
	m.connected = false
	m.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// IsConnected reports whether a WebSocket connection is currently open.
func (m *LWSCModule) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// SetHandler installs a callback invoked for every message received via
// Receive.
//
//	C: ws_set_handler()
func (m *LWSCModule) SetHandler(handler func(*LWSCMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = handler
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *LWSCModule
)

// DefaultLWSC returns the process-wide LWSCModule, creating it on first
// use.
func DefaultLWSC() *LWSCModule {
	defaultMu.RLock()
	mod := defaultM
	defaultMu.RUnlock()
	if mod != nil {
		return mod
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)initialises the process-wide LWSCModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM != nil {
		defaultM.Close()
	}
	defaultM = New()
}

// ---------------------------------------------------------------------------
// Real dialer (RFC 6455 client handshake + framing)
// ---------------------------------------------------------------------------

// netConn wraps a net.Conn with a buffered reader and implements wsConn
// using RFC 6455 framing.
type netConn struct {
	nc net.Conn
	br *bufio.Reader
}

// defaultDialer performs the WebSocket handshake against cfg.URL and
// returns a framed connection.
func defaultDialer(cfg *LWSCConfig) (wsConn, error) {
	if cfg == nil || cfg.URL == "" {
		return nil, errors.New("lwsc: missing URL")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("lwsc: unsupported scheme " + u.Scheme)
	}
	hostPort := u.Host
	if hostPort == "" {
		return nil, errors.New("lwsc: missing host")
	}
	if !strings.Contains(hostPort, ":") {
		if u.Scheme == "wss" {
			hostPort += ":443"
		} else {
			hostPort += ":80"
		}
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	var raw net.Conn
	if u.Scheme == "wss" {
		raw, err = tls.Dial("tcp", hostPort, &tls.Config{ServerName: u.Hostname()})
	} else {
		raw, err = net.DialTimeout("tcp", hostPort, timeout)
	}
	if err != nil {
		return nil, err
	}
	if u.Scheme == "wss" {
		_ = raw.SetDeadline(time.Now().Add(timeout))
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		raw.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	origin := cfg.Origin
	if origin == "" {
		origin = "http://" + u.Hostname()
	}

	var req strings.Builder
	req.WriteString("GET " + path + " HTTP/1.1\r\n")
	req.WriteString("Host: " + u.Host + "\r\n")
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	req.WriteString("Sec-WebSocket-Key: " + key + "\r\n")
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	req.WriteString("Origin: " + origin + "\r\n")
	for k, v := range cfg.Headers {
		req.WriteString(k + ": " + v + "\r\n")
	}
	req.WriteString("\r\n")

	if u.Scheme == "wss" {
		_ = raw.SetDeadline(time.Now().Add(timeout))
	}
	if _, err := raw.Write([]byte(req.String())); err != nil {
		raw.Close()
		return nil, err
	}

	br := bufio.NewReader(raw)
	statusLine, err := br.ReadString('\n')
	if err != nil {
		raw.Close()
		return nil, err
	}
	if !strings.Contains(statusLine, " 101") {
		raw.Close()
		return nil, errors.New("lwsc: unexpected handshake response: " + strings.TrimSpace(statusLine))
	}
	acceptExpected := computeAccept(key)
	gotAccept := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			raw.Close()
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-accept:") {
			gotAccept = strings.TrimSpace(line[len("sec-websocket-accept:"):])
		}
	}
	if gotAccept == "" || gotAccept != acceptExpected {
		raw.Close()
		return nil, errors.New("lwsc: invalid Sec-WebSocket-Accept")
	}
	_ = raw.SetDeadline(time.Time{})
	return &netConn{nc: raw, br: br}, nil
}

// computeAccept returns the expected Sec-WebSocket-Accept for a key.
func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// WriteMessage sends a single masked data frame (RFC 6455 §5.3).
func (c *netConn) WriteMessage(msgType int, data []byte) error {
	return c.writeFrame(msgType, data)
}

// writeFrame builds and sends a masked frame.
func (c *netConn) writeFrame(opcode int, data []byte) error {
	b0 := byte(0x80 | (opcode & 0x0f)) // FIN=1
	var b1 byte
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	b1 |= 0x80 // client frames are masked

	var buf []byte
	l := len(data)
	switch {
	case l < 126:
		b1 |= byte(l)
		buf = append(buf, b0, b1)
	case l <= 0xffff:
		b1 |= 126
		buf = append(buf, b0, b1)
		buf = binary.BigEndian.AppendUint16(buf, uint16(l))
	default:
		b1 |= 127
		buf = append(buf, b0, b1)
		buf = binary.BigEndian.AppendUint64(buf, uint64(l))
	}
	buf = append(buf, mask[:]...)
	masked := make([]byte, l)
	for i := range data {
		masked[i] = data[i] ^ mask[i%4]
	}
	buf = append(buf, masked...)
	_, err := c.nc.Write(buf)
	return err
}

// ReadMessage reads a single data frame, transparently answering ping
// frames with pong and returning an error on close frames.
func (c *netConn) ReadMessage() (int, []byte, error) {
	for {
		b0, err := c.br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		b1, err := c.br.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		opcode := int(b0 & 0x0f)
		masked := b1&0x80 != 0
		length := int64(b1 & 0x7f)
		switch length {
		case 126:
			var l [2]byte
			if _, err := io.ReadFull(c.br, l[:]); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint16(l[:]))
		case 127:
			var l [8]byte
			if _, err := io.ReadFull(c.br, l[:]); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint64(l[:]))
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.br, mask[:]); err != nil {
				return 0, nil, err
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case TextMessage, BinaryMessage:
			return opcode, payload, nil
		case CloseMessage:
			return 0, nil, errors.New("lwsc: connection closed by peer")
		case PingMessage:
			if err := c.writeFrame(PongMessage, payload); err != nil {
				return 0, nil, err
			}
			continue
		case PongMessage:
			continue
		default:
			return opcode, payload, nil
		}
	}
}

// Close closes the underlying network connection.
func (c *netConn) Close() error {
	return c.nc.Close()
}
