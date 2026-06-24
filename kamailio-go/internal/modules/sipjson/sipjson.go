// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SipJSON module - SIP message <-> JSON serialisation.
 * Port of the kamailio sipjson module (src/modules/sipjson).
 *
 * ToJSON serialises a SIP message into a JSON document carrying the
 * first-line method/URI/status, the Call-ID and the raw wire payload.
 * FromJSON performs the inverse, re-parsing the raw payload back into a
 * fully populated *parser.SIPMsg so the round-trip is lossless.
 *
 * It is safe for concurrent use.
 */
package sipjson

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// jsonSIP is the on-the-wire representation produced by ToJSON.
type jsonSIP struct {
	Method   string `json:"method,omitempty"`
	RURI     string `json:"ruri,omitempty"`
	Status   int    `json:"status,omitempty"`
	CallID   string `json:"call_id,omitempty"`
	Raw      string `json:"raw"`
}

// SipJSONModule implements the sipjson module functionality.
// C: struct module sipjson
type SipJSONModule struct {
	mu sync.RWMutex
}

// NewSipJSONModule creates a SipJSONModule.
func NewSipJSONModule() *SipJSONModule {
	return &SipJSONModule{}
}

// ToJSON serialises msg into a JSON string. Returns an error when msg is
// nil or has no raw buffer.
// C: sip_to_json()
func (m *SipJSONModule) ToJSON(msg *parser.SIPMsg) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if msg == nil {
		return "", fmt.Errorf("sipjson: nil message")
	}
	if len(msg.Buf) == 0 {
		return "", fmt.Errorf("sipjson: empty message buffer")
	}
	js := jsonSIP{Raw: string(msg.Buf)}
	if msg.FirstLine != nil {
		if msg.FirstLine.Req != nil {
			js.Method = msg.FirstLine.Req.Method.String()
			js.RURI = msg.FirstLine.Req.URI.String()
		}
		if msg.FirstLine.Reply != nil {
			js.Status = int(msg.FirstLine.Reply.StatusCode)
		}
	}
	if msg.CallID != nil {
		js.CallID = msg.CallID.Body.String()
	}
	out, err := json.Marshal(js)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FromJSON parses a JSON document produced by ToJSON back into a
// *parser.SIPMsg. The returned message is fully parsed from the embedded
// raw payload.
// C: sip_from_json()
func (m *SipJSONModule) FromJSON(j string) (*parser.SIPMsg, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if j == "" {
		return nil, fmt.Errorf("sipjson: empty json")
	}
	var js jsonSIP
	if err := json.Unmarshal([]byte(j), &js); err != nil {
		return nil, fmt.Errorf("sipjson: %w", err)
	}
	if js.Raw == "" {
		return nil, fmt.Errorf("sipjson: no raw payload")
	}
	msg, err := parser.ParseMsg([]byte(js.Raw))
	if err != nil {
		return nil, fmt.Errorf("sipjson: %w", err)
	}
	return msg, nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu       sync.RWMutex
	defaultSipJSON *SipJSONModule
)

// DefaultSipJSON returns the process-wide SipJSONModule, creating one on
// first use.
func DefaultSipJSON() *SipJSONModule {
	defaultMu.RLock()
	m := defaultSipJSON
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultSipJSON == nil {
		defaultSipJSON = NewSipJSONModule()
	}
	return defaultSipJSON
}

// Init (re)initialises the process-wide SipJSONModule to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultSipJSON = NewSipJSONModule()
}
