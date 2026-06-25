// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xhttp_pi module - HTTP provisioning interface.
 *
 * Port of the kamailio xhttp_pi module (src/modules/xhttp_pi). Exposes a
 * Kamailio configuration tree over HTTP so operators can inspect and
 * modify module parameters at runtime. The config tree is loaded from a
 * framework definition file (the C module's "framework" modparam) and
 * stored as a tree of ConfigNode values.
 *
 * C equivalent: xhttp_pi.so - xhttp_pi.c / pi_db.c.
 */

package xhttp_pi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// NodeType classifies a ConfigNode.
//
// C equivalent: the framework block types (section / param / module).
type NodeType string

const (
	NodeSection NodeType = "section"
	NodeParam   NodeType = "param"
	NodeModule  NodeType = "module"
	NodeRoot    NodeType = "root"
)

// ConfigNode is a node in the provisioning config tree.
//
// C equivalent: a framework definition entry.
type ConfigNode struct {
	Name     string        `json:"name"`
	Value    string        `json:"value,omitempty"`
	Type     NodeType      `json:"type"`
	Children []*ConfigNode `json:"children,omitempty"`
	parent   *ConfigNode
}

// ModuleInfo describes a module and its parameters.
type ModuleInfo struct {
	Name   string
	Params []ParamInfo
}

// ParamInfo describes a single module parameter.
type ParamInfo struct {
	Name  string
	Value string
}

// Config holds the xhttp_pi server configuration.
//
// C equivalent: the xhttp_pi_root / xhttp_pi_buf_size / framework modparams.
type Config struct {
	Listen     string   // HTTP listen address
	RootDir    string   // URL root path prefix
	AllowFiles []string // framework files allowed to be loaded
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		Listen:  "127.0.0.1:8082",
		RootDir: "/pi",
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("xhttp_pi: nil config")
	}
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("xhttp_pi: empty listen address")
	}
	return nil
}

// ---------------------------------------------------------------------------
// XHTTPPIModule
// ---------------------------------------------------------------------------

// XHTTPPIModule is the HTTP provisioning interface. It owns a config tree
// and serves it over HTTP.
//
// C equivalent: the module global state plus the framework tree.
type XHTTPPIModule struct {
	mu         sync.RWMutex
	cfg        Config
	root       *ConfigNode
	httpServer *http.Server
	started    atomic.Bool
	requests   atomic.Int64
}

// New creates an XHTTPPIModule with default configuration and an empty tree.
func New() *XHTTPPIModule {
	cfg := *DefaultConfig()
	return &XHTTPPIModule{
		cfg:  cfg,
		root: &ConfigNode{Name: "root", Type: NodeRoot},
	}
}

// NewWithConfig creates an XHTTPPIModule using the supplied configuration.
func NewWithConfig(cfg Config) *XHTTPPIModule {
	return &XHTTPPIModule{
		cfg:  cfg,
		root: &ConfigNode{Name: "root", Type: NodeRoot},
	}
}

// Init (re)configures the module with the supplied config and resets the
// config tree.
//
// C equivalent: xhttp_pi_init() / mod_init().
func (m *XHTTPPIModule) Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.root = &ConfigNode{Name: "root", Type: NodeRoot}
	return nil
}

