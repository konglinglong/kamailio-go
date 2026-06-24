// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * tls_wolfssl module - WolfSSL TLS engine wrapper.
 *
 * A simplified TLS engine: Init configures the certificate and key
 * paths and enables the engine; Handshake performs a 1-byte round-trip
 * on a connection to confirm it is usable. No real TLS cryptography is
 * performed. The module is safe for concurrent use.
 */

package tls_wolfssl

import (
	"errors"
	"io"
	"net"
	"sync"
)

// TLSWolfSSLModule wraps a WolfSSL-style TLS engine.
type TLSWolfSSLModule struct {
	mu       sync.RWMutex
	enabled  bool
	certFile string
	keyFile  string
}

// New creates a TLSWolfSSLModule in a disabled state.
func New() *TLSWolfSSLModule {
	return &TLSWolfSSLModule{}
}

// Init configures the certificate and key files and enables the engine.
// Empty paths leave the module disabled.
//
//	C: tls_wolfssl_init()
func (m *TLSWolfSSLModule) Init(certFile, keyFile string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.certFile = certFile
	m.keyFile = keyFile
	m.enabled = certFile != "" && keyFile != ""
}

// IsEnabled reports whether the TLS engine is enabled.
//
//	C: tls_wolfssl_is_enabled()
func (m *TLSWolfSSLModule) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// Handshake performs a 1-byte round-trip on conn to confirm it is usable.
// Returns an error when the engine is disabled or the connection is dead.
//
//	C: tls_wolfssl_handshake()
func (m *TLSWolfSSLModule) Handshake(conn net.Conn) error {
	if conn == nil {
		return errors.New("tls_wolfssl: nil connection")
	}
	m.mu.RLock()
	enabled := m.enabled
	m.mu.RUnlock()
	if !enabled {
		return errors.New("tls_wolfssl: engine not enabled")
	}
	// Write a handshake byte and read it back.
	if _, err := conn.Write([]byte{0x16}); err != nil {
		return err
	}
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	if buf[0] != 0x16 {
		return errors.New("tls_wolfssl: handshake byte mismatch")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu         sync.RWMutex
	defaultTLSWolfSSL *TLSWolfSSLModule
)

// DefaultTLSWolfSSL returns the process-wide TLSWolfSSLModule, creating it
// on first use.
func DefaultTLSWolfSSL() *TLSWolfSSLModule {
	defaultMu.RLock()
	m := defaultTLSWolfSSL
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultTLSWolfSSL == nil {
		defaultTLSWolfSSL = New()
	}
	return defaultTLSWolfSSL
}

// Init (re)initialises the process-wide TLSWolfSSLModule to a fresh,
// disabled state. Safe to call multiple times.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultTLSWolfSSL = New()
}
