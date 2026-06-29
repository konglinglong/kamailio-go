// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * jwt3 module - JSON Web Token signing and verification (RS256).
 *
 * Like the jwt module but defaults to RS256 (RSA-SHA256) signing. Sign
 * accepts a PEM-encoded RSA private key; Verify accepts a PEM-encoded
 * RSA public key. The module is safe for concurrent use.
 */

package jwt3

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"sync"
)

// defaultAlg is the signing algorithm used by this module.
const defaultAlg = "RS256"

// JWT3Module signs and verifies RS256 JSON Web Tokens.
type JWT3Module struct {
	mu sync.Mutex
}

// New creates a JWT3Module.
func New() *JWT3Module {
	return &JWT3Module{}
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

// parsePrivateKey parses a PEM-encoded RSA private key (PKCS#1 or PKCS#8).
func parsePrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("jwt3: failed to decode private key PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
	}
	return nil, errors.New("jwt3: unsupported private key format")
}

// parsePublicKey parses a PEM-encoded RSA public key (PKCS#1 or PKIX).
func parsePublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("jwt3: failed to decode public key PEM")
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
	}
	return nil, errors.New("jwt3: unsupported public key format")
}

// Sign creates an RS256 JWT for claims signed with privateKey (PEM).
//
//	C: jwt3_sign()
func (m *JWT3Module) Sign(claims map[string]interface{}, privateKey string) (string, error) {
	key, err := parsePrivateKey(privateKey)
	if err != nil {
		return "", err
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
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + b64url(sig), nil
}

// Verify reports whether token is a well-formed RS256 JWT whose signature
// matches publicKey (PEM).
//
//	C: jwt3_verify()
func (m *JWT3Module) Verify(token string, publicKey string) (bool, error) {
	key, err := parsePublicKey(publicKey)
	if err != nil {
		return false, err
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false, errors.New("jwt3: invalid token format")
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := b64urlDecode(parts[2])
	if err != nil {
		return false, err
	}
	hashed := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, hashed[:], sig); err != nil {
		return false, nil
	}
	return true, nil
}

// Decode returns the claims carried by token without verifying the
// signature. It errors on malformed tokens.
//
//	C: jwt3_decode()
func (m *JWT3Module) Decode(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("jwt3: invalid token format")
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
	defaultMu   sync.RWMutex
	defaultJWT3 *JWT3Module
)

// DefaultJWT3 returns the process-wide JWT3Module, creating it on first use.
func DefaultJWT3() *JWT3Module {
	defaultMu.RLock()
	m := defaultJWT3
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultJWT3 == nil {
		defaultJWT3 = New()
	}
	return defaultJWT3
}

// Init (re)initialises the process-wide JWT3Module to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultJWT3 = New()
}
