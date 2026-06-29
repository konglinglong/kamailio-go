// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * StirShaken module - STIR/SHAKEN Identity header signing and verification.
 * Port of the kamailio stirshaken module (src/modules/stirshaken).
 *
 * STIR/SHAKEN (RFC 8224/8225/8226) signs SIP requests with a PASSporT
 * token carried in the Identity header so that terminating parties can
 * verify the calling party's identity and attestation level.
 *
 * This implementation produces self-contained PASSporT tokens using a
 * symmetric HMAC key derived from the configured Authority, so that sign
 * and verify round-trip within a single deployment. The token layout
 * follows the JWT shape: base64url(header).base64url(payload).base64url(sig).
 */

package stirshaken

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

// DefaultAlg is the default signing algorithm.
const DefaultAlg = "HS256"

// DefaultAttestation is the default attestation level (A=full).
const DefaultAttestation = "A"

// SHakenConfig holds the configuration for a StirShakenModule.
type SHakenConfig struct {
	Authority       string
	PrivateKeyPath  string
	CertificatePath string
	DefaultAlg      string
}

// SHakenToken is a parsed PASSporT token.
type SHakenToken struct {
	Header    map[string]string
	Payload   map[string]interface{}
	Signature string
}

// StirShakenModule implements STIR/SHAKEN PASSporT signing and Identity
// header handling. It is safe for concurrent use.
type StirShakenModule struct {
	mu     sync.RWMutex
	config SHakenConfig
}

// New creates a StirShakenModule with sensible defaults.
func New() *StirShakenModule {
	return &StirShakenModule{
		config: SHakenConfig{
			Authority:  "kamailio-go",
			DefaultAlg: DefaultAlg,
		},
	}
}

// NewWithConfig creates a StirShakenModule using the supplied configuration.
func NewWithConfig(cfg SHakenConfig) *StirShakenModule {
	if cfg.DefaultAlg == "" {
		cfg.DefaultAlg = DefaultAlg
	}
	return &StirShakenModule{config: cfg}
}

// SetConfig replaces the module configuration.
func (m *StirShakenModule) SetConfig(cfg SHakenConfig) {
	if cfg.DefaultAlg == "" {
		cfg.DefaultAlg = DefaultAlg
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
}

// Config returns a copy of the current configuration.
func (m *StirShakenModule) Config() SHakenConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// alg returns the configured algorithm, falling back to the default.
func (m *StirShakenModule) alg() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config.DefaultAlg == "" {
		return DefaultAlg
	}
	return m.config.DefaultAlg
}

// signingKey derives the HMAC secret from the configured Authority.
func (m *StirShakenModule) signingKey() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.config.Authority
	if a == "" {
		a = "kamailio-go"
	}
	return []byte(a)
}

// identityInfo returns the Identity-Info URI advertised in the Identity
// header, derived from the configured Authority.
func (m *StirShakenModule) identityInfo() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.config.Authority
	if a == "" {
		a = "kamailio-go"
	}
	return "https://" + a + "/cert.pem"
}

// Sign creates a PASSporT token for msg and returns it. The originating
// and destination telephone numbers are embedded in the token payload.
func (m *StirShakenModule) Sign(msg *parser.SIPMsg, origTN, destTN string) (string, error) {
	if msg == nil {
		return "", errors.New("stirshaken: nil message")
	}
	return m.BuildPassportToken(origTN, destTN, DefaultAttestation)
}

// Verify validates the Identity header carried by msg. It returns true
// when the header is present, well-formed and the signature checks out.
func (m *StirShakenModule) Verify(msg *parser.SIPMsg) (bool, error) {
	if msg == nil {
		return false, errors.New("stirshaken: nil message")
	}
	if !m.CheckIdentityHeader(msg) {
		return false, nil
	}
	hdr := msg.Identity
	if hdr == nil {
		hdr = msg.GetHeaderByType(parser.HdrIdentity)
	}
	if hdr == nil {
		return false, nil
	}
	token := extractToken(hdr.Body.String())
	if token == "" {
		return false, errors.New("stirshaken: empty identity token")
	}
	return m.verifyToken(token)
}

