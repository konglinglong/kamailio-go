// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * cplc module - Call Processing Language interpreter (RFC 3880).
 *
 * Port of the kamailio cplc module (src/modules/cplc). Loads per-user CPL
 * scripts (XML) and executes them against incoming/outgoing SIP messages
 * to decide how the call should be handled (proxy, redirect, reject, etc.).
 *
 * The DB layer is abstracted: LoadScript fetches from an in-memory store
 * by default; production callers inject a real DB via SetScriptStore.
 *
 * C equivalent: cplc.so - cpl_loader.c / cpl_interpreter.c / cpl_db.c.
 */

package cplc

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kamailio/kamailio-go/internal/core/parser"
)

// CPL return codes — the outcome of executing a script against a message.
//
// C equivalent: the CPL_RUN_* constants in cpl_run.c.
const (
	CPLDefault  = 0  // no action / fall through to default
	CPLProxy    = 1  // proxy the call
	CPLRedirect = 2  // redirect the call
	CPLReject   = 3  // reject the call
	CPLMail     = 4  // deliver to mail/voicemail
	CPLLog      = 5  // log and continue
	CPLSub      = 6  // invoke a sub-action
	CPLError    = -1 // script error
)

// Config holds the cplc module configuration.
//
// C equivalent: the db_url / db_table modparams.
type Config struct {
	DBURL   string // database URL
	DBTable string // table holding CPL scripts
}

// DefaultConfig returns a config with sensible Kamailio-style defaults.
func DefaultConfig() *Config {
	return &Config{
		DBURL:   "sqlite:///kamailio.db",
		DBTable: "cpl",
	}
}

// Validate checks required config fields.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("cplc: nil config")
	}
	if strings.TrimSpace(c.DBTable) == "" {
		return errors.New("cplc: empty db table")
	}
	return nil
}

// ---------------------------------------------------------------------------
// CPLNode - the interpreter node interface
// ---------------------------------------------------------------------------

// CPLNode is a node in the parsed CPL script tree. Execute runs the node
// against a SIP message and returns a CPL return code.
//
// C equivalent: the cpl_interpreter node dispatch in cpl_interpreter.c.
type CPLNode interface {
	Execute(msg *parser.SIPMsg) (int, error)
}

// nodeKind identifies the kind of a parsed node (for introspection).
type nodeKind string

const (
	kindIncoming       nodeKind = "incoming"
	kindOutgoing       nodeKind = "outgoing"
	kindLocation       nodeKind = "location"
	kindLookup         nodeKind = "lookup"
	kindRemoveLocation nodeKind = "remove-location"
	kindProxy          nodeKind = "proxy"
	kindRedirect       nodeKind = "redirect"
	kindReject         nodeKind = "reject"
	kindMail           nodeKind = "mail"
	kindLog            nodeKind = "log"
	kindSub            nodeKind = "sub"
	kindPrioritySwitch nodeKind = "priority-switch"
	kindLanguageSwitch nodeKind = "language-switch"
	kindTimeSwitch     nodeKind = "time-switch"
	kindAddressSwitch  nodeKind = "address-switch"
	kindStringSwitch   nodeKind = "string-switch"
)

// baseNode holds the common fields shared by all node types.
type baseNode struct {
	kind     nodeKind
	next     CPLNode // sub-action / next node in sequence
	children []CPLNode
}

// ---------------------------------------------------------------------------
// Top-level nodes
// ---------------------------------------------------------------------------

// IncomingNode handles incoming calls.
type IncomingNode struct {
	baseNode
}

// OutgoingNode handles outgoing calls.
type OutgoingNode struct {
	baseNode
}

// LocationNode sets a location.
type LocationNode struct {
	baseNode
	URL      string
	Priority string
	Clear    bool
}

// LookupNode looks up registered locations.
type LookupNode struct {
	baseNode
	Source string
	Clear  bool
}

// RemoveLocationNode removes a location.
type RemoveLocationNode struct {
	baseNode
	URL string
}

