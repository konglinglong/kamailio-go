// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SL module - stateless SIP replies, matching Kamailio's C sl module
 * (sl_funcs.c / sl.h).
 *
 * The SL module builds SIP reply buffers from incoming requests without
 * creating any transaction state. It generates unique to-tags, tracks
 * reply statistics, and uses the msgtranslator package to assemble the
 * on-wire reply.
 */
package sl

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/msgtranslator"
	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// slSignature is the fixed to-tag signature, matching the C
// init_tags() "KAMAILIO-stateless" prefix.
const slSignature = "KAMAILIO-stateless"

// StatelessReply represents a stateless SIP reply built by the SL module.
type StatelessReply struct {
	Code    int
	Reason  string
	Headers []string
	Body    string
}

// SLStats holds stateless reply statistics. All fields are safe for
// concurrent access via atomic operations.
type SLStats struct {
	RepliesSent  atomic.Int64
	RepliesError atomic.Int64
}

// SLModule implements Kamailio's stateless (sl) module. It builds SIP
// reply buffers from incoming requests using the msgtranslator package,
// generates unique to-tags, and tracks reply statistics.
type SLModule struct {
	mu         sync.Mutex
	stats      SLStats
	tagCounter atomic.Int64
	lastReply  *StatelessReply
	lastBuffer []byte
}

// NewSLModule creates a new SLModule.
func NewSLModule() *SLModule {
	return &SLModule{}
}

// GenTag generates a unique to-tag. The tag is derived from the fixed
// server signature combined with a monotonically increasing counter,
// mirroring the C module's split between a fixed init_tags() part and a
// variable calc_crc_suffix() part.
func (s *SLModule) GenTag() string {
	n := s.tagCounter.Add(1)
	sum := md5.Sum([]byte(fmt.Sprintf("%s:%d", slSignature, n)))
	return hex.EncodeToString(sum[:])
}

// SendReply sends a stateless reply, matching C sl_send_reply().
func (s *SLModule) SendReply(msg *parser.SIPMsg, code int, reason string) error {
	return s.sendReply(msg, code, reason, nil, "")
}

// SendReplyWithHeaders sends a stateless reply with extra header lines
// appended after the headers copied from the request.
func (s *SLModule) SendReplyWithHeaders(msg *parser.SIPMsg, code int, reason string, headers []string) error {
	return s.sendReply(msg, code, reason, headers, "")
}

// SendReplyWithBody sends a stateless reply with a body. The Content-Length
// header is set to the body length, replacing any value carried over from
// the request.
func (s *SLModule) SendReplyWithBody(msg *parser.SIPMsg, code int, reason string, body string) error {
	return s.sendReply(msg, code, reason, nil, body)
}

// Stats returns the stateless reply statistics.
func (s *SLModule) Stats() *SLStats {
	return &s.stats
}

// LastBuffer returns the most recently built reply buffer.
func (s *SLModule) LastBuffer() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBuffer
}

// LastReply returns the most recently sent StatelessReply.
func (s *SLModule) LastReply() *StatelessReply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastReply
}

// sendReply is the shared implementation for all SendReply variants. It
// validates the inputs, builds the reply buffer, records the result, and
// updates statistics.
func (s *SLModule) sendReply(msg *parser.SIPMsg, code int, reason string, headers []string, body string) error {
	if msg == nil {
		s.stats.RepliesError.Add(1)
		return errors.New("sl: nil message")
	}
	if reason == "" {
		s.stats.RepliesError.Add(1)
		return errors.New("sl: empty reason phrase")
	}
	if code < 100 || code > 699 {
		s.stats.RepliesError.Add(1)
		return fmt.Errorf("sl: invalid status code %d", code)
	}
	// C: sl_reply_helper rejects ACK requests.
	if msg.IsRequest() && msg.Method() == parser.MethodACK {
		s.stats.RepliesError.Add(1)
		return errors.New("sl: cannot reply to ACK")
	}

	buf, err := s.buildBuffer(msg, code, reason, headers, body)
	if err != nil {
		s.stats.RepliesError.Add(1)
		return err
	}

	reply := &StatelessReply{
		Code:    code,
		Reason:  reason,
		Headers: headers,
		Body:    body,
	}

	s.mu.Lock()
	s.lastReply = reply
	s.lastBuffer = buf
	s.mu.Unlock()

	s.stats.RepliesSent.Add(1)
	return nil
}

