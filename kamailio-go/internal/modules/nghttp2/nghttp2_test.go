// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - nghttp2 module tests.
 */

package nghttp2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	m := New()
	if err := m.Init(&NGHTTP2Config{
		ListenAddr:           "127.0.0.1:7071",
		CertFile:             "/tmp/cert.pem",
		KeyFile:              "/tmp/key.pem",
		MaxConcurrentStreams: 128,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if m.cfg == nil {
		t.Fatalf("cfg not set after Init()")
	}
	if m.cfg.ListenAddr != "127.0.0.1:7071" {
		t.Errorf("ListenAddr = %q", m.cfg.ListenAddr)
	}
	if m.cfg.CertFile != "/tmp/cert.pem" {
		t.Errorf("CertFile = %q", m.cfg.CertFile)
	}
	if m.cfg.KeyFile != "/tmp/key.pem" {
		t.Errorf("KeyFile = %q", m.cfg.KeyFile)
	}
	if m.cfg.MaxConcurrentStreams != 128 {
		t.Errorf("MaxConcurrentStreams = %d", m.cfg.MaxConcurrentStreams)
	}
	// nil config is accepted.
	if err := (&NGHTTP2Module{}).Init(nil); err != nil {
		t.Errorf("Init(nil) error = %v", err)
	}
}

func TestRegisterAndHandle(t *testing.T) {
	m := New()
	m.RegisterHandler("/upper", func(method, path, body string, headers map[string]string) (int, string, map[string]string) {
		return 200, strings.ToUpper(body), map[string]string{"X-Echo": method}
	})

	code, respBody, respHeaders := m.handle("POST", "/upper", "hello", map[string]string{"X-In": "1"})
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
	if respBody != "HELLO" {
		t.Errorf("body = %q, want HELLO", respBody)
	}
	if respHeaders["X-Echo"] != "POST" {
		t.Errorf("X-Echo = %q, want POST", respHeaders["X-Echo"])
	}
}

func TestHandleNotFound(t *testing.T) {
	m := New()
	code, body, _ := m.handle("GET", "/missing", "", nil)
	if code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
	if body == "" {
		t.Errorf("body should not be empty for 404")
	}
}

func TestRemoveHandler(t *testing.T) {
	m := New()
	m.RegisterHandler("/x", func(method, path, body string, headers map[string]string) (int, string, map[string]string) {
		return 200, "ok", nil
	})
	if code, _, _ := m.handle("GET", "/x", "", nil); code != 200 {
		t.Fatalf("code = %d, want 200 before remove", code)
	}
	m.RemoveHandler("/x")
	if code, _, _ := m.handle("GET", "/x", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404 after remove", code)
	}
	m.RemoveHandler("/x")
}

func TestStreamCount(t *testing.T) {
	m := New()
	release := make(chan struct{})
	entered := make(chan struct{})
	m.RegisterHandler("/slow", func(method, path, body string, headers map[string]string) (int, string, map[string]string) {
		entered <- struct{}{}
		<-release
		return 200, "done", nil
	})

	srv := httptest.NewTLSServer(m)
	defer srv.Close()

	go func() {
		resp, err := srv.Client().Get(srv.URL + "/slow")
		if err != nil {
			t.Errorf("request error: %v", err)
			return
		}
		resp.Body.Close()
	}()

	<-entered
	if got := m.StreamCount(); got != 1 {
		t.Errorf("StreamCount() = %d, want 1", got)
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && m.StreamCount() != 0 {
		time.Sleep(time.Millisecond)
	}
	if got := m.StreamCount(); got != 0 {
		t.Errorf("StreamCount() = %d, want 0 after drain", got)
	}
}

func TestStartStopWithCert(t *testing.T) {
	m := New()
	m.RegisterHandler("/hello", func(method, path, body string, headers map[string]string) (int, string, map[string]string) {
		return 200, "world", map[string]string{"Content-Type": "text/plain"}
	})

	certFile, keyFile := generateCertFiles(t)
	if err := m.Init(&NGHTTP2Config{
		ListenAddr:           "127.0.0.1:0",
		CertFile:             certFile,
		KeyFile:              keyFile,
		MaxConcurrentStreams: 100,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := m.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !m.IsRunning() {
		t.Fatalf("IsRunning() = false, want true")
	}
	addr := m.listener.Addr().String()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			ForceAttemptHTTP2: true,
		},
	}
	resp, err := client.Get("https://" + addr + "/hello")
	if err != nil {
		t.Fatalf("http.Get error: %v", err)
	}
	defer resp.Body.Close()
	if resp.Proto != "HTTP/2.0" {
		t.Errorf("Proto = %q, want HTTP/2.0", resp.Proto)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "world" {
		t.Errorf("body = %q, want world", body)
	}

	m.Stop()
	if m.IsRunning() {
		t.Errorf("IsRunning() = true, want false after Stop")
	}
	m.Stop()
}

func TestStartMissingCert(t *testing.T) {
	m := New()
	if err := m.Init(&NGHTTP2Config{
		ListenAddr: "127.0.0.1:0",
		CertFile:   "/nonexistent/cert.pem",
		KeyFile:    "/nonexistent/key.pem",
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := m.Start(); err == nil {
		t.Errorf("Start() with missing cert should error")
		m.Stop()
	}
}

func TestDefaultAndInit(t *testing.T) {
	if DefaultNGHTTP2() == nil {
		t.Fatalf("DefaultNGHTTP2() nil")
	}
	Init()
	d := DefaultNGHTTP2()
	if d == nil {
		t.Fatalf("DefaultNGHTTP2() nil after Init")
	}
	if d != DefaultNGHTTP2() {
		t.Fatalf("DefaultNGHTTP2() returned different instances")
	}
}

func TestConcurrent(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/p" + itoa(i)
			m.RegisterHandler(path, func(method, p, body string, headers map[string]string) (int, string, map[string]string) {
				return 200, p, nil
			})
			if code, _, _ := m.handle("GET", path, "", nil); code != 200 {
				t.Errorf("handle %s: code %d", path, code)
			}
			m.RemoveHandler(path)
		}(i)
	}
	wg.Wait()
}

// generateCertFiles creates a self-signed certificate and key in temporary
// files, returning their paths.
func generateCertFiles(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// itoa is a tiny local int->string helper to avoid pulling strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
