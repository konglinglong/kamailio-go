// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_mongodb module tests.
 *
 * These tests do NOT require a running MongoDB server. They exercise config
 * validation, URI building, driver construction, registration and the
 * in-memory mock store (insert/query/update/delete/replace).
 */

package db_mongodb

import (
	"strings"
	"sync"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestMongoConfig verifies default values and URI construction.
func TestMongoConfig(t *testing.T) {
	cfg := DefaultMongoConfig()
	if cfg.Host != "localhost" {
		t.Errorf("default host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != DefaultMongoPort {
		t.Errorf("default port = %d, want %d", cfg.Port, DefaultMongoPort)
	}
	if cfg.Database != DefaultMongoDatabase {
		t.Errorf("default database = %q, want %q", cfg.Database, DefaultMongoDatabase)
	}
	if cfg.AuthSource != DefaultAuthSource {
		t.Errorf("default authSource = %q, want %q", cfg.AuthSource, DefaultAuthSource)
	}
	if cfg.MaxPoolSize != DefaultMaxPoolSize {
		t.Errorf("default maxPoolSize = %d, want %d", cfg.MaxPoolSize, DefaultMaxPoolSize)
	}

	cfg.User = "alice"
	cfg.Password = "s3cr3t"
	cfg.Host = "mongo.example.com"
	cfg.Port = 27018
	cfg.Database = "kamdb"
	cfg.AuthSource = "admin"

	uri := cfg.URI()
	if !strings.HasPrefix(uri, "mongodb://alice:s3cr3t@") {
		t.Errorf("URI() = %q, want creds prefix", uri)
	}
	if !strings.Contains(uri, "mongo.example.com:27018") {
		t.Errorf("URI() = %q, want host:port", uri)
	}
	if !strings.Contains(uri, "/kamdb") {
		t.Errorf("URI() = %q, want database", uri)
	}
	if !strings.Contains(uri, "authSource=admin") {
		t.Errorf("URI() = %q, want authSource", uri)
	}
}

// TestConfigValidation exercises Validate for valid and invalid configs.
func TestConfigValidation(t *testing.T) {
	valid := &MongoConfig{Host: "localhost", Port: 27017, Database: "kam"}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config: unexpected error: %v", err)
	}

	tests := []struct {
		name string
		cfg  *MongoConfig
		want string
	}{
		{"nil config", nil, "nil"},
		{"empty host", &MongoConfig{Port: 27017, Database: "d"}, "host"},
		{"zero port", &MongoConfig{Host: "h", Port: 0, Database: "d"}, "port"},
		{"negative port", &MongoConfig{Host: "h", Port: -1, Database: "d"}, "port"},
		{"oversized port", &MongoConfig{Host: "h", Port: 70000, Database: "d"}, "port"},
		{"empty database", &MongoConfig{Host: "h", Port: 27017}, "database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

// TestNewDriver verifies driver construction and name.
func TestNewDriver(t *testing.T) {
	d := NewMongoDriver()
	if d == nil {
		t.Fatal("NewMongoDriver returned nil")
	}
	if d.Name() != "mongodb" {
		t.Errorf("Name() = %q, want mongodb", d.Name())
	}
}

// TestDriverRegistration verifies the driver is registered via init().
func TestDriverRegistration(t *testing.T) {
	d := db.GetDriver("mongodb")
	if d == nil {
		t.Fatal("expected mongodb driver to be registered")
	}
	if d.Name() != "mongodb" {
		t.Errorf("registered driver name = %q, want mongodb", d.Name())
	}
	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "mongodb" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mongodb not found in RegisteredDrivers()")
	}
}

// TestConnect exercises Connect success and failure paths.
func TestConnect(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
	if err := conn.Connect(&MongoConfig{}); err == nil {
		t.Fatal("expected error for invalid config")
	}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect valid config: %v", err)
	}
}

// TestInsertQuery verifies an insert followed by a query returns the row.
func TestInsertQuery(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	keys := []db.DBKey{
		{Name: "id", Type: db.DBValInt},
		{Name: "user", Type: db.DBValString},
	}
	if err := conn.Insert("location", keys, []db.DBValue{
		{Type: db.DBValInt, IntVal: 1},
		{Type: db.DBValString, StrVal: "alice"},
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	res, err := conn.Query("location", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 1 {
		t.Fatalf("RowCount = %d, want 1", res.RowCount())
	}
	if got := res.Row(0).GetString("user"); got != "alice" {
		t.Errorf("user = %q, want alice", got)
	}
	if got := res.Row(0).GetInt("id"); got != 1 {
		t.Errorf("id = %d, want 1", got)
	}
}

// TestUpdateDelete verifies update and delete operations.
func TestUpdateDelete(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	keys := []db.DBKey{{Name: "id", Type: db.DBValInt}, {Name: "v", Type: db.DBValString}}
	conn.Insert("t", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 1}, {Type: db.DBValString, StrVal: "a"}})
	conn.Insert("t", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 2}, {Type: db.DBValString, StrVal: "b"}})

	n, err := conn.Update("t",
		[]db.DBKey{{Name: "v", Type: db.DBValString}},
		[]db.DBValue{{Type: db.DBValString, StrVal: "updated"}},
		[]db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValInt, IntVal: 2}}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n != 1 {
		t.Errorf("Update affected = %d, want 1", n)
	}
	res, _ := conn.Query("t", nil, []db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValInt, IntVal: 2}}}, "", 0, 0)
	if res.Row(0).GetString("v") != "updated" {
		t.Errorf("v = %q, want updated", res.Row(0).GetString("v"))
	}

	deleted, err := conn.Delete("t", []db.DBCondition{{Key: "id", Op: "=", Value: db.DBValue{Type: db.DBValInt, IntVal: 1}}})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Delete = %d, want 1", deleted)
	}
	res, _ = conn.Query("t", nil, nil, "", 0, 0)
	if res.RowCount() != 1 {
		t.Errorf("after delete RowCount = %d, want 1", res.RowCount())
	}
}

