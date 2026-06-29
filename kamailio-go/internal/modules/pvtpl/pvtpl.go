// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * PVTpl module - pseudo-variable template registry and rendering.
 * Port of the kamailio pvtpl module (src/modules/pvtpl).
 *
 * pvtpl stores named templates containing ${var} placeholders and renders
 * them by substituting values from a variables map. Unknown placeholders
 * are left intact.
 *
 * The module is safe for concurrent use.
 */

package pvtpl

import (
	"strings"
	"sync"
)

// PVTplModule holds a registry of named templates.
type PVTplModule struct {
	mu        sync.RWMutex
	templates map[string]string
}

// New creates an empty PVTplModule.
func New() *PVTplModule {
	return &PVTplModule{templates: make(map[string]string)}
}

// Register adds (or replaces) a named template.
func (m *PVTplModule) Register(name, template string) {
	if name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.templates[name] = template
}

// Apply renders the named template by substituting ${var} placeholders
// with values from vars. Returns "" when the template is unknown.
func (m *PVTplModule) Apply(name string, vars map[string]string) string {
	m.mu.RLock()
	tpl, ok := m.templates[name]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	return render(tpl, vars)
}

// List returns a copy of all registered templates.
func (m *PVTplModule) List() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.templates))
	for k, v := range m.templates {
		out[k] = v
	}
	return out
}

// render substitutes ${var} placeholders in tpl using vars. Unknown
// placeholders are left intact.
func render(tpl string, vars map[string]string) string {
	var sb strings.Builder
	i := 0
	for i < len(tpl) {
		if i+1 < len(tpl) && tpl[i] == '$' && tpl[i+1] == '{' {
			end := strings.IndexByte(tpl[i+2:], '}')
			if end >= 0 {
				name := tpl[i+2 : i+2+end]
				if v, ok := vars[name]; ok {
					sb.WriteString(v)
				} else {
					sb.WriteString(tpl[i : i+2+end+1])
				}
				i += 2 + end + 1
				continue
			}
		}
		sb.WriteByte(tpl[i])
		i++
	}
	return sb.String()
}

// --- package-level API ---

var (
	defaultMu sync.RWMutex
	defaultM  *PVTplModule
)

// DefaultPVTpl returns the process-wide module, creating it on first use.
func DefaultPVTpl() *PVTplModule {
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

// Init (re)initialises the process-wide module to a fresh state.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// Register is the package-level wrapper.
func Register(name, template string) { DefaultPVTpl().Register(name, template) }

// Apply is the package-level wrapper.
func Apply(name string, vars map[string]string) string { return DefaultPVTpl().Apply(name, vars) }

// List is the package-level wrapper.
func List() map[string]string { return DefaultPVTpl().List() }
