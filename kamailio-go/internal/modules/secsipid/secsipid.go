// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * SecSipID module - SIP Identity signing and verification (RFC 8224).
 * Port of the kamailio secsipid module (src/modules/secsipid).
 *
 * secsipid signs and verifies the SIP Identity header using a compact
 * JWT. The signing key is taken from the configured PrivateKey (used as
 * an HMAC secret) and verified against the configured PublicKey (which,
 * for symmetric operation, equals the PrivateKey).
 *
 * Token layout: base64url(header).base64url(payload).base64url(signature).
 */

package secsipid

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// DefaultExpire is the default token lifetime in seconds.
const DefaultExpire = 60

// DefaultAlg is the default signing algorithm.
const DefaultAlg = "HS256"

// SecSipIDConfig holds the configuration for a SecSipIDModule.
type SecSipIDConfig struct {
	PrivateKey    string
	PublicKey     string
	DefaultExpire int
}

// SecSipIDModule signs and verifies SIP Identity JWTs. It is safe for
// concurrent use.
type SecSipIDModule struct {
	mu     sync.RWMutex
	config SecSipIDConfig
}

// New creates a SecSipIDModule with sensible defaults.
func New() *SecSipIDModule {
	return &SecSipIDModule{
		config: SecSipIDConfig{
			PrivateKey:    "kamailio-go-secret",
			PublicKey:     "kamailio-go-secret",
			DefaultExpire: DefaultExpire,
		},
	}
}

// NewWithConfig creates a SecSipIDModule using the supplied configuration.
func NewWithConfig(cfg SecSipIDConfig) *SecSipIDModule {
	if cfg.DefaultExpire <= 0 {
		cfg.DefaultExpire = DefaultExpire
	}
	return &SecSipIDModule{config: cfg}
}

// SetConfig replaces the module configuration.
func (m *SecSipIDModule) SetConfig(cfg SecSipIDConfig) {
	if cfg.DefaultExpire <= 0 {
		cfg.DefaultExpire = DefaultExpire
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// Config returns a copy of the current configuration.
func (m *SecSipIDModule) Config() SecSipIDConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// expire returns the configured token lifetime in seconds.
func (m *SecSipIDModule) expire() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config.DefaultExpire <= 0 {
		return DefaultExpire
	}
	return m.config.DefaultExpire
}

// signingKey returns the HMAC secret derived from the configured PrivateKey.
func (m *SecSipIDModule) signingKey() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k := m.config.PrivateKey
	if k == "" {
		k = "kamailio-go-secret"
	}
	return []byte(k)
}

// verifyKey returns the HMAC key used to verify tokens, derived from the
// configured PublicKey (which equals the PrivateKey for symmetric use).
func (m *SecSipIDModule) verifyKey() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k := m.config.PublicKey
	if k == "" {
		k = m.config.PrivateKey
	}
	if k == "" {
		k = "kamailio-go-secret"
	}
	return []byte(k)
}

// Sign builds a signed JWT for the given originating identity and
// destination telephone number. origID is the origination identifier
// (e.g. a UUID) embedded in the payload.
func (m *SecSipIDModule) Sign(origID, origTN, destTN string) (string, error) {
	if origID == "" {
		return "", errors.New("secsipid: origination id required")
	}
	if origTN == "" || destTN == "" {
		return "", errors.New("secsipid: orig/dest telephone number required")
	}
	now := time.Now().Unix()
	exp := now + int64(m.expire())

	header := map[string]string{
		"alg": DefaultAlg,
		"typ": "passport",
	}
	payload := map[string]interface{}{
		"orig": map[string]string{"tn": origTN, "id": origID},
		"dest": map[string][]string{"tn": {destTN}},
		"iat":  now,
		"exp":  exp,
	}

	headerB, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("secsipid: marshal header: %w", err)
	}
	payloadB, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("secsipid: marshal payload: %w", err)
	}

	headerEnc := b64url(headerB)
	payloadEnc := b64url(payloadB)
	signingInput := headerEnc + "." + payloadEnc
	sig := m.sign(signingInput)
	return signingInput + "." + sig, nil
}