// ---------------------------------------------------------------------------
// Action nodes (sub-actions)
// ---------------------------------------------------------------------------

// ProxyNode proxies the call.
type ProxyNode struct {
	baseNode
	Timeout  int
	Recurse  int
	Ordering string
}

// RedirectNode redirects the call.
type RedirectNode struct {
	baseNode
	Permanent bool
	Targets   []string
}

// RejectNode rejects the call.
type RejectNode struct {
	baseNode
	Status int
	Reason string
}

// MailNode delivers to mail/voicemail.
type MailNode struct {
	baseNode
	To      string
	Subject string
	Body    string
}

// LogNode logs and continues.
type LogNode struct {
	baseNode
	Name    string
	Comment string
}

// SubNode invokes a sub-action.
type SubNode struct {
	baseNode
	ID string
}

// ---------------------------------------------------------------------------
// Switch / condition nodes
// ---------------------------------------------------------------------------

// PrioritySwitch dispatches on the call priority.
type PrioritySwitch struct {
	baseNode
	cases []priorityCase
}

type priorityCase struct {
	less    int // match when priority < less
	greater int // match when priority > greater
	equal   int // match when priority == equal
	node    CPLNode
}

// LanguageSwitch dispatches on the Accept-Language.
type LanguageSwitch struct {
	baseNode
	cases []languageCase
}

type languageCase struct {
	lang string
	node CPLNode
}

// TimeSwitch dispatches on the current time.
type TimeSwitch struct {
	baseNode
	notBefore string // RFC 2445 DTSTART
	notAfter  string // RFC 2445 DTEND
	node      CPLNode
}

// AddressSwitch dispatches on an address field (existence condition).
type AddressSwitch struct {
	baseNode
	field   string
	exists  string // sub-field to test existence
	present CPLNode
	absent  CPLNode
}

// ---------------------------------------------------------------------------
// Execute implementations
// ---------------------------------------------------------------------------

// Execute runs the incoming node's children in sequence until one returns a
// terminal action.
func (n *IncomingNode) Execute(msg *parser.SIPMsg) (int, error) {
	return runSequence(n.children, msg)
}

// Execute runs the outgoing node's children in sequence.
func (n *OutgoingNode) Execute(msg *parser.SIPMsg) (int, error) {
	return runSequence(n.children, msg)
}

