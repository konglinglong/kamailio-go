// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * smsops module - SMS body parsing and manipulation.
 * Port of the kamailio smsops module (src/modules/smsops).
 *
 * The original C module parses SMS bodies carried in SIP messages,
 * extracts text, validates and normalises phone numbers. This Go
 * counterpart provides the same operations using a simple text-based
 * body format (header lines followed by a blank line and the text).
 *
 * It is safe for concurrent use.
 */

package smsops

import (
	"fmt"
	"strings"
	"sync"
)

// SMSMessage represents a parsed SMS body.
type SMSMessage struct {
	From   string
	To     string
	Body   string
	Coding int
}

// SMSOpsModule parses, builds and manipulates SMS bodies.
// It is the Go counterpart of the kamailio smsops module.
type SMSOpsModule struct {
	mu sync.RWMutex
}

// New creates an SMSOpsModule.
func New() *SMSOpsModule {
	return &SMSOpsModule{}
}

// ParseBody parses an SMS body string into an SMSMessage. The body is
// expected to contain optional "From:", "To:" and "Coding:" header
// lines followed by a blank line and the message text.
//
//	C: smsops_parse()
func (m *SMSOpsModule) ParseBody(body string) (*SMSMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if body == "" {
		return nil, fmt.Errorf("smsops: empty body")
	}
	msg := &SMSMessage{}
	lines := strings.Split(body, "\n")
	i := 0
	for ; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "" {
			i++
			break
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "from:"):
			msg.From = strings.TrimSpace(line[len("from:"):])
		case strings.HasPrefix(lower, "to:"):
			msg.To = strings.TrimSpace(line[len("to:"):])
		case strings.HasPrefix(lower, "coding:"):
			c := strings.TrimSpace(line[len("coding:"):])
			msg.Coding = parseInt(c)
		}
	}
	msg.Body = strings.Join(lines[i:], "\n")
	msg.Body = strings.TrimRight(msg.Body, "\n")
	return msg, nil
}

// BuildBody builds an SMS body string from an SMSMessage.
//
//	C: smsops_build()
func (m *SMSOpsModule) BuildBody(msg *SMSMessage) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if msg == nil {
		return ""
	}
	var b strings.Builder
	if msg.From != "" {
		fmt.Fprintf(&b, "From: %s\n", msg.From)
	}
	if msg.To != "" {
		fmt.Fprintf(&b, "To: %s\n", msg.To)
	}
	fmt.Fprintf(&b, "Coding: %d\n", msg.Coding)
	b.WriteString("\n")
	b.WriteString(msg.Body)
	return b.String()
}

// ExtractText returns just the message text from a body, dropping the
// header section.
//
//	C: smsops_extract_text()
func (m *SMSOpsModule) ExtractText(body string) string {
	msg, err := m.ParseBody(body)
	if err != nil {
		return ""
	}
	return msg.Body
}

// ValidateNumber reports whether number looks like a valid phone number
// (digits with an optional leading +, length 3..16).
//
//	C: smsops_validate_number()
func (m *SMSOpsModule) ValidateNumber(number string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := strings.TrimSpace(number)
	if s == "" {
		return false
	}
	digits := s
	if strings.HasPrefix(digits, "+") {
		digits = digits[1:]
	}
	if len(digits) < 3 || len(digits) > 15 {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// NormalizeNumber normalises number by stripping non-digits and
// prefixing the supplied country code when the number is not already
// international.
//
//	C: smsops_normalize_number()
func (m *SMSOpsModule) NormalizeNumber(number, countryCode string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	digits := digitsOnly(number)
	if digits == "" {
		return ""
	}
	if strings.HasPrefix(number, "+") || strings.HasPrefix(number, "00") {
		return "+" + digits
	}
	cc := digitsOnly(countryCode)
	if cc == "" {
		return digits
	}
	return "+" + cc + digits
}

// --- helpers ---

// digitsOnly strips everything except digits from s.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseInt parses an integer, returning 0 on failure.
func parseInt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// --- package-level API ---

var defaultModule = New()

// DefaultSMSOps returns the package-level default SMSOpsModule.
func DefaultSMSOps() *SMSOpsModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
