// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * sms module - SMS sending, receiving and PDU (de)coding.
 * Port of the kamailio sms module (src/modules/sms).
 *
 * The original C module interfaces with a GSM modem to send and receive
 * SMS messages and to (de)code PDU strings. This Go counterpart exposes
 * the same operations using an in-memory inbox channel and a compact
 * length-prefixed PDU format:
 *
 *	[0]      coding
 *	[1:3]    validity seconds (big-endian uint16)
 *	[3]      from length
 *	[3+1..]  from bytes
 *	[next]   to length
 *	[next+1] to bytes
 *	[next+2:next+4] body length (big-endian uint16)
 *	[next+4..] body bytes
 *
 * It is safe for concurrent use.
 */

package sms

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// SMSMessage represents a short message.
type SMSMessage struct {
	From     string
	To       string
	Body     string
	Coding   int
	Validity time.Duration
}

// inboxSize is the capacity of the receive inbox.
const inboxSize = 1024

// SMSModule sends, receives and (de)codes SMS messages.
// It is the Go counterpart of the kamailio sms module.
type SMSModule struct {
	mu    sync.Mutex
	inbox chan *SMSMessage
}

// New creates an SMSModule.
func New() *SMSModule {
	return &SMSModule{inbox: make(chan *SMSMessage, inboxSize)}
}

// Init (re)initialises the module, clearing any pending messages.
//
//	C: mod_init()
func (m *SMSModule) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inbox = make(chan *SMSMessage, inboxSize)
	return nil
}

// Send enqueues a message for delivery. The message is also placed in
// the receive inbox so callers of Receive can read it.
//
//	C: sms_send_msg()
func (m *SMSModule) Send(from, to, body string) error {
	m.mu.Lock()
	inbox := m.inbox
	m.mu.Unlock()
	msg := &SMSMessage{From: from, To: to, Body: body, Coding: 0, Validity: 0}
	select {
	case inbox <- msg:
		return nil
	default:
		return fmt.Errorf("sms: inbox full")
	}
}

// Receive returns the channel from which received messages can be read.
//
//	C: sms_receive()
func (m *SMSModule) Receive() <-chan *SMSMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inbox
}

// EncodePDU encodes a message into the compact PDU byte format.
//
//	C: pdu_encode()
func (m *SMSModule) EncodePDU(msg *SMSMessage) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("sms: nil message")
	}
	coding := byte(msg.Coding)
	validity := uint16(msg.Validity / time.Second)
	from := []byte(msg.From)
	to := []byte(msg.To)
	body := []byte(msg.Body)
	if len(from) > 255 || len(to) > 255 || len(body) > 65535 {
		return nil, fmt.Errorf("sms: field too long")
	}
	out := make([]byte, 0, 3+1+len(from)+1+len(to)+2+len(body))
	out = append(out, coding)
	out = append(out, byte(validity>>8), byte(validity))
	out = append(out, byte(len(from)))
	out = append(out, from...)
	out = append(out, byte(len(to)))
	out = append(out, to...)
	var blen [2]byte
	binary.BigEndian.PutUint16(blen[:], uint16(len(body)))
	out = append(out, blen[:]...)
	out = append(out, body...)
	return out, nil
}

// DecodePDU decodes a PDU byte slice back into a message.
//
//	C: pdu_decode()
func (m *SMSModule) DecodePDU(data []byte) (*SMSMessage, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("sms: pdu too short")
	}
	msg := &SMSMessage{}
	msg.Coding = int(data[0])
	msg.Validity = time.Duration(binary.BigEndian.Uint16(data[1:3])) * time.Second
	pos := 3
	fromLen := int(data[pos])
	pos++
	if pos+fromLen > len(data) {
		return nil, fmt.Errorf("sms: truncated from")
	}
	msg.From = string(data[pos : pos+fromLen])
	pos += fromLen
	if pos >= len(data) {
		return nil, fmt.Errorf("sms: truncated to length")
	}
	toLen := int(data[pos])
	pos++
	if pos+toLen > len(data) {
		return nil, fmt.Errorf("sms: truncated to")
	}
	msg.To = string(data[pos : pos+toLen])
	pos += toLen
	if pos+2 > len(data) {
		return nil, fmt.Errorf("sms: truncated body length")
	}
	bodyLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2
	if pos+bodyLen > len(data) {
		return nil, fmt.Errorf("sms: truncated body")
	}
	msg.Body = string(data[pos : pos+bodyLen])
	return msg, nil
}

// PendingCount returns the number of messages waiting in the inbox.
//
//	C: sms_pending_count()
func (m *SMSModule) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.inbox)
}

// --- package-level API ---

var defaultModule = New()

// DefaultSMS returns the package-level default SMSModule.
func DefaultSMS() *SMSModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	_ = defaultModule.Init()
}