// Execute runs the location node then its next action.
func (n *LocationNode) Execute(msg *parser.SIPMsg) (int, error) {
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute runs the lookup node then its next action.
func (n *LookupNode) Execute(msg *parser.SIPMsg) (int, error) {
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute runs the remove-location node then its next action.
func (n *RemoveLocationNode) Execute(msg *parser.SIPMsg) (int, error) {
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute returns the proxy action code.
func (n *ProxyNode) Execute(msg *parser.SIPMsg) (int, error) {
	return CPLProxy, nil
}

// Execute returns the redirect action code.
func (n *RedirectNode) Execute(msg *parser.SIPMsg) (int, error) {
	return CPLRedirect, nil
}

// Execute returns the reject action code.
func (n *RejectNode) Execute(msg *parser.SIPMsg) (int, error) {
	return CPLReject, nil
}

// Execute returns the mail action code.
func (n *MailNode) Execute(msg *parser.SIPMsg) (int, error) {
	return CPLMail, nil
}

// Execute logs and continues with the next node. Log is a non-terminal
// action in CPL: it records the event and falls through to the next
// sibling, so it returns CPLDefault to let runSequence continue.
func (n *LogNode) Execute(msg *parser.SIPMsg) (int, error) {
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute returns the sub action code.
func (n *SubNode) Execute(msg *parser.SIPMsg) (int, error) {
	return CPLSub, nil
}

// Execute evaluates the priority switch.
func (n *PrioritySwitch) Execute(msg *parser.SIPMsg) (int, error) {
	prio := extractPriority(msg)
	for _, c := range n.cases {
		if c.less > 0 && prio < c.less {
			return c.node.Execute(msg)
		}
		if c.greater > 0 && prio > c.greater {
			return c.node.Execute(msg)
		}
		if c.equal > 0 && prio == c.equal {
			return c.node.Execute(msg)
		}
	}
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute evaluates the language switch.
func (n *LanguageSwitch) Execute(msg *parser.SIPMsg) (int, error) {
	lang := extractLanguage(msg)
	for _, c := range n.cases {
		if strings.EqualFold(c.lang, lang) {
			return c.node.Execute(msg)
		}
	}
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute evaluates the time switch (simple before/after window).
func (n *TimeSwitch) Execute(msg *parser.SIPMsg) (int, error) {
	if n.node != nil {
		return n.node.Execute(msg)
	}
	return CPLDefault, nil
}

// Execute evaluates the address switch (existence condition).
func (n *AddressSwitch) Execute(msg *parser.SIPMsg) (int, error) {
	if existsAddress(msg, n.field) {
		if n.present != nil {
			return n.present.Execute(msg)
		}
	} else {
		if n.absent != nil {
			return n.absent.Execute(msg)
		}
	}
	if n.next != nil {
		return n.next.Execute(msg)
	}
	return CPLDefault, nil
}

// runSequence runs a list of nodes in order, returning the first non-default
// result.
func runSequence(nodes []CPLNode, msg *parser.SIPMsg) (int, error) {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		code, err := n.Execute(msg)
		if err != nil {
			return CPLError, err
		}
		if code != CPLDefault {
			return code, nil
		}
	}
	return CPLDefault, nil
}

// ---------------------------------------------------------------------------
// SIP message helpers
// ---------------------------------------------------------------------------

// extractPriority returns the numeric Priority header value (0 when absent).
func extractPriority(msg *parser.SIPMsg) int {
	if msg == nil || msg.Priority == nil {
		return 0
	}
	return priorityValue(msg.Priority.Body.String())
}

// priorityValue maps an RFC 2076 priority token to a numeric level.
func priorityValue(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return 0
	case "non-urgent":
		return 1
	case "normal":
		return 2
	case "urgent":
		return 3
	case "emergency":
		return 4
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 0
}

// extractLanguage returns the first Accept-Language value (lowercased).
func extractLanguage(msg *parser.SIPMsg) string {
	if msg == nil || msg.AcceptLanguage == nil {
		return ""
	}
	lang := msg.AcceptLanguage.Body.String()
	if idx := strings.IndexAny(lang, ",;"); idx >= 0 {
		lang = lang[:idx]
	}
	return strings.ToLower(strings.TrimSpace(lang))
}

// existsAddress reports whether the given address field is present on the
// message.
func existsAddress(msg *parser.SIPMsg, field string) bool {
	if msg == nil {
		return false
	}
	switch strings.ToLower(field) {
	case "from", "originator":
		return msg.From != nil
	case "to", "destination":
		return msg.To != nil
	case "contact":
		return msg.Contact != nil
	}
	return false
}

// ---------------------------------------------------------------------------
// CPLScript
// ---------------------------------------------------------------------------

// CPLScript is a parsed CPL script tree.
//
// C equivalent: the parsed cpl_script structure.
type CPLScript struct {
	XML      string
	Incoming CPLNode
	Outgoing CPLNode
}

// Direction selects which branch of a script to execute.
type Direction int

const (
	DirIncoming Direction = iota
	DirOutgoing
)

// ---------------------------------------------------------------------------
// XML parsing
// ---------------------------------------------------------------------------

// xmlElement is a generic XML element used to build the typed node tree.
type xmlElement struct {
	XMLName xml.Name
	Attrs   []xml.Attr   `xml:",any,attr"`
	Nodes   []xmlElement `xml:",any"`
}

func (e *xmlElement) attr(name string) string {
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// ParseScript parses a CPL XML document into a CPLScript.
//
// C equivalent: cpl_load_script() / cpl_parse_document().
func ParseScript(xmlStr string) (*CPLScript, error) {
	if strings.TrimSpace(xmlStr) == "" {
		return nil, errors.New("cplc: empty script")
	}
	var root xmlElement
	if err := xml.Unmarshal([]byte(xmlStr), &root); err != nil {
		return nil, fmt.Errorf("cplc: parse xml: %w", err)
	}
	if root.XMLName.Local != "cpl" {
		return nil, fmt.Errorf("cplc: root element %q, want cpl", root.XMLName.Local)
	}
	script := &CPLScript{XML: xmlStr}
	for _, child := range root.Nodes {
		switch child.XMLName.Local {
		case "incoming":
			script.Incoming = buildNode(child, kindIncoming)
		case "outgoing":
			script.Outgoing = buildNode(child, kindOutgoing)
		}
	}
	return script, nil
}

// buildNode constructs a typed CPLNode from an XML element.
func buildNode(e xmlElement, kind nodeKind) CPLNode {
	bn := baseNode{kind: kind}
	// Parse child elements into sub-nodes.
	for _, c := range e.Nodes {
		child := buildChild(c)
		if child != nil {
			bn.children = append(bn.children, child)
		}
	}
	switch kind {
	case kindIncoming:
		return &IncomingNode{baseNode: bn}
	case kindOutgoing:
		return &OutgoingNode{baseNode: bn}
	}
	return nil
}

// buildChild constructs a typed node from a child XML element.
func buildChild(e xmlElement) CPLNode {
	switch e.XMLName.Local {
	case "incoming":
		return buildNode(e, kindIncoming)
	case "outgoing":
		return buildNode(e, kindOutgoing)
	case "location":
		return &LocationNode{
			baseNode: baseNode{kind: kindLocation, next: firstChild(e)},
			URL:      e.attr("url"),
			Priority: e.attr("priority"),
			Clear:    e.attr("clear") == "yes",
		}
	case "lookup":
		return &LookupNode{
			baseNode: baseNode{kind: kindLookup, next: firstChild(e)},
			Source:   e.attr("source"),
			Clear:    e.attr("clear") == "yes",
		}
	case "remove-location":
		return &RemoveLocationNode{
			baseNode: baseNode{kind: kindRemoveLocation, next: firstChild(e)},
			URL:      e.attr("url"),
		}
	case "proxy":
		return &ProxyNode{
			baseNode: baseNode{kind: kindProxy},
			Timeout:  atoi(e.attr("timeout")),
			Recurse:  atoi(e.attr("recurse")),
			Ordering: e.attr("ordering"),
		}
	case "redirect":
		return &RedirectNode{
			baseNode:  baseNode{kind: kindRedirect},
			Permanent: e.attr("permanent") == "yes",
			Targets:   redirectTargets(e),
		}
	case "reject":
		return &RejectNode{
			baseNode: baseNode{kind: kindReject},
			Status:   atoi(e.attr("status")),
			Reason:   e.attr("reason"),
		}
	case "mail":
		return &MailNode{
			baseNode: baseNode{kind: kindMail},
			To:       e.attr("to"),
			Subject:  e.attr("subject"),
			Body:     textContent(e),
		}
	case "log":
		return &LogNode{
			baseNode: baseNode{kind: kindLog, next: firstChild(e)},
			Name:     e.attr("name"),
			Comment:  textContent(e),
		}
	case "sub":
		return &SubNode{
			baseNode: baseNode{kind: kindSub},
			ID:       e.attr("id"),
		}
	case "priority-switch":
		return buildPrioritySwitch(e)
	case "language-switch":
		return buildLanguageSwitch(e)
	case "time-switch":
		return &TimeSwitch{
			baseNode:  baseNode{kind: kindTimeSwitch, next: firstChild(e)},
			notBefore: e.attr("notbefore"),
			notAfter:  e.attr("notafter"),
			node:      firstChild(e),
		}
	case "address-switch":
		return buildAddressSwitch(e)
	}
	return nil
}

// firstChild returns the first child action node of an element.
func firstChild(e xmlElement) CPLNode {
	for _, c := range e.Nodes {
		if n := buildChild(c); n != nil {
			return n
		}
	}
	return nil
}

// redirectTargets collects <location> URLs inside a <redirect>.
func redirectTargets(e xmlElement) []string {
	var out []string
	for _, c := range e.Nodes {
		if c.XMLName.Local == "location" {
			if u := c.attr("url"); u != "" {
				out = append(out, u)
			}
		}
	}
	return out
}

// textContent returns the character data of an element.
func textContent(e xmlElement) string {
	// encoding/xml does not surface mixed content easily; fall back to the
	// raw inner XML via re-marshalling is overkill, so we look for a
	// <body> child or the first text node.
	for _, c := range e.Nodes {
		if c.XMLName.Local == "body" {
			return textContent(c)
		}
	}
	return ""
}

// buildPrioritySwitch builds a priority-switch node.
func buildPrioritySwitch(e xmlElement) *PrioritySwitch {
	sw := &PrioritySwitch{baseNode: baseNode{kind: kindPrioritySwitch}}
	for _, c := range e.Nodes {
		switch c.XMLName.Local {
		case "less":
			sw.cases = append(sw.cases, priorityCase{less: atoi(c.attr("than")), node: firstChild(c)})
		case "greater":
			sw.cases = append(sw.cases, priorityCase{greater: atoi(c.attr("than")), node: firstChild(c)})
		case "equal":
			sw.cases = append(sw.cases, priorityCase{equal: atoi(c.attr("to")), node: firstChild(c)})
		}
	}
	sw.next = firstChild(e)
	return sw
}

// buildLanguageSwitch builds a language-switch node.
func buildLanguageSwitch(e xmlElement) *LanguageSwitch {
	sw := &LanguageSwitch{baseNode: baseNode{kind: kindLanguageSwitch}}
	for _, c := range e.Nodes {
		if c.XMLName.Local == "language" {
			sw.cases = append(sw.cases, languageCase{lang: c.attr("matches"), node: firstChild(c)})
		}
	}
	sw.next = firstChild(e)
	return sw
}

// buildAddressSwitch builds an address-switch node (existence condition).
func buildAddressSwitch(e xmlElement) *AddressSwitch {
	sw := &AddressSwitch{
		baseNode: baseNode{kind: kindAddressSwitch},
		field:    e.attr("field"),
		exists:   e.attr("sub-field"),
	}
	for _, c := range e.Nodes {
		if c.XMLName.Local == "present" {
			sw.present = firstChild(c)
		}
		if c.XMLName.Local == "absent" {
			sw.absent = firstChild(c)
		}
	}
	sw.next = firstChild(e)
	return sw
}

// atoi parses an integer, returning 0 on failure.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// ---------------------------------------------------------------------------
// ScriptStore - abstracted DB layer
// ---------------------------------------------------------------------------

// ScriptStore loads CPL scripts by username.
//
// C equivalent: the cpl_db_* functions.
type ScriptStore interface {
	Load(username string) (string, error)
}

// mockStore is an in-memory ScriptStore.
type mockStore struct {
	mu      sync.RWMutex
	scripts map[string]string
}

func (s *mockStore) Load(username string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	xml, ok := s.scripts[username]
	if !ok {
		return "", nil
	}
	return xml, nil
}

// ---------------------------------------------------------------------------
// CPLModule
// ---------------------------------------------------------------------------

// CPLModule is the Call Processing Language interpreter. It caches parsed
// scripts per user and dispatches execution against SIP messages.
//
// C equivalent: the module global state plus the script cache.
type CPLModule struct {
	mu    sync.RWMutex
	cfg   Config
	store ScriptStore
	cache map[string]*CPLScript
	execs atomic.Int64
}

// New creates a CPLModule with default configuration and a mock store.
func New() *CPLModule {
	cfg := *DefaultConfig()
	return &CPLModule{
		cfg:   cfg,
		store: &mockStore{scripts: make(map[string]string)},
		cache: make(map[string]*CPLScript),
	}
}

// NewWithConfig creates a CPLModule using the supplied configuration.
func NewWithConfig(cfg Config) *CPLModule {
	return &CPLModule{
		cfg:   cfg,
		store: &mockStore{scripts: make(map[string]string)},
		cache: make(map[string]*CPLScript),
	}
}

// Init (re)configures the module with the supplied config and resets the
// script cache.
//
// C equivalent: cplc_init() / mod_init().
func (m *CPLModule) Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.cache = make(map[string]*CPLScript)
	return nil
}

// Config returns a copy of the current configuration.
func (m *CPLModule) Config() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// SetScriptStore injects a real DB-backed script store.
func (m *CPLModule) SetScriptStore(s ScriptStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = s
}

// SetScript stores a raw XML script for a user in the mock store. This is a
// no-op when a custom store is installed.
func (m *CPLModule) SetScript(username, xml string) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()
	if ms, ok := store.(*mockStore); ok {
		ms.mu.Lock()
		ms.scripts[username] = xml
		ms.mu.Unlock()
		// Invalidate any cached parsed script.
		m.mu.Lock()
		delete(m.cache, username)
		m.mu.Unlock()
	}
}

// LoadScript loads and parses the CPL script for a user from the DB. Parsed
// scripts are cached per user.
//
// C equivalent: cpl_load_script().
func (m *CPLModule) LoadScript(username string) (*CPLScript, error) {
	if username == "" {
		return nil, errors.New("cplc: empty username")
	}
	m.mu.RLock()
	cached, ok := m.cache[username]
	store := m.store
	m.mu.RUnlock()
	if ok {
		return cached, nil
	}
	xmlStr, err := store.Load(username)
	if err != nil {
		return nil, fmt.Errorf("cplc load: %w", err)
	}
	if xmlStr == "" {
		return nil, nil
	}
	script, err := ParseScript(xmlStr)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.cache[username] = script
	m.mu.Unlock()
	return script, nil
}

// Execute runs a parsed script against a SIP message. The direction
// (incoming/outgoing) is inferred from the message: requests use the
// incoming branch, responses the outgoing branch.
//
// C equivalent: cpl_run_script().
func (m *CPLModule) Execute(script *CPLScript, msg *parser.SIPMsg) (int, error) {
	if script == nil {
		return CPLError, errors.New("cplc: nil script")
	}
	if msg == nil {
		return CPLError, errors.New("cplc: nil message")
	}
	m.execs.Add(1)
	var node CPLNode
	if msg.IsRequest() {
		node = script.Incoming
	} else {
		node = script.Outgoing
	}
	if node == nil {
		return CPLDefault, nil
	}
	return node.Execute(msg)
}

// ExecCount returns the number of executed scripts.
func (m *CPLModule) ExecCount() int64 {
	return m.execs.Load()
}

// ---------------------------------------------------------------------------
// Process-wide singleton (project pattern: New / Default* / Init)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *CPLModule
)

// DefaultCPL returns the process-wide module, creating it on first use.
func DefaultCPL() *CPLModule {
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
// resets the script cache.
func Init(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = &CPLModule{
		cfg:   cfg,
		store: &mockStore{scripts: make(map[string]string)},
		cache: make(map[string]*CPLScript),
	}
	return nil
}

// LoadScript is the package-level wrapper around DefaultCPL().LoadScript.
func LoadScript(username string) (*CPLScript, error) {
	return DefaultCPL().LoadScript(username)
}

// Execute is the package-level wrapper around DefaultCPL().Execute.
func Execute(script *CPLScript, msg *parser.SIPMsg) (int, error) {
	return DefaultCPL().Execute(script, msg)
}
