// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * DB cluster module - database node cluster management.
 * Port of the kamailio db_cluster module (src/modules/db_cluster).
 *
 * The module maintains a registry of database nodes identified by name,
 * each with a driver and connection URL. Execute runs a query against
 * the first available node. It is safe for concurrent use.
 */

package db_cluster

import (
	"errors"
	"sync"
)

// ClusterNode describes a single database node in the cluster.
type ClusterNode struct {
	Name   string
	Driver string
	URL    string
}

// DBClusterModule maintains a registry of database nodes.
type DBClusterModule struct {
	mu    sync.RWMutex
	nodes map[string]*ClusterNode
	order []string
}

// New creates a DBClusterModule with empty storage.
func New() *DBClusterModule {
	return &DBClusterModule{nodes: make(map[string]*ClusterNode)}
}

// AddNode registers a node, overwriting any previous node with the same
// name.
//
//	C: db_cluster_add_node()
func (m *DBClusterModule) AddNode(name, driver, url string) {
	if name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nodes == nil {
		m.nodes = make(map[string]*ClusterNode)
	}
	if _, ok := m.nodes[name]; !ok {
		m.order = append(m.order, name)
	}
	m.nodes[name] = &ClusterNode{Name: name, Driver: driver, URL: url}
}

// RemoveNode removes the node identified by name. Returns true when a
// node was removed.
//
//	C: db_cluster_remove_node()
func (m *DBClusterModule) RemoveNode(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[name]; !ok {
		return false
	}
	delete(m.nodes, name)
	for i, n := range m.order {
		if n == name {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return true
}

// GetNode returns the URL of the node identified by name.
func (m *DBClusterModule) GetNode(name string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[name]
	if !ok {
		return "", false
	}
	return n.URL, true
}

// Execute runs the given query against the first available node and
// returns the query string as a mock result. It returns an error when
// no nodes are registered.
//
//	C: db_cluster_execute()
func (m *DBClusterModule) Execute(query string) (interface{}, error) {
	m.mu.RLock()
	if len(m.order) == 0 {
		m.mu.RUnlock()
		return nil, errors.New("db_cluster: no nodes registered")
	}
	node := m.nodes[m.order[0]]
	m.mu.RUnlock()
	return query + "@" + node.URL, nil
}

// NodeCount returns the number of registered nodes.
func (m *DBClusterModule) NodeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes)
}

// ---------------------------------------------------------------------------
// Process-wide singleton (mirrors the C module's global state)
// ---------------------------------------------------------------------------

var (
	defaultMu sync.RWMutex
	defaultM  *DBClusterModule
)

// DefaultDBCluster returns the process-wide DBClusterModule.
func DefaultDBCluster() *DBClusterModule {
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

// Init (re)initialises the process-wide DBClusterModule.
func Init() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultM = New()
}

// AddNode is the package-level wrapper around DefaultDBCluster().AddNode.
func AddNode(name, driver, url string) { DefaultDBCluster().AddNode(name, driver, url) }

// RemoveNode is the package-level wrapper around DefaultDBCluster().RemoveNode.
func RemoveNode(name string) bool { return DefaultDBCluster().RemoveNode(name) }

// GetNode is the package-level wrapper around DefaultDBCluster().GetNode.
func GetNode(name string) (string, bool) { return DefaultDBCluster().GetNode(name) }

// Execute is the package-level wrapper around DefaultDBCluster().Execute.
func Execute(query string) (interface{}, error) { return DefaultDBCluster().Execute(query) }

// NodeCount is the package-level wrapper around DefaultDBCluster().NodeCount.
func NodeCount() int { return DefaultDBCluster().NodeCount() }
