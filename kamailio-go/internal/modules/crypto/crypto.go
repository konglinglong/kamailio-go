// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * crypto module - generic cryptographic helpers.
 *
 * Provides AES-256-GCM authenticated encryption, SHA-256 hashing and
 * HMAC-SHA256 signing/verification. Encryption keys are derived from the
 * caller-supplied key string via SHA-256 so that any-length passphrase
 * can be used. The module is safe for concurrent use.
 */

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// CryptoModule provides symmetric encryption, hashing and signing.
type CryptoModule struct {
	mu sync.Mutex
}

// New creates a CryptoModule.
func New() *CryptoModule {
	return &CryptoModule{}
}

// deriveKey returns a 32-byte AES-256 key derived from key via SHA-256.
func deriveKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// Encrypt encrypts data using AES-256-GCM with a key derived from key.
// The returned slice is nonce || ciphertext. An empty key is rejected.
//
//	C: crypto_encrypt()
func (m *CryptoModule) Encrypt(data []byte, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("crypto: empty key")
	}
	if len(data) == 0 {
		return nil, errors.New("crypto: empty data")
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

// Decrypt decrypts a nonce||ciphertext blob produced by Encrypt using a
// key derived from key.
//
//	C: crypto_decrypt()
func (m *CryptoModule) Decrypt(data []byte, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("crypto: empty key")
	}
	if len(data) == 0 {
		return nil, errors.New("crypto: empty data")
	}
	block, err := aes.NewCipher(deriveKey(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return nil, errors.New("crypto: ciphertext too short")
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}

// Hash returns the hex-encoded SHA-256 digest of data.
//
//	C: crypto_hash()
func (m *CryptoModule) Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Sign returns the hex-encoded HMAC-SHA256 of data keyed by key.
//
//	C: crypto_sign()
func (m *CryptoModule) Sign(data []byte, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("crypto: empty key")
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(data)
	return []byte(hex.EncodeToString(mac.Sum(nil))), nil
}

// Verify reports whether sig is a valid HMAC-SHA256 of data keyed by key.
//
//	C: crypto_verify()
func (m *CryptoModule) Verify(data, sig []byte, key string) bool {
	if key == "" || len(sig) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(data)
	expected := []byte(hex.EncodeToString(mac.Sum(nil)))
	return hmac.Equal(expected, sig)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultCrypto *CryptoModule
)

// DefaultCrypto returns the process-wide CryptoModule, creating it on
// first use.
func DefaultCrypto() *CryptoModule {
	defaultMu.RLock()
	m := defaultCrypto
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultCrypto == nil {
		defaultCrypto = New()
	}
	return defaultCrypto
}

// Init (re)initialises the process-wide CryptoModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultCrypto = New()
}
