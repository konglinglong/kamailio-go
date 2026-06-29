// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * MaxFwd module - Max-Forwards header processing.
 * Port of the kamailio maxfwd module (src/modules/maxfwd).
 *
 * The Max-Forwards header limits the number of hops a SIP request may
 * traverse. Each proxy decrements it by one; when it reaches zero the
 * request must not be forwarded further (the caller should reply 483
 * Too Many Hops).
 */

package maxfwd

import (
	"errors"
	"strconv"
	"strings"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// DefaultMaxLimit is the default maximum Max-Forwards limit.
// C: cfg_group_maxfwd.max_limit = 70
const DefaultMaxLimit = 70

// MaxMaxFwdValue is the largest legal Max-Forwards value (single byte, 0-255).
const MaxMaxFwdValue = 255

// MaxFwdModule implements Max-Forwards header processing.
// It is the Go counterpart of the kamailio maxfwd module.
type MaxFwdModule struct {
	// MaxLimit caps an excessively large Max-Forwards value before
	// decrement. Values above MaxLimit are reduced to MaxLimit+1 so
	// that after decrement they equal MaxLimit.
	MaxLimit int
}

// New creates a MaxFwdModule with the default max limit.
func New() *MaxFwdModule {
	return &MaxFwdModule{MaxLimit: DefaultMaxLimit}
}

// Process processes the Max-Forwards header of a SIP message.
//
// Return values:
//
//	 0  OK to continue (header present and decremented)
//	-1  max forwards reached (value was 0; caller should reply 483)
//	 1  header was missing and has been added
//
// A non-nil error is returned for parse failures or invalid parameters.
//
// This mirrors the C process_maxfwd_header() semantics, remapped:
// C return 2 (added) -> 1, C return 1 (decremented) -> 0,
// C return -1 (value zero) -> -1, C return -2 (error) -> error.
func (m *MaxFwdModule) Process(msg *parser.SIPMsg, defaultLimit int) (int, error) {
	if defaultLimit < 0 || defaultLimit > MaxMaxFwdValue {
		return -1, errors.New("maxfwd: invalid default limit")
	}
	if msg == nil {
		return -1, errors.New("maxfwd: nil message")
	}

	val, found, err := m.lookupMaxFwd(msg)
	if err != nil {
		return -1, err
	}
	if !found {
		m.addMaxFwdHeader(msg, defaultLimit)
		return 1, nil
	}
	if val == 0 {
		// Max forwards exhausted - do not forward.
		return -1, nil
	}
	if val > m.MaxLimit {
		// Clamp excessively large values down to MaxLimit (after decrement).
		val = m.MaxLimit + 1
	}
	m.setMaxFwd(msg, val-1)
	return 0, nil
}

// CheckMaxFwd returns the current Max-Forwards value, or -1 if the
// header is missing or cannot be parsed.
func (m *MaxFwdModule) CheckMaxFwd(msg *parser.SIPMsg) int {
	val, found, err := m.lookupMaxFwd(msg)
	if err != nil || !found {
		return -1
	}
	return val
}

// DecrementMaxFwd decrements the Max-Forwards value by one and returns
// the new value, or -1 if the header is missing or unparseable.
// The value is never reduced below zero.
func (m *MaxFwdModule) DecrementMaxFwd(msg *parser.SIPMsg) int {
	val, found, err := m.lookupMaxFwd(msg)
	if err != nil || !found {
		return -1
	}
	newVal := val - 1
	if newVal < 0 {
		newVal = 0
	}
	m.setMaxFwd(msg, newVal)
	return newVal
}

// SetMaxFwd sets the Max-Forwards header to the given value and returns
// it, or -1 if the value is out of range (0-255).
func (m *MaxFwdModule) SetMaxFwd(msg *parser.SIPMsg, value int) int {
	if value < 0 || value > MaxMaxFwdValue {
		return -1
	}
	m.setMaxFwd(msg, value)
	return value
}

// IsZero reports whether the Max-Forwards value is zero.
func (m *MaxFwdModule) IsZero(msg *parser.SIPMsg) bool {
	return m.CheckMaxFwd(msg) == 0
}

// lookupMaxFwd finds and parses the Max-Forwards header.
// Returns the numeric value, whether the header was present, and any
// parse error.
func (m *MaxFwdModule) lookupMaxFwd(msg *parser.SIPMsg) (int, bool, error) {
	h := msg.MaxForwards
	if h == nil {
		h = msg.GetHeaderByType(parser.HdrMaxForwards)
	}
	if h == nil {
		return 0, false, nil
	}
	body := strings.TrimSpace(h.Body.String())
	val, err := strconv.Atoi(body)
	if err != nil {
		return 0, true, errors.New("maxfwd: invalid Max-Forwards value: " + body)
	}
	if val < 0 || val > MaxMaxFwdValue {
		return 0, true, errors.New("maxfwd: Max-Forwards value out of range: " + body)
	}
	return val, true, nil
}

// setMaxFwd overwrites the Max-Forwards header body with value.
func (m *MaxFwdModule) setMaxFwd(msg *parser.SIPMsg, value int) {
	h := msg.MaxForwards
	if h == nil {
		h = msg.GetHeaderByType(parser.HdrMaxForwards)
	}
	if h != nil {
		h.Body = str.Mk(strconv.Itoa(value))
	}
}

// addMaxFwdHeader inserts a new Max-Forwards header into the message.
func (m *MaxFwdModule) addMaxFwdHeader(msg *parser.SIPMsg, value int) {
	h := &parser.HdrField{
		Type: parser.HdrMaxForwards,
		Name: str.Mk("Max-Forwards"),
		Body: str.Mk(strconv.Itoa(value)),
	}
	msg.Headers = append(msg.Headers, h)
	msg.MaxForwards = h
}

// --- package-level API ---

var defaultModule = New()

// DefaultMaxFwd returns the default Max-Forwards limit.
func DefaultMaxFwd() int {
	return DefaultMaxLimit
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}

// Process processes the Max-Forwards header using the default module.
func Process(msg *parser.SIPMsg, defaultLimit int) (int, error) {
	return defaultModule.Process(msg, defaultLimit)
}

// CheckMaxFwd returns the Max-Forwards value using the default module.
func CheckMaxFwd(msg *parser.SIPMsg) int {
	return defaultModule.CheckMaxFwd(msg)
}
