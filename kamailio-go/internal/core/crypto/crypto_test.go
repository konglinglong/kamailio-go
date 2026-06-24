// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - core crypto helpers tests.
 *
 * Mirrors the C sources in src/core/crypto/{md5utils,shautils}.c.
 */
package crypto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// TestMD5 verifies MD5 against well-known RFC 1321 test vectors.
func TestMD5(t *testing.T) {
	cases := []struct {
		in   string
		hex  string
	}{
		{"", "d41d8cd98f00b204e9800998ecf8427e"},
		{"abc", "900150983cd24fb0d6963f7d28e17f72"},
		{"message digest", "f96b697d7cb7938d525a2f31aaf161d0"},
	}
	for _, c := range cases {
		got := MD5Hex([]byte(c.in))
		if got != c.hex {
			t.Errorf("MD5Hex(%q) = %q, want %q", c.in, got, c.hex)
		}
		raw := MD5([]byte(c.in))
		if len(raw) != 16 {
			t.Errorf("MD5(%q) len = %d, want 16", c.in, len(raw))
		}
		if hex.EncodeToString(raw) != c.hex {
			t.Errorf("MD5(%q) hex mismatch", c.in)
		}
	}
}

// TestSHA256 verifies SHA-256 against FIPS 180-2 test vectors.
func TestSHA256(t *testing.T) {
	cases := []struct {
		in  string
		hex string
	}{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, c := range cases {
		if got := SHA256Hex([]byte(c.in)); got != c.hex {
			t.Errorf("SHA256Hex(%q) = %q, want %q", c.in, got, c.hex)
		}
		raw := SHA256([]byte(c.in))
		if len(raw) != 32 {
			t.Errorf("SHA256(%q) len = %d, want 32", c.in, len(raw))
		}
	}
	// SHA-1 sanity check against known vector.
	if got := SHA1Hex([]byte("abc")); got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Errorf("SHA1Hex(abc) = %q", got)
	}
	if len(SHA1([]byte("abc"))) != 20 {
		t.Error("SHA1 should produce 20 bytes")
	}
}

// TestSHA3 verifies SHA3-256 against NIST test vectors.
func TestSHA3(t *testing.T) {
	cases := []struct {
		in  string
		hex string
	}{
		{"", "a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a"},
		{"abc", "3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532"},
	}
	for _, c := range cases {
		if got := SHA3Hex([]byte(c.in)); got != c.hex {
			t.Errorf("SHA3Hex(%q) = %q, want %q", c.in, got, c.hex)
		}
		raw := SHA3([]byte(c.in))
		if len(raw) != 32 {
			t.Errorf("SHA3(%q) len = %d, want 32", c.in, len(raw))
		}
	}
}

// TestHMAC verifies HMAC variants against RFC 4231 test case 1.
func TestHMAC(t *testing.T) {
	key := []byte{0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b,
		0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b, 0x0b}
	data := []byte("Hi There")

	if got := hex.EncodeToString(HMACSHA256(key, data)); got != "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7" {
		t.Errorf("HMACSHA256 = %q", got)
	}
	if got := hex.EncodeToString(HMACSHA1(key, data)); got != "b617318655057264e28bc0b6fb378c8ef146be00" {
		t.Errorf("HMACSHA1 = %q", got)
	}
	if len(HMACMD5(key, data)) != 16 {
		t.Error("HMACMD5 should produce 16 bytes")
	}
	// Determinism.
	if !bytes.Equal(HMACSHA256(key, data), HMACSHA256(key, data)) {
		t.Error("HMACSHA256 should be deterministic")
	}
}

// TestAESEncryptDecrypt verifies AES-256-GCM round trip and tamper detection.
func TestAESEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32) // 32-byte AES-256 key
	for i := range key {
		key[i] = byte(i)
	}
	plain := []byte("the quick brown fox")

	ct, err := AES256Encrypt(key, plain)
	if err != nil {
		t.Fatalf("AES256Encrypt: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Error("ciphertext should not equal plaintext")
	}
	pt, err := AES256Decrypt(key, ct)
	if err != nil {
		t.Fatalf("AES256Decrypt: %v", err)
	}
	if !bytes.Equal(pt, plain) {
		t.Errorf("round-trip mismatch: got %q want %q", pt, plain)
	}
	// Wrong key length rejected.
	if _, err := AES256Encrypt([]byte("short"), plain); err == nil {
		t.Error("AES256Encrypt should reject non-32-byte key")
	}
	// Tampered ciphertext fails authentication.
	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := AES256Decrypt(key, tampered); err == nil {
		t.Error("AES256Decrypt should reject tampered ciphertext")
	}
	// Too-short ciphertext rejected.
	if _, err := AES256Decrypt(key, []byte("short")); err == nil {
		t.Error("AES256Decrypt should reject too-short input")
	}
}