// Verify validates a JWT signature against the configured key. It returns
// true when the signature is valid and the token has not expired.
func (m *SecSipIDModule) Verify(token string) (bool, error) {
	if token == "" {
		return false, errors.New("secsipid: empty token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, errors.New("secsipid: invalid token format")
	}
	expected := m.signWith(m.verifyKey(), parts[0]+"."+parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return false, nil
	}
	// Check expiry if present.
	if payloadB, err := b64urlDecode(parts[1]); err == nil {
		var payload map[string]interface{}
		if err := json.Unmarshal(payloadB, &payload); err == nil {
			if exp, ok := payload["exp"].(float64); ok {
				if time.Now().Unix() >= int64(exp) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// CheckIdentity extracts the Identity header from msg and verifies its
// token. Returns true when the header is present and valid.
func (m *SecSipIDModule) CheckIdentity(msg *parser.SIPMsg) (bool, error) {
	if msg == nil {
		return false, errors.New("secsipid: nil message")
	}
	hdr := msg.Identity
	if hdr == nil {
		hdr = msg.GetHeaderByType(parser.HdrIdentity)
	}
	if hdr == nil {
		return false, nil
	}
	token := ParseIdentityToken(hdr.Body.String())
	if token == "" {
		return false, errors.New("secsipid: empty identity token")
	}
	return m.Verify(token)
}

// BuildIdentityHeader builds an Identity header value carrying token with
// the standard info/alg parameters.
func (m *SecSipIDModule) BuildIdentityHeader(token string) string {
	if token == "" {
		return ""
	}
	return token + ";info=<https://kamailio-go/cert.pem>;alg=" + DefaultAlg + ";ppt=shaken"
}

// ParseIdentity extracts the raw JWT token from an Identity header value
// (the part before the first ';'). Returns an error when the header is
// empty or carries no token.
func ParseIdentity(header string) (string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", errors.New("secsipid: empty identity header")
	}
	if i := strings.IndexByte(header, ';'); i >= 0 {
		token := strings.TrimSpace(header[:i])
		if token == "" {
			return "", errors.New("secsipid: missing identity token")
		}
		return token, nil
	}
	return header, nil
}

// ParseIdentityToken is an alias for ParseIdentity that never returns an
// error (returning "" when the token cannot be extracted).
func ParseIdentityToken(header string) string {
	tok, _ := ParseIdentity(header)
	return tok
}

// sign computes the HMAC-SHA256 signature of signingInput using the
// configured signing key.
func (m *SecSipIDModule) sign(signingInput string) string {
	return m.signWith(m.signingKey(), signingInput)
}

// signWith computes the HMAC-SHA256 signature of signingInput using key.
func (m *SecSipIDModule) signWith(key []byte, signingInput string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	return b64url(mac.Sum(nil))
}

// b64url encodes b using base64url without padding (JWT encoding).
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
	defaultM  *SecSipIDModule
)

// DefaultSecSipID returns the process-wide SecSipIDModule, creating it
// on first use.
func DefaultSecSipID() *SecSipIDModule {
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

// Init (re)initialises the process-wide SecSipIDModule to a fresh state,
// mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Sign is the package-level wrapper around DefaultSecSipID().Sign.
func Sign(origID, origTN, destTN string) (string, error) {
	return DefaultSecSipID().Sign(origID, origTN, destTN)
}

// Verify is the package-level wrapper around DefaultSecSipID().Verify.
func Verify(token string) (bool, error) {
	return DefaultSecSipID().Verify(token)
}

// CheckIdentity is the package-level wrapper.
func CheckIdentity(msg *parser.SIPMsg) (bool, error) {
	return DefaultSecSipID().CheckIdentity(msg)
}
