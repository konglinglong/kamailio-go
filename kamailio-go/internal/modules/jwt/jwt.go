// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * jwt module - JSON Web Token signing and verification (HS256).
 *
 * Produces compact JWTs signed with HMAC-SHA256. Sign builds a token
 * from a claims map; Verify checks the signature; Decode returns the
 * claims without verifying. The module is safe for concurrent use.
 */

package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// defaultAlg is the signing algorithm used by this module.
const defaultAlg = "HS256"

// JWTModule signs and verifies HS256 JSON Web Tokens.
type JWTModule struct {
	mu sync.Mutex
}

// New creates a JWTModule.
func New() *JWTModule {
	return &JWTModule{}
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

// Sign creates an HS256 JWT for claims signed with key.
//
//	C: jwt_sign()
func (m *JWTModule) Sign(claims map[string]interface{}, key string) (string, error) {
	if key == "" {
		return "", errors.New("jwt: empty key")
	}
	if claims == nil {
		claims = map[string]interface{}{}
	}
	header := map[string]string{"alg": defaultAlg, "typ": "JWT"}
	headerB, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadB, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64url(headerB) + "." + b64url(payloadB)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64url(mac.Sum(nil)), nil
}

// Verify reports whether token is a well-formed HS256 JWT whose signature
// matches key.
//
//	C: jwt_verify()
func (m *JWTModule) Verify(token string, key string) (bool, error) {
	if key == "" {
		return false, errors.New("jwt: empty key")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, errors.New("jwt: invalid token format")
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(signingInput))
	expected := b64url(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return false, nil
	}
	return true, nil
}

// Decode returns the claims carried by token without verifying the
// signature. It errors on malformed tokens.
//
//	C: jwt_decode()
func (m *JWTModule) Decode(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("jwt: invalid token format")
	}
	payloadB, err := b64urlDecode(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payloadB, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu  sync.RWMutex
	defaultJWT *JWTModule
)

// DefaultJWT returns the process-wide JWTModule, creating it on first use.
func DefaultJWT() *JWTModule {
	defaultMu.RLock()
	m := defaultJWT
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultJWT == nil {
		defaultJWT = New()
	}
	return defaultJWT
}

// Init (re)initialises the process-wide JWTModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultJWT = New()
}
