// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * gcrypt module - libgcrypt-style cryptographic helpers.
 *
 * Mirrors the subset of libgcrypt used by the kamailio gcrypt module:
 * AES-256-CBC authenticated encryption (here AES-256-GCM for integrity),
 * SHA-256 hashing and HMAC-SHA256. Keys are derived from the caller's
 * key string via SHA-256. The module is safe for concurrent use.
 */

package gcrypt

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

// GCryptModule provides AES-256 encryption, SHA-256 and HMAC helpers.
type GCryptModule struct {
	mu sync.Mutex
}

// New creates a GCryptModule.
func New() *GCryptModule {
	return &GCryptModule{}
}

// deriveKey returns a 32-byte AES-256 key derived from key via SHA-256.
func deriveKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// AES256Encrypt encrypts data using AES-256-GCM with a key derived from
// key. The returned slice is nonce || ciphertext.
//
//	C: gcrypt_aes256_encrypt()
func (m *GCryptModule) AES256Encrypt(data []byte, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("gcrypt: empty key")
	}
	if len(data) == 0 {
		return nil, errors.New("gcrypt: empty data")
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

// AES256Decrypt decrypts a nonce||ciphertext blob produced by
// AES256Encrypt using a key derived from key.
//
//	C: gcrypt_aes256_decrypt()
func (m *GCryptModule) AES256Decrypt(data []byte, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.New("gcrypt: empty key")
	}
	if len(data) == 0 {
		return nil, errors.New("gcrypt: empty data")
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
		return nil, errors.New("gcrypt: ciphertext too short")
	}
	return gcm.Open(nil, data[:ns], data[ns:], nil)
}

// SHA256 returns the hex-encoded SHA-256 digest of data.
//
//	C: gcrypt_sha256()
func (m *GCryptModule) SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HMAC returns the hex-encoded HMAC-SHA256 of data keyed by key.
//
//	C: gcrypt_hmac()
func (m *GCryptModule) HMAC(data []byte, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu     sync.RWMutex
	defaultGCrypt *GCryptModule
)

// DefaultGCrypt returns the process-wide GCryptModule, creating it on
// first use.
func DefaultGCrypt() *GCryptModule {
	defaultMu.RLock()
	m := defaultGCrypt
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultGCrypt == nil {
		defaultGCrypt = New()
	}
	return defaultGCrypt
}

// Init (re)initialises the process-wide GCryptModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultGCrypt = New()
}
