// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * xmlops module - XML parsing, querying and building helpers.
 * Port of the kamailio xmlops module (src/modules/xmlops).
 *
 * The original C module uses libxml2 to parse XML documents, query them
 * with XPath and build new documents. This Go counterpart provides a
 * simplified element-path API (dot-notation, e.g. "root.child.leaf")
 * built on top of encoding/xml, plus a basic well-formedness validator.
 *
 * It is safe for concurrent use.
 */

package xmlops

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"sync"
)

// xmlNode is the generic representation of an XML element used internally.
type xmlNode struct {
	Name     string
	Text     string
	Attrs    map[string]string
	Children []*xmlNode
}

// XMLOpsModule parses, queries, builds and validates XML documents.
// It is the Go counterpart of the kamailio xmlops module.
type XMLOpsModule struct {
	mu sync.RWMutex
}

// New creates an XMLOpsModule.
func New() *XMLOpsModule {
	return &XMLOpsModule{}
}

// Parse decodes an XML string into a generic map representation. The
// returned value is a map keyed by the root element name; each element
// becomes a nested map with "_text", "_attrs" and child keys.
//
//	C: xmlDocParse()
func (m *XMLOpsModule) Parse(xmlStr string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	root, err := parseNode(xmlStr)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{root.Name: nodeToMap(root)}, nil
}

// Get returns the text value at the given element path. The path uses
// dot-notation (e.g. "presence.tuple.status.basic"). Returns an error
// if the path cannot be resolved.
//
//	C: xmlops_xpath_get()
func (m *XMLOpsModule) Get(xmlStr, xpath string) (string, error) {
	root, err := parseNode(xmlStr)
	if err != nil {
		return "", err
	}
	node, ok := findByPath(root, xpath)
	if !ok {
		return "", fmt.Errorf("xmlops: path %q not found", xpath)
	}
	return strings.TrimSpace(node.Text), nil
}