// TestReplace verifies upsert behaviour on the first key.
func TestReplace(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	keys := []db.DBKey{{Name: "id", Type: db.DBValInt}, {Name: "v", Type: db.DBValString}}
	if err := conn.Replace("t", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 1}, {Type: db.DBValString, StrVal: "a"}}); err != nil {
		t.Fatalf("Replace insert: %v", err)
	}
	// Same id -> update.
	if err := conn.Replace("t", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 1}, {Type: db.DBValString, StrVal: "b"}}); err != nil {
		t.Fatalf("Replace update: %v", err)
	}
	res, _ := conn.Query("t", nil, nil, "", 0, 0)
	if res.RowCount() != 1 {
		t.Errorf("after replace RowCount = %d, want 1", res.RowCount())
	}
	if res.Row(0).GetString("v") != "b" {
		t.Errorf("v = %q, want b", res.Row(0).GetString("v"))
	}
}

// TestPingClose verifies Ping/Close behaviour.
func TestPingClose(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := conn.Ping(); err != nil {
		t.Errorf("Ping before close: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := conn.Ping(); err == nil {
		t.Error("Ping after close: expected error")
	}
}

// TestGetCollection verifies the collection accessor returns documents.
func TestGetCollection(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := conn.GetCollection("missing"); got != nil {
		t.Errorf("GetCollection missing = %v, want nil", got)
	}
	keys := []db.DBKey{{Name: "id", Type: db.DBValInt}}
	conn.Insert("c", keys, []db.DBValue{{Type: db.DBValInt, IntVal: 7}})
	got := conn.GetCollection("c")
	docs, ok := got.([]map[string]interface{})
	if !ok {
		t.Fatalf("GetCollection returned %T, want []map[string]interface{}", got)
	}
	if len(docs) != 1 {
		t.Fatalf("docs len = %d, want 1", len(docs))
	}
	if docs[0]["id"] != "7" {
		t.Errorf("docs[0][id] = %v, want 7", docs[0]["id"])
	}
}

// TestParseMongoURL verifies URL parsing.
func TestParseMongoURL(t *testing.T) {
	cfg, err := parseMongoURL("mongodb://alice:pw@mongo.host:27018/kamdb?authSource=admin")
	if err != nil {
		t.Fatalf("parseMongoURL: %v", err)
	}
	if cfg.User != "alice" {
		t.Errorf("user = %q, want alice", cfg.User)
	}
	if cfg.Password != "pw" {
		t.Errorf("password = %q, want pw", cfg.Password)
	}
	if cfg.Host != "mongo.host" {
		t.Errorf("host = %q, want mongo.host", cfg.Host)
	}
	if cfg.Port != 27018 {
		t.Errorf("port = %d, want 27018", cfg.Port)
	}
	if cfg.Database != "kamdb" {
		t.Errorf("database = %q, want kamdb", cfg.Database)
	}
	if cfg.AuthSource != "admin" {
		t.Errorf("authSource = %q, want admin", cfg.AuthSource)
	}
}

// TestConcurrentAccess exercises the connection under -race by performing
// concurrent inserts and queries.
func TestConcurrentAccess(t *testing.T) {
	conn := &MongoConn{}
	if err := conn.Connect(DefaultMongoConfig()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	keys := []db.DBKey{{Name: "id", Type: db.DBValInt}}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = conn.Insert("c", keys, []db.DBValue{{Type: db.DBValInt, IntVal: int64(n)}})
			_, _ = conn.Query("c", nil, nil, "", 0, 0)
		}(i)
	}
	wg.Wait()
	res, err := conn.Query("c", nil, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.RowCount() != 20 {
		t.Errorf("RowCount = %d, want 20", res.RowCount())
	}
}