// Config returns a copy of the current configuration.
func (m *XHTTPPIModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// SetListen configures the HTTP listen address.
func (m *XHTTPPIModule) SetListen(listen string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Listen = listen
}

// SetRootDir configures the URL root path prefix.
func (m *XHTTPPIModule) SetRootDir(rootDir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.RootDir = rootDir
}

// LoadConfigFile loads a framework definition file. The file is a simple
// line-oriented format where each line is "type name [value]" indented to
// express nesting (two spaces per level). Returns an error if the file
// cannot be read.
//
// C equivalent: the framework file parser in pi_framework.c.
func (m *XHTTPPIModule) LoadConfigFile(path string) error {
	if path == "" {
		return errors.New("xhttp_pi: empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("xhttp_pi: read %q: %w", path, err)
	}
	tree, err := parseFramework(string(data))
	if err != nil {
		return fmt.Errorf("xhttp_pi: parse %q: %w", path, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = tree
	return nil
}

// parseFramework parses the line-oriented framework definition into a tree.
// Each line is "type name [value]"; indentation (two spaces) sets depth.
func parseFramework(text string) (*ConfigNode, error) {
	root := &ConfigNode{Name: "root", Type: NodeRoot}
	// Stack of ancestors keyed by depth; root is at depth 0.
	stack := []*ConfigNode{root}
	for lineNum, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		depth := 0
		for depth < len(line) && line[depth] == ' ' {
			depth++
		}
		depth /= 2
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("line %d: expected at least type and name", lineNum+1)
		}
		nodeType := NodeType(fields[0])
		name := fields[1]
		value := ""
		if len(fields) >= 3 {
			value = strings.Join(fields[2:], " ")
		}
		node := &ConfigNode{Name: name, Value: value, Type: nodeType}
		// Walk up the stack to the parent at the right depth.
		for len(stack) > depth+1 {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		node.parent = parent
		parent.Children = append(parent.Children, node)
		stack = append(stack, node)
	}
	return root, nil
}

// GetConfigTree returns the root of the config tree.
func (m *XHTTPPIModule) GetConfigTree() *ConfigNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneTree(m.root)
}

// cloneTree returns a deep copy of the tree (without parent pointers, which
// are internal).
func cloneTree(n *ConfigNode) *ConfigNode {
	if n == nil {
		return nil
	}
	out := &ConfigNode{Name: n.Name, Value: n.Value, Type: n.Type}
	for _, c := range n.Children {
		out.Children = append(out.Children, cloneTree(c))
	}
	return out
}

// findNodeIn walks the tree following the dotted path. An empty path
// returns the root.
func findNodeIn(root *ConfigNode, path string) *ConfigNode {
	if root == nil {
		return nil
	}
	if path == "" || path == root.Name {
		return root
	}
	parts := strings.Split(path, ".")
	if parts[0] != root.Name && root.Name != "root" {
		// Allow the path to start at root or skip it.
	}
	cur := root
	for _, p := range parts {
		if p == "" || p == cur.Name {
			continue
		}
		next := cur.child(p)
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// child returns the named child, or nil.
func (n *ConfigNode) child(name string) *ConfigNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// GetParameter returns the value of the parameter at the given dotted path.
//
// C equivalent: the PI get command.
func (m *XHTTPPIModule) GetParameter(path string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node := findNodeIn(m.root, path)
	if node == nil {
		return "", fmt.Errorf("xhttp_pi: parameter %q not found", path)
	}
	return node.Value, nil
}

// SetParameter updates the value of the parameter at the given dotted path.
//
// C equivalent: the PI set command.
func (m *XHTTPPIModule) SetParameter(path, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node := findNodeIn(m.root, path)
	if node == nil {
		return fmt.Errorf("xhttp_pi: parameter %q not found", path)
	}
	if node.Type != NodeParam {
		return fmt.Errorf("xhttp_pi: %q is not a parameter", path)
	}
	node.Value = value
	return nil
}

// ListModules returns the modules in the config tree with their parameters.
//
// C equivalent: the PI list command.
func (m *XHTTPPIModule) ListModules() []ModuleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ModuleInfo
	for _, c := range m.root.Children {
		if c.Type != NodeModule {
			continue
		}
		mi := ModuleInfo{Name: c.Name}
		for _, p := range c.Children {
			if p.Type == NodeParam {
				mi.Params = append(mi.Params, ParamInfo{Name: p.Name, Value: p.Value})
			}
		}
		out = append(out, mi)
	}
	return out
}

// AddModule adds a module node to the tree (used by tests / programmatic
// setup).
func (m *XHTTPPIModule) AddModule(name string) *ConfigNode {
	m.mu.Lock()
	defer m.mu.Unlock()
	node := &ConfigNode{Name: name, Type: NodeModule, parent: m.root}
	m.root.Children = append(m.root.Children, node)
	return node
}

// AddParam adds a parameter to a module node.
func (n *ConfigNode) AddParam(name, value string) *ConfigNode {
	if n == nil {
		return nil
	}
	child := &ConfigNode{Name: name, Value: value, Type: NodeParam, parent: n}
	n.Children = append(n.Children, child)
	return child
}

// RequestCount returns the number of served HTTP requests.
func (m *XHTTPPIModule) RequestCount() int64 {
	return m.requests.Load()
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

// ServeHTTP implements http.Handler. GET returns the config tree (or a
// parameter when ?get=path is supplied); POST sets a parameter
// (?set=path&value=...).
//
// C equivalent: the xhttp_pi request dispatcher.
func (m *XHTTPPIModule) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.requests.Add(1)
	switch r.Method {
	case http.MethodGet:
		m.handleGet(w, r)
	case http.MethodPost:
		m.handlePost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *XHTTPPIModule) handleGet(w http.ResponseWriter, r *http.Request) {
	if get := r.URL.Query().Get("get"); get != "" {
		val, err := m.GetParameter(get)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"path": get, "value": val})
		return
	}
	if list := r.URL.Query().Get("list"); list == "modules" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.ListModules())
		return
	}
	tree := m.GetConfigTree()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tree)
}

func (m *XHTTPPIModule) handlePost(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("set")
	value := r.URL.Query().Get("value")
	if path == "" {
		http.Error(w, "missing set parameter", http.StatusBadRequest)
		return
	}
	if err := m.SetParameter(path, value); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path, "value": value, "status": "ok"})
}

// Start begins serving HTTP on the configured listen address. It blocks
// until Stop is called.
func (m *XHTTPPIModule) Start() error {
	m.mu.Lock()
	if m.started.Load() {
		m.mu.Unlock()
		return errors.New("xhttp_pi: already started")
	}
	cfg := m.cfg
	m.started.Store(true)
	m.mu.Unlock()
	defer m.started.Store(false)
	srv := &http.Server{Addr: cfg.Listen, Handler: m}
	m.mu.Lock()
	m.httpServer = srv
	m.mu.Unlock()
	return srv.ListenAndServe()
}

// Stop tears down the HTTP server.
func (m *XHTTPPIModule) Stop() error {
	m.mu.Lock()
	srv := m.httpServer
	m.httpServer = nil
	m.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *XHTTPPIModule
)

// DefaultXHTTPPI returns the process-wide module, creating it on first use.
func DefaultXHTTPPI() *XHTTPPIModule {
	defaultMu.RLock()
	m := defaultM
	defaultMu.RUnlock()
	if m != nil {
		return m
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultM == nil {
		defaultM = New()
	}
	return defaultM
}

// Init (re)configures the process-wide module with the supplied config and
// resets the config tree.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = &XHTTPPIModule{
		cfg:  cfg,
		root: &ConfigNode{Name: "root", Type: NodeRoot},
	}
	return nil
}

// GetParameter is the package-level wrapper around DefaultXHTTPPI().GetParameter.
func GetParameter(path string) (string, error) { return DefaultXHTTPPI().GetParameter(path) }

// SetParameter is the package-level wrapper around DefaultXHTTPPI().SetParameter.
func SetParameter(path, value string) error { return DefaultXHTTPPI().SetParameter(path, value) }

// ListModules is the package-level wrapper around DefaultXHTTPPI().ListModules.
func ListModules() []ModuleInfo { return DefaultXHTTPPI().ListModules() }