// Set sets the text value at the given element path, creating
// intermediate elements when missing, and returns the modified XML.
//
//	C: xmlops_xpath_set()
func (m *XMLOpsModule) Set(xmlStr, xpath, value string) (string, error) {
	root, err := parseNode(xmlStr)
	if err != nil {
		return "", err
	}
	if root == nil {
		return "", fmt.Errorf("xmlops: empty document")
	}
	node, ok := findByPath(root, xpath)
	if !ok {
		// Auto-create the leaf path.
		node, ok = createByPath(root, xpath)
		if !ok {
			return "", fmt.Errorf("xmlops: cannot create path %q", xpath)
		}
	}
	node.Text = value
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := writeNode(&buf, root, 0); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Build constructs an XML document from a map representation. The map is
// expected to use the same shape produced by Parse.
//
//	C: xmlDocSerialize()
func (m *XMLOpsModule) Build(data interface{}) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	root, err := mapToNode(data)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := writeNode(&buf, root, 0); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Validate reports whether xmlStr is well-formed XML. The xsd argument
// is accepted for API compatibility but only basic well-formedness is
// checked (XSD schema validation is not performed).
//
//	C: xmlValidateDoc()
func (m *XMLOpsModule) Validate(xmlStr, xsd string) bool {
	_, err := parseNode(xmlStr)
	return err == nil
}

// --- parsing helpers ---

// parseNode decodes xmlStr into an xmlNode tree.
func parseNode(xmlStr string) (*xmlNode, error) {
	dec := xml.NewDecoder(strings.NewReader(xmlStr))
	var root *xmlNode
	var stack []*xmlNode
	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("xmlops: parse: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{Name: t.Name.Local, Attrs: make(map[string]string)}
			for _, a := range t.Attr {
				node.Attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			} else if root == nil {
				root = node
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				node := stack[len(stack)-1]
				node.Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("xmlops: no root element")
	}
	return root, nil
}

// nodeToMap converts an xmlNode tree into a map representation.
func nodeToMap(n *xmlNode) map[string]interface{} {
	if n == nil {
		return nil
	}
	out := map[string]interface{}{
		"_text":  strings.TrimSpace(n.Text),
		"_attrs": n.Attrs,
	}
	for _, c := range n.Children {
		existing, ok := out[c.Name]
		if !ok {
			out[c.Name] = nodeToMap(c)
			continue
		}
		// Promote to a slice when multiple children share a name.
		switch v := existing.(type) {
		case []interface{}:
			out[c.Name] = append(v, nodeToMap(c))
		default:
			out[c.Name] = []interface{}{existing, nodeToMap(c)}
		}
	}
	return out
}

// findByPath resolves a dot-notation path against a node tree.
func findByPath(root *xmlNode, path string) (*xmlNode, bool) {
	if path == "" {
		return root, root != nil
	}
	segs := strings.Split(path, ".")
	cur := root
	for _, seg := range segs {
		if cur == nil {
			return nil, false
		}
		if seg == cur.Name {
			continue
		}
		next := cur.findChild(seg)
		if next == nil {
			return nil, false
		}
		cur = next
	}
	return cur, cur != nil
}

// createByPath creates intermediate nodes for path and returns the leaf.
func createByPath(root *xmlNode, path string) (*xmlNode, bool) {
	segs := strings.Split(path, ".")
	cur := root
	for _, seg := range segs {
		if seg == cur.Name {
			continue
		}
		next := cur.findChild(seg)
		if next == nil {
			next = &xmlNode{Name: seg, Attrs: make(map[string]string)}
			cur.Children = append(cur.Children, next)
		}
		cur = next
	}
	return cur, true
}

// findChild returns the first child with the given name.
func (n *xmlNode) findChild(name string) *xmlNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// writeNode serialises an xmlNode tree with indentation.
func writeNode(buf *bytes.Buffer, n *xmlNode, depth int) error {
	if n == nil {
		return nil
	}
	indent := strings.Repeat("  ", depth)
	buf.WriteString(indent)
	buf.WriteString("<")
	buf.WriteString(n.Name)
	for k, v := range n.Attrs {
		fmt.Fprintf(buf, " %s=%q", k, v)
	}
	text := strings.TrimSpace(n.Text)
	if len(n.Children) == 0 && text == "" {
		buf.WriteString("/>\n")
		return nil
	}
	buf.WriteString(">")
	if text != "" {
		buf.WriteString(text)
	}
	if len(n.Children) > 0 {
		buf.WriteString("\n")
		for _, c := range n.Children {
			if err := writeNode(buf, c, depth+1); err != nil {
				return err
			}
		}
		buf.WriteString(indent)
	}
	fmt.Fprintf(buf, "</%s>\n", n.Name)
	return nil
}

// mapToNode converts a map representation back into an xmlNode tree.
func mapToNode(data interface{}) (*xmlNode, error) {
	mp, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("xmlops: build expects a map")
	}
	// Find the root element name (first non-meta key).
	for k, v := range mp {
		if k == "_text" || k == "_attrs" {
			continue
		}
		node := &xmlNode{Name: k, Attrs: make(map[string]string)}
		if err := fillNode(node, v); err != nil {
			return nil, err
		}
		return node, nil
	}
	return nil, fmt.Errorf("xmlops: no root element in map")
}

// fillNode populates a node from a map value.
func fillNode(node *xmlNode, v interface{}) error {
	mp, ok := v.(map[string]interface{})
	if !ok {
		node.Text = fmt.Sprintf("%v", v)
		return nil
	}
	if t, ok := mp["_text"].(string); ok {
		node.Text = t
	}
	if a, ok := mp["_attrs"].(map[string]string); ok {
		node.Attrs = a
	}
	for k, child := range mp {
		if k == "_text" || k == "_attrs" {
			continue
		}
		switch c := child.(type) {
		case map[string]interface{}:
			cn := &xmlNode{Name: k, Attrs: make(map[string]string)}
			if err := fillNode(cn, c); err != nil {
				return err
			}
			node.Children = append(node.Children, cn)
		case []interface{}:
			for _, item := range c {
				cn := &xmlNode{Name: k, Attrs: make(map[string]string)}
				if err := fillNode(cn, item); err != nil {
					return err
				}
				node.Children = append(node.Children, cn)
			}
		default:
			cn := &xmlNode{Name: k, Text: fmt.Sprintf("%v", c)}
			node.Children = append(node.Children, cn)
		}
	}
	return nil
}

// --- package-level API ---

var defaultModule = New()

// DefaultXMLOps returns the package-level default XMLOpsModule.
func DefaultXMLOps() *XMLOpsModule {
	return defaultModule
}

// Init (re)initialises the package-level default module.
func Init() {
	defaultModule = New()
}
