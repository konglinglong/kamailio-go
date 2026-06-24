// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * msrp - Message Session Relay Protocol helpers.
 *
 * Parses and builds MSRP frames. An MSRP frame looks like:
 *
 *   MSRP <method> <transaction-id>\r\n
 *   To-Path: msrp://host\r\n
 *   From-Path: msrp://host\r\n
 *   Message-ID: <id>\r\n
 *   \r\n
 *   <body>\r\n
 *   -------<transaction-id>$\r\n
 *
 * Mirrors the kamailio msrp module.
 */

package msrp

import (
	"bytes"
	"errors"
	"strings"
)

// MSRPMsg represents a parsed MSRP message.
type MSRPMsg struct {
	Method    string
	ToPath    string
	FromPath  string
	MessageID string
	Body      string
	TxnID     string
}

// MSRPModule parses and builds MSRP frames.
type MSRPModule struct{}

// New returns a new MSRPModule.
func New() *MSRPModule { return &MSRPModule{} }

// IsMSRP reports whether data begins with the MSRP protocol marker.
func (m *MSRPModule) IsMSRP(data []byte) bool {
	if m == nil || len(data) < 4 {
		return false
	}
	return string(data[:4]) == "MSRP"
}

// ParseMessage parses an MSRP frame into an MSRPMsg.
func (m *MSRPModule) ParseMessage(data []byte) (*MSRPMsg, error) {
	if m == nil {
		return nil, errors.New("msrp: nil module")
	}
	if !m.IsMSRP(data) {
		return nil, errors.New("msrp: not an MSRP message")
	}
	// Split headers from body.
	hdrEnd := bytes.Index(data, []byte("\r\n\r\n"))
	var hdrPart, bodyPart []byte
	if hdrEnd == -1 {
		hdrPart = data
	} else {
		hdrPart = data[:hdrEnd]
		bodyPart = data[hdrEnd+4:]
	}
	lines := strings.Split(string(hdrPart), "\r\n")
	if len(lines) == 0 {
		return nil, errors.New("msrp: empty message")
	}
	msg := &MSRPMsg{}
	// First line: "MSRP <method> <txn-id>"
	fields := strings.Fields(lines[0])
	if len(fields) < 2 {
		return nil, errors.New("msrp: malformed first line")
	}
	msg.Method = fields[1]
	if len(fields) >= 3 {
		msg.TxnID = fields[2]
	}
	for _, line := range lines[1:] {
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch strings.ToLower(name) {
		case "to-path":
			msg.ToPath = val
		case "from-path":
			msg.FromPath = val
		case "message-id":
			msg.MessageID = val
		}
	}
	// Strip trailing end-marker if present.
	body := string(bodyPart)
	if end := strings.Index(body, "\r\n-------"); end >= 0 {
		body = body[:end]
	}
	msg.Body = body
	return msg, nil
}

// BuildMessage serialises an MSRPMsg into an MSRP frame.
func (m *MSRPModule) BuildMessage(msg *MSRPMsg) []byte {
	if m == nil || msg == nil {
		return nil
	}
	txn := msg.TxnID
	if txn == "" {
		txn = "txnid"
	}
	var b strings.Builder
	b.WriteString("MSRP " + msg.Method + " " + txn + "\r\n")
	if msg.ToPath != "" {
		b.WriteString("To-Path: " + msg.ToPath + "\r\n")
	}
	if msg.FromPath != "" {
		b.WriteString("From-Path: " + msg.FromPath + "\r\n")
	}
	if msg.MessageID != "" {
		b.WriteString("Message-ID: " + msg.MessageID + "\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(msg.Body)
	b.WriteString("\r\n-------" + txn + "$\r\n")
	return []byte(b.String())
}