// buildBuffer constructs the on-wire reply buffer using
// msgtranslator.BuildResBufFromSIPReq as the base, then applies the
// to-tag, extra headers, and body.
func (s *SLModule) buildBuffer(msg *parser.SIPMsg, code int, reason string, headers []string, body string) ([]byte, error) {
	// C: a to-tag is generated for provisional/final replies (code >= 180)
	// when the To header has no tag.
	var tag string
	if code >= 180 {
		tag = s.GenTag()
	}

	base, _ := msgtranslator.BuildResBufFromSIPReq(code, reason, tag, msg)
	if base == nil {
		return nil, errors.New("sl: failed to build reply buffer")
	}

	// Fast path: no to-tag, no extra headers, no body.
	if tag == "" && len(headers) == 0 && body == "" {
		return base, nil
	}

	// Split the base buffer into status line and header lines so we can
	// inject the to-tag, extra headers, and a corrected Content-Length.
	baseStr := string(base)
	firstCRLF := strings.Index(baseStr, "\r\n")
	if firstCRLF < 0 {
		return base, nil
	}
	statusLine := baseStr[:firstCRLF+2]
	rest := baseStr[firstCRLF+2:]
	// Strip the trailing blank-line CRLF that terminates the header block.
	if strings.HasSuffix(rest, "\r\n") {
		rest = rest[:len(rest)-2]
	}

	var headerLines []string
	if rest != "" {
		headerLines = strings.Split(rest, "\r\n")
	}

	// Add the generated to-tag to the To header (C: build_res_buf_from_sip_req
	// adds the tag when code >= 180 and the To header has no tag).
	if tag != "" {
		for i, h := range headerLines {
			if strings.HasPrefix(strings.ToLower(h), "to:") {
				if !strings.Contains(strings.ToLower(h), ";tag=") {
					headerLines[i] = h + ";tag=" + tag
				}
				break
			}
		}
	}

	// When a body is supplied, replace any existing Content-Length with the
	// correct value for the new body.
	if body != "" {
		filtered := headerLines[:0]
		for _, h := range headerLines {
			if !strings.HasPrefix(strings.ToLower(h), "content-length:") {
				filtered = append(filtered, h)
			}
		}
		headerLines = filtered
		headerLines = append(headerLines, fmt.Sprintf("Content-Length: %d", len(body)))
	}

	// Append caller-supplied extra headers.
	if len(headers) > 0 {
		headerLines = append(headerLines, headers...)
	}

	// Reassemble the buffer.
	var sb strings.Builder
	sb.WriteString(statusLine)
	for _, h := range headerLines {
		sb.WriteString(h)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")
	if body != "" {
		sb.WriteString(body)
	}

	return []byte(sb.String()), nil
}

// --- Package-level default instance ---

var (
	defaultMu sync.RWMutex
	defaultSL *SLModule
)

// Init initializes the default SLModule, replacing any previous instance.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSL = NewSLModule()
}

// DefaultSL returns the default SLModule, initializing one on first use.
func DefaultSL() *SLModule {
	defaultMu.RLock()
	sl := defaultSL
	defaultMu.RUnlock()
	if sl != nil {
		return sl
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSL == nil {
		defaultSL = NewSLModule()
	}
	return defaultSL
}

// SendReply sends a stateless reply using the default SLModule.
func SendReply(msg *parser.SIPMsg, code int, reason string) error {
	return DefaultSL().SendReply(msg, code, reason)
}
