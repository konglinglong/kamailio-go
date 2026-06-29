// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Faked (internal) SIP message handling - matching C fmsg.c / fmsg.h.
 *
 * In the C core, fmsg.c maintains a single reusable "faked" sip_msg_t that
 * is initialised from a static OPTIONS template and handed out to internal
 * callers (timers, async tasks, route execution without a real network
 * message). The Go port generalises this into a FakeMsgBuilder that can
 * construct arbitrary internal SIP messages, plus convenience constructors
 * NewTimerMsg and NewInternalReq for the most common use cases.
 *
 * The package follows the project New() / Default*() / Init() convention:
 *   - NewFakeMsgBuilder() builds a custom message.
 *   - DefaultFakedMsg() returns the process-wide singleton faked message
 *     (the Go counterpart of C's _faked_msg), lazily initialised.
 *   - Init() resets the singleton.
 */

package fmsg

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
	"github.com/kamailio/kamailio-go/internal/core/str"
)

// fakedMsgTemplate mirrors C's FAKED_SIP_MSG macro: a minimal OPTIONS
// request that is reused as the basis for internal messages.
const fakedMsgTemplate = "OPTIONS sip:kamailio.org SIP/2.0\r\n" +
	"Via: SIP/2.0/UDP 127.0.0.1\r\n" +
	"From: <sip:server@kamailio.org>;tag=xyz\r\n" +
	"To: <sip:server@kamailio.org>\r\n" +
	"Call-ID: aaa-bbb-ccc-ddd\r\n" +
	"CSeq: 1 OPTIONS\r\n" +
	"Content-Length: 0\r\n" +
	"\r\n"

// FakeMsgBuilder constructs an internal ("faked") SIP message. It is the
// Go counterpart of the C faked_msg_init_new() flow, generalised so the
// caller can customise every header instead of being limited to the
// static OPTIONS template.
type FakeMsgBuilder struct {
	Method     string
	RURI       string
	FromURI    string
	ToURI      string
	CallID     string
	CSeq       int
	CSeqMethod string
	FromTag    string
	Body       string
	Headers    []string // extra headers appended after the standard set
}

// NewFakeMsgBuilder creates a builder pre-filled with sensible defaults
// (matching the C FAKED_SIP_MSG template). The caller only needs to set
// Method and RURI; the remaining fields fall back to defaults that produce
// a valid SIP message.
func NewFakeMsgBuilder(method, ruri string) *FakeMsgBuilder {
	return &FakeMsgBuilder{
		Method:     method,
		RURI:       ruri,
		FromURI:    "sip:server@kamailio.org",
		ToURI:      "sip:server@kamailio.org",
		CallID:     "faked-" + randomCallID(),
		CSeq:       1,
		CSeqMethod: method,
		FromTag:    "xyz",
		Body:       "",
		Headers:    nil,
	}
}

