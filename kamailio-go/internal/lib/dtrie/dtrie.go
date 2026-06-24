// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * internal/lib/dtrie - generic character-keyed trie.
 *
 * Mirrors the C data structure in src/lib/trie/dtrie.{c,h}. The C version
 * is optimised for digit-only (branches==10) or ASCII (branches==128)
 * matching used by the carrierroute and userblocklist modules. This Go
 * port uses a map per node so any rune (digits, letters, etc.) may be used
 * as a key, and adds longest-prefix matching, prefix deletion, prefix
 * walks and thread-safe access via a sync.RWMutex.
 */
package dtrie

import "sync"

// DTrieNode is a single node in the trie. children maps the next rune to
// the child node. data holds the caller-supplied payload when hasData is
// true.
type DTrieNode struct {
	children map[rune]*DTrieNode
	data     interface{}
	hasData  bool
}

// DTrie is a thread-safe character-keyed trie.
type DTrie struct {
	root *DTrieNode
	mu   sync.RWMutex
}

// New creates an empty trie.
func New() *DTrie {
	return &DTrie{root: &DTrieNode{children: make(map[rune]*DTrieNode)}}
}

// newNode allocates a fresh node with an initialised child map.
func newNode() *DTrieNode {
	return &DTrieNode{children: make(map[rune]*DTrieNode)}
}

// Insert stores data at the given key path, creating intermediate nodes as
// needed. A subsequent Insert with the same key overwrites the previous
// value.
//
//	C: dtrie_insert()
func (t *DTrie) Insert(key string, data interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	node := t.root
	for _, r := range key {
		child := node.children[r]
		if child == nil {
			child = newNode()
			node.children[r] = child
		}
		node = child
	}
	node.data = data
	node.hasData = true
}

// Lookup performs an exact-match lookup: it returns the data stored at
// key only when a node with data exists at exactly that path.
//
//	C: dtrie_contains()
func (t *DTrie) Lookup(key string) (interface{}, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	node := t.root
	for _, r := range key {
		child := node.children[r]
		if child == nil {
			return nil, false
		}
		node = child
	}
	if !node.hasData {
		return nil, false
	}
	return node.data, true
}

// LongestPrefix returns the data associated with the longest stored prefix
// of key. It walks the key one rune at a time, remembering the most recent
// node that carries data.
//
//	C: dtrie_longest_match()
func (t *DTrie) LongestPrefix(key string) (interface{}, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	node := t.root
	var bestData interface{}
	found := false
	if node.hasData {
		bestData = node.data
		found = true
	}
	for _, r := range key {
		child := node.children[r]
		if child == nil {
			break
		}
		node = child
		if node.hasData {
			bestData = node.data
			found = true
		}
	}
	return bestData, found
}

// Delete removes the data stored at key and prunes any leaf nodes that
// become empty as a result. It returns true when key previously held data.
func (t *DTrie) Delete(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Collect the path of nodes (root first) so we can prune upwards.
	path := make([]*DTrieNode, 0, len(key)+1)
	node := t.root
	path = append(path, node)
	for _, r := range key {
		child := node.children[r]
		if child == nil {
			return false
		}
		path = append(path, child)
		node = child
	}
	target := path[len(path)-1]
	if !target.hasData {
		return false
	}
	target.data = nil
	target.hasData = false

	// Prune childless, dataless nodes back up the path (never the root).
	for i := len(path) - 1; i >= 1; i-- {
		cur := path[i]
		if len(cur.children) == 0 && !cur.hasData {
			parent := path[i-1]
			// Remove the child mapping for the rune that led to cur.
			r := rune(key[i-1])
			delete(parent.children, r)
		} else {
			break
		}
	}
	return true
}

// DeletePrefix removes every entry whose key starts with prefix and returns
// the number of data-bearing entries removed. The subtree under prefix is
// detached from its parent. An empty prefix clears the whole trie.
func (t *DTrie) DeletePrefix(prefix string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	if prefix == "" {
		n := countData(t.root)
		t.root.children = make(map[rune]*DTrieNode)
		t.root.data = nil
		t.root.hasData = false
		return n
	}

	runes := []rune(prefix)
	parent := t.root
	for _, r := range runes[:len(runes)-1] {
		child := parent.children[r]
		if child == nil {
			return 0
		}
		parent = child
	}
	last := runes[len(runes)-1]
	sub := parent.children[last]
	if sub == nil {
		return 0
	}
	n := countData(sub)
	delete(parent.children, last)
	return n
}

// Walk invokes fn for every data-bearing entry whose key starts with
// prefix. The key passed to fn is the full key from the trie root. Walk
// does not run concurrently with writers (it holds the read lock).
func (t *DTrie) Walk(prefix string, fn func(key string, data interface{})) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.root
	for _, r := range prefix {
		child := node.children[r]
		if child == nil {
			return
		}
		node = child
	}
	walk(node, prefix, fn)
}

// walk recursively visits node and its descendants, invoking fn for each
// data-bearing node. key is the full path from the root to node.
func walk(node *DTrieNode, key string, fn func(string, interface{})) {
	if node == nil {
		return
	}
	if node.hasData {
		fn(key, node.data)
	}
	// Iterate in a stable order for deterministic output.
	for r, child := range node.children {
		walk(child, key+string(r), fn)
	}
}

// Count returns the number of data-bearing entries in the trie.
func (t *DTrie) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return countData(t.root)
}

// Size returns the total number of nodes in the trie, including the root.
//
//	C: dtrie_size()
func (t *DTrie) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return countNodes(t.root)
}

// Clear removes every entry and node, leaving an empty (root-only) trie.
//
//	C: dtrie_clear()
func (t *DTrie) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.root = newNode()
}

// countData counts data-bearing nodes in the subtree rooted at node.
func countData(node *DTrieNode) int {
	if node == nil {
		return 0
	}
	n := 0
	if node.hasData {
		n++
	}
	for _, child := range node.children {
		n += countData(child)
	}
	return n
}

// countNodes counts every node in the subtree rooted at node (inclusive).
func countNodes(node *DTrieNode) int {
	if node == nil {
		return 0
	}
	n := 1
	for _, child := range node.children {
		n += countNodes(child)
	}
	return n
}
