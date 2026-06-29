// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_postgres module tests.
 *
 * These tests do NOT require a running PostgreSQL server. They exercise
 * config validation, DSN building, driver construction and registration only.
 */

package db_postgres

import (
	"strings"
	"testing"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestPostgresConfig verifies default values and DSN construction.
func TestPostgresConfig(t *testing.T) {
	cfg := DefaultPostgresConfig()
	if cfg.Host != "localhost" {
		t.Errorf("default host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != DefaultPostgresPort {
		t.Errorf("default port = %d, want %d", cfg.Port, DefaultPostgresPort)
	}
	if cfg.User != "postgres" {
		t.Errorf("default user = %q, want postgres", cfg.User)
	}
	if cfg.SSLMode != DefaultSSLMode {
		t.Errorf("default sslmode = %q, want %q", cfg.SSLMode, DefaultSSLMode)
	}
	if cfg.MaxOpenConns != 10 {
		t.Errorf("default MaxOpenConns = %d, want 10", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("default MaxIdleConns = %d, want 5", cfg.MaxIdleConns)
	}

	cfg.Host = "db.example.com"
	cfg.Port = 15432
	cfg.User = "kamailio"
	cfg.Password = "s3cr3t"
	cfg.Database = "kamailio"
	cfg.SSLMode = "require"

	dsn := cfg.DSN()
	if !strings.Contains(dsn, "host=db.example.com") {
		t.Errorf("DSN missing host: %q", dsn)
	}
	if !strings.Contains(dsn, "port=15432") {
		t.Errorf("DSN missing port: %q", dsn)
	}
	if !strings.Contains(dsn, "user=kamailio") {
		t.Errorf("DSN missing user: %q", dsn)
	}
	if !strings.Contains(dsn, "password=s3cr3t") {
		t.Errorf("DSN missing password: %q", dsn)
	}
	if !strings.Contains(dsn, "dbname=kamailio") {
		t.Errorf("DSN missing dbname: %q", dsn)
	}
	if !strings.Contains(dsn, "sslmode=require") {
		t.Errorf("DSN missing sslmode: %q", dsn)
	}
}

// TestNewDriver verifies driver construction and name.
func TestNewDriver(t *testing.T) {
	d := NewPostgresDriver()
	if d == nil {
		t.Fatal("NewPostgresDriver returned nil")
	}
	if d.Name() != "postgres" {
		t.Errorf("Name() = %q, want postgres", d.Name())
	}
}

// TestDriverRegistration verifies the driver is registered with the db
// package's global registry via init().
func TestDriverRegistration(t *testing.T) {
	d := db.GetDriver("postgres")
	if d == nil {
		t.Fatal("expected postgres driver to be registered")
	}
	if d.Name() != "postgres" {
		t.Errorf("registered driver name = %q, want postgres", d.Name())
	}

	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "postgres" {
			found = true
			break
		}
	}
	if !found {
		t.Error("postgres not found in RegisteredDrivers()")
	}
}

// TestConfigValidation exercises the Validate method for valid and invalid
// configurations.
func TestConfigValidation(t *testing.T) {
	valid := &PostgresConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Database: "kamailio",
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config: unexpected error: %v", err)
	}

	tests := []struct {
		name string
		cfg  *PostgresConfig
		want string
	}{
		{"nil config", nil, "nil"},
		{"empty host", &PostgresConfig{Port: 5432, User: "u", Database: "d"}, "host"},
		{"zero port", &PostgresConfig{Host: "h", Port: 0, User: "u", Database: "d"}, "port"},
		{"negative port", &PostgresConfig{Host: "h", Port: -1, User: "u", Database: "d"}, "port"},
		{"oversized port", &PostgresConfig{Host: "h", Port: 70000, User: "u", Database: "d"}, "port"},
		{"empty user", &PostgresConfig{Host: "h", Port: 5432, Database: "d"}, "user"},
		{"empty database", &PostgresConfig{Host: "h", Port: 5432, User: "u"}, "database"},
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

// TestParsePostgresURL verifies URL parsing into a config.
func TestParsePostgresURL(t *testing.T) {
	cfg, err := parsePostgresURL("postgres://alice:pw@db.host:5433/kamdb?sslmode=require")
	if err != nil {
		t.Fatalf("parsePostgresURL: %v", err)
	}
	if cfg.User != "alice" {
		t.Errorf("user = %q, want alice", cfg.User)
	}
	if cfg.Password != "pw" {
		t.Errorf("password = %q, want pw", cfg.Password)
	}
	if cfg.Host != "db.host" {
		t.Errorf("host = %q, want db.host", cfg.Host)
	}
	if cfg.Port != 5433 {
		t.Errorf("port = %d, want 5433", cfg.Port)
	}
	if cfg.Database != "kamdb" {
		t.Errorf("database = %q, want kamdb", cfg.Database)
	}
	if cfg.SSLMode != "require" {
		t.Errorf("sslmode = %q, want require", cfg.SSLMode)
	}
}

// TestConnectNilConfig ensures Connect rejects a nil config.
func TestConnectNilConfig(t *testing.T) {
	_, err := Connect(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

// TestConnectInvalidConfig ensures Connect rejects an invalid config.
func TestConnectInvalidConfig(t *testing.T) {
	_, err := Connect(&PostgresConfig{})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

// TestGetVersionNilDB ensures GetVersion rejects a nil *sql.DB.
func TestGetVersionNilDB(t *testing.T) {
	_, err := GetVersion(nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

// TestPingNilDB ensures Ping rejects a nil *sql.DB.
func TestPingNilDB(t *testing.T) {
	if err := Ping(nil); err == nil {
		t.Fatal("expected error for nil db")
	}
}