// BuildPassportToken builds a signed PASSporT JWT for the given parties
// and attestation level (A, B or C).
func (m *StirShakenModule) BuildPassportToken(origTN, destTN, attestation string) (string, error) {
	if origTN == "" || destTN == "" {
		return "", errors.New("stirshaken: orig/dest telephone number required")
	}
	if attestation == "" {
		attestation = DefaultAttestation
	}
	alg := m.alg()

	header := map[string]string{
		"alg": alg,
		"ppt": "shaken",
		"typ": "passport",
	}
	payload := map[string]interface{}{
		"att":  attestation,
		"iat":  time.Now().Unix(),
		"orig": map[string]string{"tn": origTN},
		"dest": map[string][]string{"tn": {destTN}},
	}

	headerB, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("stirshaken: marshal header: %w", err)
	}
	payloadB, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("stirshaken: marshal payload: %w", err)
	}

	headerEnc := b64url(headerB)
	payloadEnc := b64url(payloadB)
	signingInput := headerEnc + "." + payloadEnc
	sig := m.sign(signingInput)

	return signingInput + "." + sig, nil
}

// ParsePassportToken decodes a PASSporT JWT into its header, payload and
// signature parts. The signature is returned as the raw base64url string.
func (m *StirShakenModule) ParsePassportToken(token string) (*SHakenToken, error) {
	if token == "" {
		return nil, errors.New("stirshaken: empty token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("stirshaken: invalid token format")
	}
	headerB, err := b64urlDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("stirshaken: decode header: %w", err)
	}
	payloadB, err := b64urlDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("stirshaken: decode payload: %w", err)
	}

	var header map[string]string
	if err := json.Unmarshal(headerB, &header); err != nil {
		return nil, fmt.Errorf("stirshaken: unmarshal header: %w", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadB, &payload); err != nil {
		return nil, fmt.Errorf("stirshaken: unmarshal payload: %w", err)
	}
	return &SHakenToken{
		Header:    header,
		Payload:   payload,
		Signature: parts[2],
	}, nil
}

// AddIdentityHeader attaches an Identity header carrying token to msg.
// It also sets the Identity-Info quick reference. Returns 0 on success
// or -1 when msg or token is empty.
func (m *StirShakenModule) AddIdentityHeader(msg *parser.SIPMsg, token string) int {
	if msg == nil || token == "" {
		return -1
	}
	alg := m.alg()
	value := token + ";info=<" + m.identityInfo() + ">;alg=" + alg + ";ppt=shaken"
	hdr := msg.AddHeader("Identity", value)
	if msg.Identity == nil {
		msg.Identity = hdr
	}
	return 0
}

// CheckIdentityHeader reports whether msg carries an Identity header
// whose PASSporT token verifies against the configured key.
func (m *StirShakenModule) CheckIdentityHeader(msg *parser.SIPMsg) bool {
	if msg == nil {
		return false
	}
	hdr := msg.Identity
	if hdr == nil {
		hdr = msg.GetHeaderByType(parser.HdrIdentity)
	}
	if hdr == nil {
		return false
	}
	token := extractToken(hdr.Body.String())
	if token == "" {
		return false
	}
	ok, err := m.verifyToken(token)
	return err == nil && ok
}

// sign computes the HMAC-SHA256 signature of signingInput using the
// configured key, returning it as a base64url string.
func (m *StirShakenModule) sign(signingInput string) string {
	mac := hmac.New(sha256.New, m.signingKey())
	mac.Write([]byte(signingInput))
	return b64url(mac.Sum(nil))
}

// verifyToken recomputes the signature for the token's header.payload
// and compares it to the carried signature in constant time.
func (m *StirShakenModule) verifyToken(token string) (bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, errors.New("stirshaken: invalid token format")
	}
	expected := m.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return false, nil
	}
	return true, nil
}

// extractToken returns the PASSporT token portion of an Identity header
// body (everything before the first ';').
func extractToken(body string) string {
	body = strings.TrimSpace(body)
	if i := strings.IndexByte(body, ';'); i >= 0 {
		return strings.TrimSpace(body[:i])
	}
	return body
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
	defaultM  *StirShakenModule
)

// DefaultStirShaken returns the process-wide StirShakenModule, creating
// it on first use.
func DefaultStirShaken() *StirShakenModule {
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

// Init (re)initialises the process-wide StirShakenModule to a fresh
// state, mirroring Kamailio's mod_init. It is safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Sign is the package-level wrapper around DefaultStirShaken().Sign.
func Sign(msg *parser.SIPMsg, origTN, destTN string) (string, error) {
	return DefaultStirShaken().Sign(msg, origTN, destTN)
}

// Verify is the package-level wrapper around DefaultStirShaken().Verify.
func Verify(msg *parser.SIPMsg) (bool, error) {
	return DefaultStirShaken().Verify(msg)
}

// AddIdentityHeader is the package-level wrapper.
func AddIdentityHeader(msg *parser.SIPMsg, token string) int {
	return DefaultStirShaken().AddIdentityHeader(msg, token)
}
