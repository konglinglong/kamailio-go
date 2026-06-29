// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xcap_client module - XCAP (XML Configuration Access Protocol) client.
 * Port of the kamailio xcap_client module (src/modules/xcap_client).
 *
 * The original C module fetches/stores XCAP documents on a remote XCAP
 * server, typically to manage resource-lists and presence authorization
 * rules for the presence server. This Go counterpart exposes the same
 * CRUD operations over an HTTP-backed store.
 *
 * It is safe for concurrent use.
 */

package xcap_client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// XCAPConfig configures an XCAPModule.
type XCAPConfig struct {
	ServerURL string
	Username  string
	Password  string
}

// XCAPModule talks to a remote XCAP server to manage XML documents.
// It is the Go counterpart of the kamailio xcap_client module.
type XCAPModule struct {
	mu       sync.Mutex
	serverURL string
	username string
	password string
	client   *http.Client
	connected bool
}

// New creates an XCAPModule with default settings.
func New() *XCAPModule {
	m := &XCAPModule{client: &http.Client{}}
	return m
}

// Init (re)configures the module from cfg. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *XCAPModule) Init(cfg *XCAPConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &XCAPConfig{}
	}
	m.serverURL = strings.TrimRight(cfg.ServerURL, "/")
	m.username = cfg.Username
	m.password = cfg.Password
	if m.client == nil {
		m.client = &http.Client{}
	}
	m.connected = m.serverURL != ""
	return nil
}

// docURL builds the XCAP document URL for the given AUID, document type
// and user. The AUID maps to the XCAP application usage identifier.
func (m *XCAPModule) docURL(auid, docType, user string) string {
	return fmt.Sprintf("%s/%s/users/%s/%s", m.serverURL, auid, user, docType)
}

// do performs an HTTP request with optional basic auth and body.
func (m *XCAPModule) do(method, url string, body []byte) (*http.Response, []byte, error) {
	m.mu.Lock()
	client := m.client
	user, pass := m.username, m.password
	m.mu.Unlock()
	if client == nil {
		client = &http.Client{}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("xcap_client: %w", err)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/xcap-el+xml")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("xcap_client: %w", err)
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return resp, nil, fmt.Errorf("xcap_client: read body: %w", rerr)
	}
	return resp, data, nil
}

// GetDocument fetches the full XCAP document identified by auid, docType
// and user. Returns the raw document bytes.
//
//	C: xcapGetDoc()
func (m *XCAPModule) GetDocument(auid, docType, user string) ([]byte, error) {
	if !m.IsConnected() {
		return nil, fmt.Errorf("xcap_client: not connected")
	}
	resp, data, err := m.do(http.MethodGet, m.docURL(auid, docType, user), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xcap_client: GET status %d", resp.StatusCode)
	}
	return data, nil
}

// GetElement fetches a single element from an XCAP document. The element
// path is appended to the document URL.
//
//	C: xcapGetElem()
func (m *XCAPModule) GetElement(auid, docType, user, element string) ([]byte, error) {
	if !m.IsConnected() {
		return nil, fmt.Errorf("xcap_client: not connected")
	}
	url := m.docURL(auid, docType, user) + "/" + strings.TrimLeft(element, "/")
	resp, data, err := m.do(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xcap_client: GET element status %d", resp.StatusCode)
	}
	return data, nil
}

// PutDocument creates or replaces an XCAP document with body.
//
//	C: xcapPutDoc()
func (m *XCAPModule) PutDocument(auid, docType, user string, body []byte) error {
	if !m.IsConnected() {
		return fmt.Errorf("xcap_client: not connected")
	}
	resp, _, err := m.do(http.MethodPut, m.docURL(auid, docType, user), body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("xcap_client: PUT status %d", resp.StatusCode)
	}
	return nil
}

// DeleteDocument removes an XCAP document.
//
//	C: xcapDelDoc()
func (m *XCAPModule) DeleteDocument(auid, docType, user string) error {
	if !m.IsConnected() {
		return fmt.Errorf("xcap_client: not connected")
	}
	resp, _, err := m.do(http.MethodDelete, m.docURL(auid, docType, user), nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("xcap_client: DELETE status %d", resp.StatusCode)
	}
	return nil
}

// IsConnected reports whether the module has been initialised with a
// server URL.
func (m *XCAPModule) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

// --- package-level API ---

var defaultModule = New()

// DefaultXCAPClient returns the package-level default XCAPModule.
func DefaultXCAPClient() *XCAPModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() error {
	return defaultModule.Init(&XCAPConfig{})
}
