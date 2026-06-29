// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xcap_server module - XCAP server-side document store.
 * Port of the kamailio xcap_server module (src/modules/xcap_server).
 *
 * The original C module serves XCAP documents over HTTP and persists
 * them in a database. This Go counterpart exposes an in-memory document
 * store keyed by (auid, docType, user) and a minimal HTTP listener.
 *
 * It is safe for concurrent use.
 */

package xcap_server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// XCAPServerConfig configures an XCAPServerModule.
type XCAPServerConfig struct {
	ListenAddr string
	DBDriver   string
}

// docKey uniquely identifies an XCAP document.
type docKey struct {
	auid    string
	docType string
	user    string
}

// XCAPServerModule stores and serves XCAP documents.
// It is the Go counterpart of the kamailio xcap_server module.
type XCAPServerModule struct {
	mu        sync.RWMutex
	listenAddr string
	dbDriver  string
	docs      map[docKey][]byte
	server    *http.Server
	running   bool
}

// New creates an XCAPServerModule with default settings.
func New() *XCAPServerModule {
	return &XCAPServerModule{docs: make(map[docKey][]byte)}
}

// Init (re)configures the module from cfg. A nil cfg applies defaults.
//
//	C: mod_init()
func (m *XCAPServerModule) Init(cfg *XCAPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == nil {
		cfg = &XCAPServerConfig{}
	}
	m.listenAddr = cfg.ListenAddr
	m.dbDriver = cfg.DBDriver
	if m.docs == nil {
		m.docs = make(map[docKey][]byte)
	}
	return nil
}

// StoreDocument stores (or replaces) an XCAP document.
//
//	C: xcap_db_insert / xcap_db_update
func (m *XCAPServerModule) StoreDocument(auid, docType, user string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(body))
	copy(cp, body)
	m.docs[docKey{auid, docType, user}] = cp
	return nil
}

// GetDocument returns the stored XCAP document, or an error if absent.
//
//	C: xcap_db_get_doc()
func (m *XCAPServerModule) GetDocument(auid, docType, user string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	body, ok := m.docs[docKey{auid, docType, user}]
	if !ok {
		return nil, fmt.Errorf("xcap_server: document not found")
	}
	out := make([]byte, len(body))
	copy(out, body)
	return out, nil
}

// DeleteDocument removes a stored XCAP document. Removing a missing
// document is not an error.
//
//	C: xcap_db_delete()
func (m *XCAPServerModule) DeleteDocument(auid, docType, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.docs, docKey{auid, docType, user})
	return nil
}

// ListDocuments returns the user identifiers of all documents stored
// for the given AUID.
//
//	C: xcap_db_get_auid()
func (m *XCAPServerModule) ListDocuments(auid string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]bool)
	var out []string
	for k := range m.docs {
		if k.auid == auid {
			id := k.user + "/" + k.docType
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// Start begins serving XCAP documents over HTTP on the configured
// listen address. It returns an error if already running.
//
//	C: http server start
func (m *XCAPServerModule) Start() error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("xcap_server: already running")
	}
	addr := m.listenAddr
	m.mu.Unlock()
	if addr == "" {
		return fmt.Errorf("xcap_server: no listen address")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", m.handleHTTP)
	srv := &http.Server{Addr: addr, Handler: mux}
	m.mu.Lock()
	m.server = srv
	m.running = true
	m.mu.Unlock()
	go func() {
		_ = srv.ListenAndServe()
	}()
	return nil
}

// Stop shuts down the HTTP server if running.
func (m *XCAPServerModule) Stop() {
	m.mu.Lock()
	srv := m.server
	m.server = nil
	m.running = false
	m.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// handleHTTP serves XCAP GET/PUT/DELETE requests.
func (m *XCAPServerModule) handleHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// Expected: {auid}/users/{user}/{docType}
	if len(parts) < 4 || parts[1] != "users" {
		http.NotFound(w, r)
		return
	}
	auid := parts[0]
	user := parts[2]
	docType := strings.Join(parts[3:], "/")
	switch r.Method {
	case http.MethodGet:
		body, err := m.GetDocument(auid, docType, user)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xcap-el+xml")
		_, _ = w.Write(body)
	case http.MethodPut:
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		_ = m.StoreDocument(auid, docType, user, buf)
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		_ = m.DeleteDocument(auid, docType, user)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- package-level API ---

var defaultModule = New()

// DefaultXCAPServer returns the package-level default XCAPServerModule.
func DefaultXCAPServer() *XCAPServerModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() error {
	return defaultModule.Init(&XCAPServerConfig{})
}
