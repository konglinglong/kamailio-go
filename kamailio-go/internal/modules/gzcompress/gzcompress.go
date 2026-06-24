// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * gzcompress module - gzip compression helpers.
 *
 * Compresses and decompresses byte slices and strings using the gzip
 * format. String variants base64-encode the compressed bytes so the
 * result is safe to transport as text. The module is safe for
 * concurrent use.
 */

package gzcompress

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"io"
	"sync"
)

// GZCompressModule provides gzip compression and decompression.
type GZCompressModule struct {
	mu sync.Mutex
}

// New creates a GZCompressModule.
func New() *GZCompressModule {
	return &GZCompressModule{}
}

// Compress gzip-compresses data and returns the result.
//
//	C: gz_compress()
func (m *GZCompressModule) Compress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("gzcompress: empty data")
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decompress gzip-decompresses data produced by Compress.
//
//	C: gz_decompress()
func (m *GZCompressModule) Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("gzcompress: empty data")
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CompressString gzip-compresses s and returns the result base64-encoded.
//
//	C: gz_compress_str()
func (m *GZCompressModule) CompressString(s string) (string, error) {
	if s == "" {
		return "", errors.New("gzcompress: empty string")
	}
	c, err := m.Compress([]byte(s))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(c), nil
}

// DecompressString decodes a base64-encoded gzip blob and returns the
// original string.
//
//	C: gz_decompress_str()
func (m *GZCompressModule) DecompressString(s string) (string, error) {
	if s == "" {
		return "", errors.New("gzcompress: empty string")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	out, err := m.Decompress(raw)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultGZCompress *GZCompressModule
)

// DefaultGZCompress returns the process-wide GZCompressModule, creating
// it on first use.
func DefaultGZCompress() *GZCompressModule {
	defaultMu.RLock()
	m := defaultGZCompress
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultGZCompress == nil {
		defaultGZCompress = New()
	}
	return defaultGZCompress
}

// Init (re)initialises the process-wide GZCompressModule to a fresh state.
// Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultGZCompress = New()
}
