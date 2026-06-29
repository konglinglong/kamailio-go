// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - lwsc module tests.
 *
 * Tests use a mock dialer / mock connection so no real WebSocket server is
 * required.
 */

package lwsc

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&LWSCConfig{
		URL:     "ws://127.0.0.1:5060/ws",
		Origin:  "http://127.0.0.1",
		Headers: map[string]string{"X-Token": "abc"},
		Timeout: 5 * time.Second,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.URL != "ws://127.0.0.1:5060/ws" {
		t.Errorf("URL = %q", m.cfg.URL)
	}
	if m.cfg.Origin != "http://127.0.0.1" {
		t.Errorf("Origin = %q", m.cfg.Origin)
	}
	if m.cfg.Headers["X-Token"] != "abc" {
		t.Errorf("Headers = %v", m.cfg.Headers)
	}
	if m.cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v", m.cfg.Timeout)
	}
	// nil config is accepted.
	if err := (&LWSCModule{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestConnectWithMock(t *testing.T) {
	m := New()
	mc := newMockConn()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return mc, nil }

	if err := m.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !m.IsConnected() {
		t.Errorf("IsConnected() = false, want true")
	}
	// Connecting again replaces the connection without error.
	if err := m.Connect(); err != nil {
		t.Errorf("second Connect() error = %v", err)
	}
	m.Close()
}

func TestConnectError(t *testing.T) {
	m := New()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return nil, errDial }
	if err := m.Connect(); err == nil {
		t.Errorf("Connect() should error when dialer fails")
	}
	if m.IsConnected() {
		t.Errorf("IsConnected() = true, want false after failed Connect")
	}
}

func TestSend(t *testing.T) {
	m := New()
	mc := newMockConn()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return mc, nil }
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if err := m.Send(&LWSCMessage{Type: 1, Data: []byte("hello")}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := m.Send(&LWSCMessage{Type: 2, Data: []byte{0, 1, 2}}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	sent := mc.sentMessages()
	if len(sent) != 2 {
		t.Fatalf("sent = %d messages, want 2", len(sent))
	}
	if sent[0].msgType != 1 || string(sent[0].data) != "hello" {
		t.Errorf("sent[0] = %+v", sent[0])
	}
	if sent[1].msgType != 2 || len(sent[1].data) != 3 {
		t.Errorf("sent[1] = %+v", sent[1])
	}

	// Send while not connected -> error.
	m.Close()
	if err := m.Send(&LWSCMessage{Type: 1, Data: []byte("x")}); err == nil {
		t.Errorf("Send() after Close should error")
	}
}

func TestReceive(t *testing.T) {
	m := New()
	mc := newMockConn()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return mc, nil }
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	mc.queueRecv(1, []byte("ping"))
	msg, err := m.Receive()
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if msg.Type != 1 || string(msg.Data) != "ping" {
		t.Errorf("msg = %+v, want {Type:1 Data:ping}", msg)
	}

	mc.queueRecv(2, []byte{0xff})
	msg, err = m.Receive()
	if err != nil {
		t.Fatalf("Receive() error: %v", err)
	}
	if msg.Type != 2 || msg.Data[0] != 0xff {
		t.Errorf("msg = %+v", msg)
	}

	// Receive while not connected -> error.
	m.Close()
	if _, err := m.Receive(); err == nil {
		t.Errorf("Receive() after Close should error")
	}
}

func TestReceiveWithHandler(t *testing.T) {
	m := New()
	mc := newMockConn()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return mc, nil }
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	var received []*LWSCMessage
	var mu sync.Mutex
	m.SetHandler(func(msg *LWSCMessage) {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
	})

	mc.queueRecv(1, []byte("a"))
	mc.queueRecv(1, []byte("b"))
	if _, err := m.Receive(); err != nil {
		t.Fatalf("Receive() error: %v", err)
	}
	if _, err := m.Receive(); err != nil {
		t.Fatalf("Receive() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("handler received %d messages, want 2", len(received))
	}
	if string(received[0].Data) != "a" || string(received[1].Data) != "b" {
		t.Errorf("handler messages = %v", received)
	}
}

func TestClose(t *testing.T) {
	m := New()
	mc := newMockConn()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return mc, nil }
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}
	m.Close()
	if m.IsConnected() {
		t.Errorf("IsConnected() = true, want false after Close")
	}
	if !mc.closed {
		t.Errorf("underlying conn not closed")
	}
	// Close is idempotent.
	m.Close()
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultLWSC() == nil {
		t.Fatalf("DefaultLWSC() nil")
	}
	Init()
	d := DefaultLWSC()
	if d == nil {
		t.Fatalf("DefaultLWSC() nil after Init")
	}
	if d != DefaultLWSC() {
		t.Fatalf("DefaultLWSC() returned different instances")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	mc := newMockConn()
	m.dialer = func(cfg *LWSCConfig) (wsConn, error) { return mc, nil }
	if err := m.Connect(); err != nil {
		t.Fatalf("Connect() error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = m.Send(&LWSCMessage{Type: 1, Data: []byte("m")})
			mc.queueRecv(1, []byte("r"))
			_, _ = m.Receive()
		}(i)
	}
	wg.Wait()
	m.Close()
}

// --- mock connection ---

var errDial = errors.New("dial failed")

type mockMessage struct {
	msgType int
	data    []byte
}

type mockConn struct {
	mu     sync.Mutex
	sent   []mockMessage
	recv   chan mockMessage
	closed bool
}

func newMockConn() *mockConn {
	return &mockConn{recv: make(chan mockMessage, 16)}
}

func (c *mockConn) WriteMessage(msgType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("connection closed")
	}
	dup := make([]byte, len(data))
	copy(dup, data)
	c.sent = append(c.sent, mockMessage{msgType: msgType, data: dup})
	return nil
}

func (c *mockConn) ReadMessage() (int, []byte, error) {
	select {
	case msg := <-c.recv:
		return msg.msgType, msg.data, nil
	default:
		return 0, nil, errors.New("no message")
	}
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *mockConn) queueRecv(msgType int, data []byte) {
	dup := make([]byte, len(data))
	copy(dup, data)
	c.recv <- mockMessage{msgType: msgType, data: dup}
}

func (c *mockConn) sentMessages() []mockMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]mockMessage, len(c.sent))
	copy(out, c.sent)
	return out
}