// TestGenerateNonce verifies random byte generation.
func TestGenerateNonce(t *testing.T) {
	n, err := GenerateNonce(32)
	if err != nil {
		t.Fatalf("GenerateNonce: %v", err)
	}
	if len(n) != 32 {
		t.Errorf("len = %d, want 32", len(n))
	}
	// Two nonces should differ (randomness sanity).
	n2, _ := GenerateNonce(32)
	if bytes.Equal(n, n2) {
		t.Error("two nonces should differ")
	}
	if _, err := GenerateNonce(0); err != nil {
		t.Errorf("GenerateNonce(0) err: %v", err)
	}
	if _, err := GenerateNonce(-1); err == nil {
		t.Error("GenerateNonce(-1) should error")
	}
}

// TestGenerateUUID verifies UUID v4 format and uniqueness.
func TestGenerateUUID(t *testing.T) {
	u := GenerateUUID()
	if len(u) != 36 {
		t.Errorf("UUID len = %d, want 36", len(u))
	}
	// Format 8-4-4-4-12 with dashes at the expected positions.
	if u[8] != '-' || u[13] != '-' || u[18] != '-' || u[23] != '-' {
		t.Errorf("UUID %q has bad dash layout", u)
	}
	// Version nibble must be 4.
	if u[14] != '4' {
		t.Errorf("UUID %q version nibble = %q, want '4'", u, string(u[14]))
	}
	// Variant nibble must be 8, 9, a or b.
	v := u[19]
	if v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("UUID %q variant nibble = %q", u, string(v))
	}
	// Uniqueness across many generations.
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		seen[GenerateUUID()] = struct{}{}
	}
	if len(seen) != 1000 {
		t.Errorf("UUID collisions: %d unique out of 1000", len(seen))
	}
}

// TestBase64 verifies base64 encode/decode round trip.
func TestBase64(t *testing.T) {
	cases := []struct {
		in      string
		encoded string
	}{
		{"", ""},
		{"f", "Zg=="},
		{"fo", "Zm8="},
		{"foo", "Zm9v"},
		{"foob", "Zm9vYg=="},
		{"fooba", "Zm9vYmE="},
		{"foobar", "Zm9vYmFy"},
	}
	for _, c := range cases {
		if got := Base64Encode([]byte(c.in)); got != c.encoded {
			t.Errorf("Base64Encode(%q) = %q, want %q", c.in, got, c.encoded)
		}
		dec, err := Base64Decode(c.encoded)
		if err != nil {
			t.Errorf("Base64Decode(%q): %v", c.encoded, err)
			continue
		}
		if string(dec) != c.in {
			t.Errorf("Base64Decode(%q) = %q, want %q", c.encoded, dec, c.in)
		}
	}
	// Invalid input errors.
	if _, err := Base64Decode("!!!not base64!!!"); err == nil {
		t.Error("Base64Decode should reject invalid input")
	}
	// CRC32 sanity: "123456789" -> 0xCBF43926.
	if got := CRC32([]byte("123456789")); got != 0xCBF43926 {
		t.Errorf("CRC32(123456789) = %#x, want 0xCBF43926", got)
	}
	// Empty input is deterministic and zero.
	if CRC32(nil) != 0 {
		t.Error("CRC32(nil) should be 0")
	}
}

// TestCryptoConcurrent exercises the package under the race detector.
func TestCryptoConcurrent(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	done := make(chan struct{})
	for i := 0; i < 16; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			MD5Hex([]byte("x"))
			SHA256Hex([]byte("x"))
			SHA3Hex([]byte("x"))
			HMACSHA256(key, []byte("x"))
			ct, _ := AES256Encrypt(key, []byte("secret"))
			_, _ = AES256Decrypt(key, ct)
			_, _ = GenerateNonce(16)
			_ = GenerateUUID()
			_ = Base64Encode([]byte("x"))
			_, _ = Base64Decode("eA==")
			_ = CRC32([]byte("x"))
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}