// Build constructs a parsed *parser.SIPMsg from the builder's fields.
// It serialises the message to wire format and then runs it through the
// standard parser, exactly as C's faked_msg_init_new() calls parse_msg().
func (b *FakeMsgBuilder) Build() (*parser.SIPMsg, error) {
	raw, err := b.BuildAsString()
	if err != nil {
		return nil, err
	}
	msg, err := parser.ParseMsg([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("fmsg: parse failed: %w", err)
	}
	// Stamp a unique ID and receive time, mirroring C's faked_msg_get_next_id().
	msg.ID = nextMsgID()
	msg.PID = 0
	msg.ReceivedAt = time.Now()
	return msg, nil
}

// BuildAsString serialises the builder's fields into a SIP wire-format
// string (request line + headers + blank line + body). It does not parse
// the result; use Build() if you need a *parser.SIPMsg.
func (b *FakeMsgBuilder) BuildAsString() (string, error) {
	if b.Method == "" {
		return "", fmt.Errorf("fmsg: method is empty")
	}
	if b.RURI == "" {
		return "", fmt.Errorf("fmsg: RURI is empty")
	}

	var sb strings.Builder

	// Request line.
	sb.WriteString(b.Method)
	sb.WriteByte(' ')
	sb.WriteString(b.RURI)
	sb.WriteString(" SIP/2.0\r\n")

	// Via (always present, matching the C template).
	sb.WriteString("Via: SIP/2.0/UDP 127.0.0.1\r\n")

	// From.
	fromURI := b.FromURI
	if fromURI == "" {
		fromURI = "sip:server@kamailio.org"
	}
	sb.WriteString("From: <")
	sb.WriteString(fromURI)
	sb.WriteByte('>')
	if b.FromTag != "" {
		sb.WriteString(";tag=")
		sb.WriteString(b.FromTag)
	}
	sb.WriteString("\r\n")

	// To.
	toURI := b.ToURI
	if toURI == "" {
		toURI = "sip:server@kamailio.org"
	}
	sb.WriteString("To: <")
	sb.WriteString(toURI)
	sb.WriteString(">\r\n")

	// Call-ID.
	callID := b.CallID
	if callID == "" {
		callID = "faked-" + randomCallID()
	}
	sb.WriteString("Call-ID: ")
	sb.WriteString(callID)
	sb.WriteString("\r\n")

	// CSeq.
	cseq := b.CSeq
	if cseq <= 0 {
		cseq = 1
	}
	cseqMethod := b.CSeqMethod
	if cseqMethod == "" {
		cseqMethod = b.Method
	}
	sb.WriteString("CSeq: ")
	sb.WriteString(strconv.Itoa(cseq))
	sb.WriteByte(' ')
	sb.WriteString(cseqMethod)
	sb.WriteString("\r\n")

	// Max-Forwards (good practice for internal messages too).
	sb.WriteString("Max-Forwards: 70\r\n")

	// Extra headers.
	for _, h := range b.Headers {
		h = strings.TrimSpace(h)
		if h != "" {
			if !strings.HasSuffix(h, "\r\n") {
				h += "\r\n"
			}
			sb.WriteString(h)
		}
	}

	// Content-Length (always present, matching C template).
	bodyLen := len(b.Body)
	sb.WriteString("Content-Length: ")
	sb.WriteString(strconv.Itoa(bodyLen))
	sb.WriteString("\r\n")

	// Blank line separating headers from body.
	sb.WriteString("\r\n")

	// Body.
	if bodyLen > 0 {
		sb.WriteString(b.Body)
	}

	return sb.String(), nil
}

// NewTimerMsg creates a faked message suitable for timer-triggered route
// execution. The route name is encoded in a custom X-Timer-Route header
// and any parameters are added as X-Timer-Param headers, so the receiving
// route can inspect them via the parser.
//
// C counterpart: the C core uses faked_msg_get_next_clear() before invoking
// a timer route; this function provides the Go equivalent.
func NewTimerMsg(routeName string, params map[string]string) (*parser.SIPMsg, error) {
	if routeName == "" {
		return nil, fmt.Errorf("fmsg: routeName is empty")
	}

	b := NewFakeMsgBuilder("OPTIONS", "sip:kamailio.org")
	b.CallID = "timer-" + randomCallID()
	b.Headers = append(b.Headers, "X-Timer-Route: "+routeName)

	// Sort parameter keys for deterministic output.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	// Simple insertion sort (param count is typically small).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	for _, k := range keys {
		b.Headers = append(b.Headers, fmt.Sprintf("X-Timer-Param: %s=%s", k, params[k]))
	}

	return b.Build()
}

// NewInternalReq creates an internal SIP request that never touches the
// network. It is useful for generating self-originated requests (e.g.
// pinging a downstream, triggering a reload route). The fromTag identifies
// the logical sender.
func NewInternalReq(method, ruri, fromTag string) (*parser.SIPMsg, error) {
	if method == "" {
		return nil, fmt.Errorf("fmsg: method is empty")
	}
	if ruri == "" {
		return nil, fmt.Errorf("fmsg: RURI is empty")
	}

	b := NewFakeMsgBuilder(method, ruri)
	if fromTag != "" {
		b.FromTag = fromTag
	}
	b.CallID = "internal-" + randomCallID()
	b.Headers = append(b.Headers, "X-Internal-Req: true")

	return b.Build()
}

// ============================================================
// Process-wide singleton faked message (C _faked_msg equivalent)
// ============================================================

