// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go - db_mysql module tests.
 *
 * These tests do NOT require a running MySQL server. They exercise config
 * validation, DSN building, driver construction and registration only.
 */

package db_mysql

import (
	"strings"
	"testing"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
)

// TestMySQLConfig verifies default values and DSN construction.
func TestMySQLConfig(t *testing.T) {
	cfg := DefaultMySQLConfig()
	if cfg.Host != "localhost" {
		t.Errorf("default host = %q, want localhost", cfg.Host)
	}
	if cfg.Port != DefaultMySQLPort {
		t.Errorf("default port = %d, want %d", cfg.Port, DefaultMySQLPort)
	}
	if cfg.User != "root" {
		t.Errorf("default user = %q, want root", cfg.User)
	}
	if cfg.MaxOpenConns != 10 {
		t.Errorf("default MaxOpenConns = %d, want 10", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 5 {
		t.Errorf("default MaxIdleConns = %d, want 5", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 5*time.Minute {
		t.Errorf("default ConnMaxLifetime = %v, want 5m", cfg.ConnMaxLifetime)
	}

	cfg.Host = "db.example.com"
	cfg.Port = 13306
	cfg.User = "kamailio"
	cfg.Password = "s3cr3t"
	cfg.Database = "kamailio"

	dsn := cfg.DSN()
	want := "kamailio:s3cr3t@tcp(db.example.com:13306)/kamailio"
	if dsn != want {
		t.Errorf("DSN() = %q, want %q", dsn, want)
	}
}

// TestNewDriver verifies driver construction and name.
func TestNewDriver(t *testing.T) {
	d := NewMySQLDriver()
	if d == nil {
		t.Fatal("NewMySQLDriver returned nil")
	}
	if d.Name() != "mysql" {
		t.Errorf("Name() = %q, want mysql", d.Name())
	}
}

// TestDriverRegistration verifies the driver is registered with the db
// package's global registry via init().
func TestDriverRegistration(t *testing.T) {
	d := db.GetDriver("mysql")
	if d == nil {
		t.Fatal("expected mysql driver to be registered")
	}
	if d.Name() != "mysql" {
		t.Errorf("registered driver name = %q, want mysql", d.Name())
	}

	// Ensure it appears in the list of registered drivers.
	found := false
	for _, name := range db.RegisteredDrivers() {
		if name == "mysql" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mysql not found in RegisteredDrivers()")
	}
}

// TestConfigValidation exercises the Validate method for valid and invalid
// configurations.
func TestConfigValidation(t *testing.T) {
	valid := &MySQLConfig{
		Host:     "localhost",
		Port:     3306,
		User:     "root",
		Database: "kamailio",
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config: unexpected error: %v", err)
	}

	tests := []struct {
		name string
		cfg  *MySQLConfig
		want string
	}{
		{"nil config", nil, "nil"},
		{"empty host", &MySQLConfig{Port: 3306, User: "u", Database: "d"}, "host"},
		{"zero port", &MySQLConfig{Host: "h", Port: 0, User: "u", Database: "d"}, "port"},
		{"negative port", &MySQLConfig{Host: "h", Port: -1, User: "u", Database: "d"}, "port"},
		{"oversized port", &MySQLConfig{Host: "h", Port: 70000, User: "u", Database: "d"}, "port"},
		{"empty user", &MySQLConfig{Host: "h", Port: 3306, Database: "d"}, "user"},
		{"empty database", &MySQLConfig{Host: "h", Port: 3306, User: "u"}, "database"},
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

// TestParseMySQLURL verifies URL parsing into a config.
func TestParseMySQLURL(t *testing.T) {
	cfg, err := parseMySQLURL("mysql://alice:pw@db.host:3307/kamdb")
	if err != nil {
		t.Fatalf("parseMySQLURL: %v", err)
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
	if cfg.Port != 3307 {
		t.Errorf("port = %d, want 3307", cfg.Port)
	}
	if cfg.Database != "kamdb" {
		t.Errorf("database = %q, want kamdb", cfg.Database)
	}
}

// TestConnectNilConfig ensures Connect rejects a nil config without
// attempting to open a database connection.
func TestConnectNilConfig(t *testing.T) {
	_, err := Connect(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

// TestConnectInvalidConfig ensures Connect rejects an invalid config.
func TestConnectInvalidConfig(t *testing.T) {
	_, err := Connect(&MySQLConfig{})
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
