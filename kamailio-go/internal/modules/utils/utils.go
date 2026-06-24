// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Utils module - string encoding helpers.
 * Port of the kamailio utils module (src/modules/utils).
 *
 * The utils module exposes a grab-bag of helper functions usable from
 * the config script: base64 / hex encoding and URL (percent) encoding
 * and decoding. This Go counterpart wraps the standard-library
 * encoders.
 *
 * The methods are stateless and therefore safe for concurrent use.
 */

package utils

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
)

// UtilsModule exposes string encoding helpers.
// C: struct module utils
type UtilsModule struct{}

// New creates a UtilsModule.
func New() *UtilsModule {
	return &UtilsModule{}
}

// EncodeBase64 returns the base64 (std) encoding of data.
//
//	C: utils_base64_encode()
func (m *UtilsModule) EncodeBase64(data string) string {
	if m == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(data))
}

// DecodeBase64 decodes a base64 (std) string.
//
//	C: utils_base64_decode()
func (m *UtilsModule) DecodeBase64(data string) (string, error) {
	if m == nil {
		return "", nil
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EncodeHex returns the lowercase hexadecimal encoding of data.
//
//	C: utils_hex_encode()
func (m *UtilsModule) EncodeHex(data string) string {
	if m == nil {
		return ""
	}
	return hex.EncodeToString([]byte(data))
}

// DecodeHex decodes a hexadecimal string.
//
//	C: utils_hex_decode()
func (m *UtilsModule) DecodeHex(data string) (string, error) {
	if m == nil {
		return "", nil
	}
	b, err := hex.DecodeString(data)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// URLEncode returns the percent-encoded form of data.
//
//	C: utils_url_encode()
func (m *UtilsModule) URLEncode(data string) string {
	if m == nil {
		return ""
	}
	return url.QueryEscape(data)
}

// URLDecode decodes a percent-encoded string.
//
//	C: utils_url_decode()
func (m *UtilsModule) URLDecode(data string) (string, error) {
	if m == nil {
		return "", nil
	}
	return url.QueryUnescape(data)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

// DefaultUtils returns the process-wide UtilsModule.
func DefaultUtils() *UtilsModule {
	return New()
}

// Init (re)initialises the process-wide UtilsModule. Stateless, so a no-op
// apart from returning the default instance.
func Init() *UtilsModule {
	return New()
}