// fakedMsgState holds the singleton faked message and its initialisation
// guard, mirroring C's _faked_msg / _faked_sip_buf_init.
type fakedMsgState struct {
	mu      sync.Mutex
	msg     *parser.SIPMsg
	initErr error
	ready   bool
}

var fakedState fakedMsgState

// initFakedMsg lazily parses the C-equivalent FAKED_SIP_MSG template into
// the singleton. It is safe to call concurrently; only the first caller
// performs the parse.
func initFakedMsg() error {
	fakedState.mu.Lock()
	defer fakedState.mu.Unlock()
	if fakedState.ready {
		return fakedState.initErr
	}
	msg, err := parser.ParseMsg([]byte(fakedMsgTemplate))
	if err != nil {
		fakedState.initErr = fmt.Errorf("fmsg: failed to parse faked template: %w", err)
		fakedState.ready = true
		return fakedState.initErr
	}
	msg.ID = nextMsgID()
	msg.PID = 0
	msg.ReceivedAt = time.Now()
	fakedState.msg = msg
	fakedState.ready = true
	fakedState.initErr = nil
	return nil
}

// DefaultFakedMsg returns the process-wide singleton faked message,
// initialising it on first use (lazy, matching C's faked_msg_init()).
//
// The returned message is the shared template; callers must treat it as
// read-only. Use GetNext() or GetNextClear() to obtain a private, mutable
// copy.
func DefaultFakedMsg() *parser.SIPMsg {
	if err := initFakedMsg(); err != nil {
		return nil
	}
	fakedState.mu.Lock()
	defer fakedState.mu.Unlock()
	return fakedState.msg
}

// GetNext returns a private clone of the singleton faked message with a
// fresh ID and timestamp, mirroring C's faked_msg_next(). Because the
// returned message is a deep copy, callers may freely modify it without
// affecting the shared template or other goroutines.
func GetNext() *parser.SIPMsg {
	if err := initFakedMsg(); err != nil {
		return nil
	}
	fakedState.mu.Lock()
	defer fakedState.mu.Unlock()
	if fakedState.msg == nil {
		return nil
	}
	clone, err := fakedState.msg.Clone()
	if err != nil {
		return nil
	}
	clone.ID = nextMsgID()
	clone.ReceivedAt = time.Now()
	return clone
}

// GetNextClear is the Go counterpart of C's faked_msg_get_next_clear().
// It behaves like GetNext() but also resets the clone's routing fields
// (NewURI, DstURI, Flags) so the caller starts from a clean slate.
func GetNextClear() *parser.SIPMsg {
	msg := GetNext()
	if msg == nil {
		return nil
	}
	msg.NewURI = str.Str{}
	msg.DstURI = str.Str{}
	msg.Flags = 0
	return msg
}

// Match reports whether the given message pointer is the singleton faked
// message, mirroring C's faked_msg_match().
func Match(msg *parser.SIPMsg) bool {
	if err := initFakedMsg(); err != nil {
		return false
	}
	fakedState.mu.Lock()
	defer fakedState.mu.Unlock()
	return msg == fakedState.msg
}

// Init (re)initialises the process-wide singleton faked message. It is
// safe to call multiple times and is intended for test isolation.
func Init() {
	fakedState.mu.Lock()
	defer fakedState.mu.Unlock()
	fakedState.msg = nil
	fakedState.ready = false
	fakedState.initErr = nil
}

// ============================================================
// Internal helpers
// ============================================================

// msgIDCounter is the monotonic message-ID counter, mirroring C's
// _faked_msg_no. It is accessed atomically so it is safe under -race.
var msgIDCounter uint32

// nextMsgID returns the next message ID, skipping zero (matching C's
// faked_msg_get_next_id which jumps over 0).
func nextMsgID() uint32 {
	for {
		id := atomic.AddUint32(&msgIDCounter, 1)
		if id != 0 {
			return id
		}
		// Skip zero: increment once more.
		atomic.AddUint32(&msgIDCounter, 1)
	}
}

// randomCallID generates a pseudo-random Call-ID suffix. It uses the
// monotonic counter combined with the current time so that concurrent
// builders produce distinct values without requiring crypto/rand.
func randomCallID() string {
	return strconv.FormatUint(uint64(nextMsgID()), 16) + "-" +
		strconv.FormatInt(time.Now().UnixNano(), 16)
}
