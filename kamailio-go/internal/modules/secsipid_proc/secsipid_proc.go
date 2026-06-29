// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SecSIPIDProc module - SIP Identity signing/verification via an external
 * key process. Port of the kamailio secsipid_proc module
 * (src/modules/secsipid_proc).
 *
 * secsipid_proc signs and verifies JWT-like tokens using a key loaded from
 * a file (keyPath). The key file contents are used as the HMAC-SHA256
 * secret. Init loads the key; Sign produces a token for a payload; Verify
 * checks a token's signature.
 *
 * Token layout: base64url(payload).base64url(signature).
 *
 * The module is safe for concurrent use.
 */

package secsipid_proc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultExpire is the default token lifetime in seconds.
const DefaultExpire = 60

// SecSIPIDProcModule signs and verifies tokens using a file-based key.
type SecSIPIDProcModule struct {
	mu      sync.RWMutex
	key     []byte
	keyPath string
	expire  int
}

// New creates a SecSIPIDProcModule with no key loaded.
func New() *SecSIPIDProcModule {
	return &SecSIPIDProcModule{expire: DefaultExpire}
}

// Init loads the signing key from keyPath. A non-existent or empty file is
// an error. It mirrors Kamailio's mod_init.
func (m *SecSIPIDProcModule) Init(keyPath string) error {
	if keyPath == "" {
		return errors.New("secsipid_proc: empty key path")
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("secsipid_proc: empty key file")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.key = data
	m.keyPath = keyPath
	return nil
}

// IsInitialized reports whether a key has been loaded.
func (m *SecSIPIDProcModule) IsInitialized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.key) > 0
}

// SetExpire sets the token lifetime in seconds.
func (m *SecSIPIDProcModule) SetExpire(seconds int) {
	if seconds <= 0 {
		seconds = DefaultExpire
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire = seconds
}

// keyBytes returns the loaded key (or a default when none loaded).
func (m *SecSIPIDProcModule) keyBytes() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.key) > 0 {
		return m.key
	}
	return []byte("secsipid-proc-default")
}

// expireSec returns the configured token lifetime.
func (m *SecSIPIDProcModule) expireSec() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.expire <= 0 {
		return DefaultExpire
	}
	return m.expire
}

// Sign produces a signed token for payload. The payload is wrapped with an
// issued-at and expiry timestamp before signing.
func (m *SecSIPIDProcModule) Sign(payload string) (string, error) {
	if payload == "" {
		return "", errors.New("secsipid_proc: empty payload")
	}
	now := time.Now().Unix()
	body := map[string]interface{}{
		"payload": payload,
		"iat":     now,
		"exp":     now + int64(m.expireSec()),
	}
	bodyB, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	payloadEnc := b64url(bodyB)
	sig := m.sign(payloadEnc)
	return payloadEnc + "." + sig, nil
}

// Verify checks a token's signature and expiry. Returns true when the
// signature is valid and the token has not expired.
func (m *SecSIPIDProcModule) Verify(token string) (bool, error) {
	if token == "" {
		return false, errors.New("secsipid_proc: empty token")
	}
	dot := -1
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 {
		return false, errors.New("secsipid_proc: invalid token format")
	}
	payloadEnc := token[:dot]
	sig := token[dot+1:]
	expected := m.sign(payloadEnc)
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return false, nil
	}
	if bodyB, err := b64urlDecode(payloadEnc); err == nil {
		var body map[string]interface{}
		if err := json.Unmarshal(bodyB, &body); err == nil {
			if exp, ok := body["exp"].(float64); ok {
				if time.Now().Unix() >= int64(exp) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// sign computes the HMAC-SHA256 signature of input using the loaded key.
func (m *SecSIPIDProcModule) sign(input string) string {
	mac := hmac.New(sha256.New, m.keyBytes())
	mac.Write([]byte(input))
	return b64url(mac.Sum(nil))
}

// b64url encodes b using base64url without padding.
func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// b64urlDecode decodes a base64url string (padding optional).
func b64urlDecode(s string) ([]byte, error) {
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.URLEncoding.DecodeString(s)
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *SecSIPIDProcModule
)

// DefaultSecSIPIDProc returns the process-wide module, creating it on first use.
func DefaultSecSIPIDProc() *SecSIPIDProcModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init is the package-level (re)initialiser that loads the key file.
func Init(keyPath string) error { return DefaultSecSIPIDProc().Init(keyPath) }

// Sign is the package-level wrapper.
func Sign(payload string) (string, error) { return DefaultSecSIPIDProc().Sign(payload) }

// Verify is the package-level wrapper.
func Verify(token string) (bool, error) { return DefaultSecSIPIDProc().Verify(token) }
