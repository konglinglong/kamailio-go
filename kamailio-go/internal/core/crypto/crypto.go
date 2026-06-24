// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * core/crypto - cryptographic helper functions.
 *
 * Mirrors the C sources in src/core/crypto/{md5utils,shautils}.c and
 * provides hashing (MD5/SHA1/SHA256/SHA3), HMAC, AES-256-GCM authenticated
 * encryption, random nonce/UUID generation, base64 and CRC32 helpers.
 * All functions are safe for concurrent use; they rely only on the Go
 * standard library.
 */
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
)

// MD5 computes the MD5 digest of data and returns the 16-byte hash.
//
//	C: MD5StringArray() in src/core/crypto/md5utils.c
func MD5(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}

// MD5Hex returns the MD5 digest of data as a lowercase hex string.
func MD5Hex(data []byte) string {
	return hex.EncodeToString(MD5(data))
}

// SHA1 computes the SHA-1 digest of data and returns the 20-byte hash.
//
//	C: compute_sha1() in src/core/crypto/shautils.c
func SHA1(data []byte) []byte {
	sum := sha1.Sum(data)
	return sum[:]
}

// SHA1Hex returns the SHA-1 digest of data as a lowercase hex string.
func SHA1Hex(data []byte) string {
	return hex.EncodeToString(SHA1(data))
}

// SHA256 computes the SHA-256 digest of data and returns the 32-byte hash.
func SHA256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// SHA256Hex returns the SHA-256 digest of data as a lowercase hex string.
func SHA256Hex(data []byte) string {
	return hex.EncodeToString(SHA256(data))
}

// SHA3 computes the SHA3-256 digest of data and returns the 32-byte hash.
func SHA3(data []byte) []byte {
	sum := sha3.Sum256(data)
	return sum[:]
}

// SHA3Hex returns the SHA3-256 digest of data as a lowercase hex string.
func SHA3Hex(data []byte) string {
	return hex.EncodeToString(SHA3(data))
}

// HMACMD5 returns the HMAC-MD5 of data keyed by key.
func HMACMD5(key, data []byte) []byte {
	mac := hmac.New(md5.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// HMACSHA1 returns the HMAC-SHA1 of data keyed by key.
func HMACSHA1(key, data []byte) []byte {
	mac := hmac.New(sha1.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// HMACSHA256 returns the HMAC-SHA256 of data keyed by key.
func HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// AES256Encrypt encrypts plaintext with AES-256-GCM. The key must be
// exactly 32 bytes. The returned slice is nonce || ciphertext || tag.
func AES256Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: AES-256 key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
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
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// AES256Decrypt decrypts a nonce||ciphertext||tag blob produced by
// AES256Encrypt using the same 32-byte key.
func AES256Decrypt(key, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: AES-256 key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("crypto: ciphertext too short")
	}
	ns := gcm.NonceSize()
	return gcm.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}

// GenerateNonce returns length cryptographically secure random bytes.
// A negative length is rejected with an error.
func GenerateNonce(length int) ([]byte, error) {
	if length < 0 {
		return nil, fmt.Errorf("crypto: nonce length must be non-negative, got %d", length)
	}
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// GenerateUUID returns a random RFC 4122 version 4 UUID string in the
// canonical 8-4-4-4-12 hex form, e.g. "f47ac10b-58cc-4372-a567-0e02b2c3d479".
func GenerateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read only errors on a broken reader; fall back to a
		// zeroed buffer rather than panicking so the function is total.
		return "00000000-0000-4000-8000-000000000000"
	}
	// Set version 4.
	b[6] = (b[6] & 0x0f) | 0x40
	// Set variant to RFC 4122 (10xx).
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}

// Base64Encode returns the standard base64 encoding of data.
func Base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// Base64Decode decodes a standard base64 string.
func Base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// CRC32 returns the IEEE 802.3 CRC32 checksum of data.
func CRC32(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}
