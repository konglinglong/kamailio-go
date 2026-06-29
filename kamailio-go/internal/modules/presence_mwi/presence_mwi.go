// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * presence_mwi module - Message Waiting Indication.
 *
 * Port of the kamailio presence_mwi module
 * (src/modules/presence_mwi). An MWIModule parses and builds RFC 3842
 * message-summary bodies and tracks the current MWI state per mailbox.
 *
 * The message-summary body has the form:
 *
 *	Messages-Waiting: yes
 *	Message-Account: sip:alice@example.com
 *	Voice-Message: 2/1 (1/0)
 *
 * where Voice-Message is new/old (new-urgent/old-urgent).
 *
 * The module is safe for concurrent use.
 */
package presence_mwi

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// MWIInfo represents the message-waiting state of a single mailbox.
type MWIInfo struct {
	Mailbox           string
	NewMessages       int
	OldMessages       int
	NewUrgentMessages int
	MessageAccount    string
	VoiceMessageURL   string
}

// MWIModule stores MWI state per mailbox and parses/builds message-summary
// bodies. It mirrors the C presence_mwi module, which registers the
// "message-summary" event package with the presence server.
type MWIModule struct {
	mu        sync.RWMutex
	mailboxes map[string]*MWIInfo
}

// NewMWIModule creates an MWIModule with empty mailbox storage.
func NewMWIModule() *MWIModule {
	return &MWIModule{mailboxes: make(map[string]*MWIInfo)}
}

// ParseMWIBody parses a message-summary body into an MWIInfo. Missing
// fields default to zero values; the function only returns an error
// when the body contains no recognisable message-summary lines.
func (m *MWIModule) ParseMWIBody(body string) (*MWIInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info := &MWIInfo{}
	found := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		switch strings.ToLower(key) {
		case "messages-waiting":
			found = true
		case "message-account":
			info.MessageAccount = val
			if info.Mailbox == "" {
				info.Mailbox = val
			}
			found = true
		case "voice-message":
			newMsg, oldMsg, newUrgent := parseVoiceMessage(val)
			info.NewMessages = newMsg
			info.OldMessages = oldMsg
			info.NewUrgentMessages = newUrgent
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("presence_mwi: no message-summary lines in body")
	}
	return info, nil
}

// BuildMWIBody builds a message-summary body from an MWIInfo.
// Messages-Waiting is "yes" when NewMessages is positive.
func (m *MWIModule) BuildMWIBody(info *MWIInfo) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if info == nil {
		return ""
	}
	waiting := "no"
	if info.NewMessages > 0 {
		waiting = "yes"
	}
	account := info.MessageAccount
	if account == "" {
		account = info.Mailbox
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Messages-Waiting: %s\r\n", waiting))
	b.WriteString(fmt.Sprintf("Message-Account: %s\r\n", account))
	b.WriteString(fmt.Sprintf("Voice-Message: %d/%d (%d/0)\r\n",
		info.NewMessages, info.OldMessages, info.NewUrgentMessages))
	return b.String()
}

// Notify stores info for mailbox, replacing any existing entry. A copy
// of info is stored so later caller mutations do not affect the module.
func (m *MWIModule) Notify(mailbox string, info *MWIInfo) error {
	if mailbox == "" {
		return fmt.Errorf("presence_mwi: empty mailbox")
	}
	if info == nil {
		return fmt.Errorf("presence_mwi: nil info")
	}
	cp := *info
	if cp.Mailbox == "" {
		cp.Mailbox = mailbox
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mailboxes[mailbox] = &cp
	return nil
}

// GetMWI returns the MWIInfo for mailbox, or nil if none exists. The
// returned pointer is a snapshot that remains valid after the lock is
// released because Notify stores fresh copies.
func (m *MWIModule) GetMWI(mailbox string) *MWIInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mailboxes[mailbox]
}

// ClearMWI removes the MWI state for mailbox. Returns true if an entry
// was removed.
func (m *MWIModule) ClearMWI(mailbox string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mailboxes[mailbox]; !ok {
		return false
	}
	delete(m.mailboxes, mailbox)
	return true
}

// Count returns the number of tracked mailboxes.
func (m *MWIModule) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.mailboxes)
}

// parseVoiceMessage parses a "new/old (new-urgent/old-urgent)" value
// into its three numeric components. Missing parts default to 0.
func parseVoiceMessage(val string) (newMsg, oldMsg, newUrgent int) {
	main := val
	urgent := ""
	if i := strings.Index(val, "("); i >= 0 {
		main = strings.TrimSpace(val[:i])
		rest := val[i+1:]
		if j := strings.Index(rest, ")"); j >= 0 {
			urgent = rest[:j]
		}
	}
	newMsg, oldMsg = splitIntPair(main)
	newUrgent, _ = splitIntPair(urgent)
	return newMsg, oldMsg, newUrgent
}

// splitIntPair splits "a/b" into two ints, defaulting to 0.
func splitIntPair(s string) (int, int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0
	}
	parts := strings.SplitN(s, "/", 2)
	a, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	b := 0
	if len(parts) > 1 {
		b, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	return a, b
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu  sync.RWMutex
	defaultMWI *MWIModule
)

// DefaultMWI returns the process-wide MWIModule, creating one on first
// use.
func DefaultMWI() *MWIModule {
	defaultMu.RLock()
	m := defaultMWI
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultMWI == nil {
		defaultMWI = NewMWIModule()
	}
	return defaultMWI
}

// Init (re)initialises the process-wide MWIModule to a fresh state,
// mirroring Kamailio's mod_init. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultMWI = NewMWIModule()
}
