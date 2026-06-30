// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * kamctl - operator control CLI for the Kamailio-Go SIP server.
 *
 * A Go port of utils/kamctl/kamctl. It performs two roles:
 *
 *   1. Live server control via JSON-RPC (ping, stats, dialog list,
 *      usrloc dump/lookup/rm, ...). This reuses the same JSON-RPC
 *      transport as kamcmd, so the same -s server flag is accepted.
 *
 *   2. Subscriber database management (add / passwd / rm / list) by
 *      talking directly to the configured DB URL. The subscriber table
 *      schema matches Kamailio's default: columns
 *        username, domain, password, ha1, ha1b, email_address.
 *
 * Usage:
 *
 *	kamctl [global flags] <command> [args...]
 *
 * Global flags:
 *   -s addr        JSON-RPC server (default http://localhost:2048)
 *   -db url        DB URL for subscriber commands
 *                  (e.g. sqlite:./kamailio.db, sqlite::memory:)
 *   -realm realm   SIP auth realm (default: kamailio.org)
 *   -f json|text   output format for RPC results (default: json)
 *
 * Commands:
 *   ping                          ping the server via JSON-RPC
 *   stats                         show server metrics
 *   status                        show subsystem status
 *   version                       print kamctl version
 *   dlg list [limit]              list active dialogs
 *   ul show [domain]              dump usrloc bindings (all or one domain)
 *   ul lookup <domain> <aor>      look up an AOR's contacts
 *   ul rm <domain> <aor>          remove an AOR from usrloc
 *   sub add <user> <pass> [email] add a subscriber (computes HA1/HA1B)
 *   sub passwd <user> <newpass>   change a subscriber's password
 *   sub rm <user>                 remove a subscriber
 *   sub list [user]               list subscribers (all or one)
 *   rpc <method> [params...]      send an arbitrary JSON-RPC method
 *   help                          show this help
 */

package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/db"
	"github.com/kamailio/kamailio-go/internal/core/rpc"
)

// stdout / stderr are package-level so tests can swap them for buffers.
// They default to os.Stdout / os.Stderr in main().
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// Version is the kamctl binary version. It is overridden at build time
// via -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

// DefaultServer is the JSON-RPC endpoint used when -s is not supplied.
const DefaultServer = "http://localhost:2048"

// DefaultRealm is used to compute HA1 when -realm is not supplied.
const DefaultRealm = "kamailio.org"

// flags holds the parsed global CLI flags shared by every subcommand.
type flags struct {
	server string
	dbURL  string
	realm  string
	format string
}

// subscriberColumn lists the columns kamctl manages on the subscriber
// table. The schema matches Kamailio's standard layout so existing
// deployments can be reused.
var subscriberColumns = []db.DBKey{
	{Name: "username", Type: db.DBValString},
	{Name: "domain", Type: db.DBValString},
	{Name: "password", Type: db.DBValString},
	{Name: "ha1", Type: db.DBValString},
	{Name: "ha1b", Type: db.DBValString},
	{Name: "email_address", Type: db.DBValString},
}

// ha1 computes H(username:realm:password) using MD5 (Kamailio's
// default algorithm when none is stored).
func ha1(username, realm, password string) string {
	h := md5.Sum([]byte(username + ":" + realm + ":" + password))
	return hex.EncodeToString(h[:])
}

// ha1b computes H(username@realm:realm:password). Kamailio stores this
// alongside ha1 so that lookups against either form of the AOR work.
func ha1b(username, realm, password string) string {
	h := md5.Sum([]byte(username + "@" + realm + ":" + realm + ":" + password))
	return hex.EncodeToString(h[:])
}

// parseDriverURL splits a kamctl DB URL of the form
// "<driver>:<rest>" into a driver name and a db.Open-compatible URL.
// "sqlite:./foo.db" -> ("sqlite", "./foo.db").
// "sqlite::memory:" -> ("sqlite", ":memory:").
func parseDriverURL(url string) (driver, rest string, err error) {
	idx := strings.Index(url, ":")
	if idx <= 0 {
		return "", "", fmt.Errorf("malformed db url %q (want driver:rest)", url)
	}
	return url[:idx], url[idx+1:], nil
}

// openDB opens the DB connection described by url. The first call
// registers the SQLite driver with the global registry.
func openDB(url string) (db.DBConn, error) {
	driver, rest, err := parseDriverURL(url)
	if err != nil {
		return nil, err
	}
	if d := db.GetDriver(driver); d == nil {
		// Lazily register the SQLite driver the first time it is
		// requested. Other drivers (postgres, mysql, redis) must be
		// registered by the caller's environment; kamctl ships with
		// SQLite only.
		if driver == "sqlite" {
			if err := db.RegisterDriver(&db.SQLiteDriver{}); err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("db driver %q not registered (kamctl only ships sqlite)", driver)
		}
	}
	return db.Open(driver, rest)
}

// ensureSubscriberTable creates the subscriber table if it does not yet
// exist. The SQLite driver auto-creates tables on first Insert, but we
// issue an explicit CREATE TABLE so subsequent SELECTs do not fail with
// "no such table" before any Insert has run.
func ensureSubscriberTable(conn db.DBConn) error {
	const ddl = `CREATE TABLE IF NOT EXISTS subscriber (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		domain TEXT NOT NULL,
		password TEXT,
		ha1 TEXT,
		ha1b TEXT,
		email_address TEXT
	)`
	_, err := conn.Raw(ddl)
	return err
}

// usage prints the help text to the appropriate stream and returns the
// requested exit code. Tests rely on it not calling os.Exit.
func usage(code int) int {
	out := stderr
	if code == 0 {
		out = stdout
	}
	fmt.Fprintf(out, `kamctl - operator control CLI for Kamailio-Go

Usage:
  kamctl [global flags] <command> [args...]

Global flags:
  -s addr        JSON-RPC server (default %s)
  -db url        DB URL for subscriber commands (driver:rest, e.g. sqlite:./kamailio.db)
  -realm realm   SIP auth realm (default %s)
  -f json|text   output format for RPC results (default: json)

Commands:
  ping                          ping the server via JSON-RPC
  stats                         show server metrics
  status                        show subsystem status
  version                       print kamctl version
  dlg list [limit]              list active dialogs
  ul show [domain]              dump usrloc bindings (all or one domain)
  ul lookup <domain> <aor>      look up an AOR's contacts
  ul rm <domain> <aor>          remove an AOR from usrloc
  sub add <user> <pass> [email] add a subscriber (computes HA1/HA1B)
  sub passwd <user> <newpass>   change a subscriber's password
  sub rm <user>                 remove a subscriber
  sub list [user]               list subscribers (all or one)
  rpc <method> [params...]      send an arbitrary JSON-RPC method
  help                          show this help
`, DefaultServer, DefaultRealm)
	return code
}

// run is the entry point, separated from main for testability.
func run(args []string) int {
	fs := flag.NewFlagSet("kamctl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := &flags{
		server: DefaultServer,
		realm:  DefaultRealm,
		format: "json",
	}
	fs.StringVar(&g.server, "s", g.server, "JSON-RPC server address")
	fs.StringVar(&g.dbURL, "db", "", "DB URL (driver:rest, e.g. sqlite:./kamailio.db)")
	fs.StringVar(&g.realm, "realm", g.realm, "SIP auth realm")
	fs.StringVar(&g.format, "f", g.format, "output format: json|text")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rest := fs.Args()
	if len(rest) == 0 {
		return usage(0)
	}
	cmd := rest[0]
	args = rest[1:]

	switch cmd {
	case "help", "-h", "--help":
		return usage(0)
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "kamctl version %s\n", Version)
		return 0
	case "ping":
		return cmdRPC(g, "kamailio.ping")
	case "stats":
		return cmdRPC(g, "kamailio.stats")
	case "status":
		return cmdRPC(g, "kamailio.status")
	case "dlg":
		return cmdDlg(g, args)
	case "ul":
		return cmdUL(g, args)
	case "sub":
		return cmdSub(g, args)
	case "rpc":
		if len(args) == 0 {
			fmt.Fprintln(stderr, "kamctl rpc: missing method")
			return 2
		}
		return cmdRPC(g, args[0], args[1:]...)
	}

	fmt.Fprintf(stderr, "kamctl: unknown command %q\n", cmd)
	return usage(2)
}

// cmdRPC sends a JSON-RPC method to the server and pretty-prints the
// result. It reuses the rpc package's request/response types so the
// wire format is identical to kamcmd.
func cmdRPC(g *flags, method string, params ...string) int {
	client := rpc.NewClient(g.server, 30*time.Second)
	result, err := client.Call(method, strParams(params)...)
	if err != nil {
		fmt.Fprintf(stderr, "kamctl: %v\n", err)
		return 1
	}
	rpc.PrintResult(stdout, result, g.format)
	return 0
}

// strParams converts a []string to []interface{} for the JSON-RPC
// client. Strings that look like integers are still passed as strings
// — the server is expected to coerce as needed.
func strParams(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// cmdDlg dispatches the "dlg" subcommand.
func cmdDlg(g *flags, args []string) int {
	if len(args) == 0 {
		return cmdRPC(g, "kamailio.dialog.list")
	}
	switch args[0] {
	case "list":
		return cmdRPC(g, "kamailio.dialog.list", args[1:]...)
	default:
		fmt.Fprintf(stderr, "kamctl dlg: unknown subcommand %q\n", args[0])
		return 2
	}
}

// cmdUL dispatches the "ul" subcommand.
func cmdUL(g *flags, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "kamctl ul: missing subcommand (show|lookup|rm)")
		return 2
	}
	switch args[0] {
	case "show", "dump":
		return cmdRPC(g, "kamailio.ul.dump", args[1:]...)
	case "lookup":
		if len(args) < 3 {
			fmt.Fprintln(stderr, "usage: kamctl ul lookup <domain> <aor>")
			return 2
		}
		return cmdRPC(g, "kamailio.ul.lookup", args[1], args[2])
	case "rm":
		if len(args) < 3 {
			fmt.Fprintln(stderr, "usage: kamctl ul rm <domain> <aor>")
			return 2
		}
		return cmdRPC(g, "kamailio.ul.rm", args[1], args[2])
	default:
		fmt.Fprintf(stderr, "kamctl ul: unknown subcommand %q\n", args[0])
		return 2
	}
}

// cmdSub dispatches the "sub" (subscriber) subcommand. These commands
// talk directly to the DB URL supplied via -db.
func cmdSub(g *flags, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "kamctl sub: missing subcommand (add|passwd|rm|list)")
		return 2
	}
	if g.dbURL == "" {
		fmt.Fprintln(stderr, "kamctl sub: -db URL is required for subscriber commands")
		return 2
	}
	conn, err := openDB(g.dbURL)
	if err != nil {
		fmt.Fprintf(stderr, "kamctl: open db: %v\n", err)
		return 1
	}
	defer conn.Close()
	if err := ensureSubscriberTable(conn); err != nil {
		fmt.Fprintf(stderr, "kamctl: ensure subscriber table: %v\n", err)
		return 1
	}
	switch args[0] {
	case "add":
		return subAdd(g, conn, args[1:])
	case "passwd":
		return subPasswd(g, conn, args[1:])
	case "rm":
		return subRm(conn, args[1:])
	case "list":
		return subList(conn, args[1:])
	default:
		fmt.Fprintf(stderr, "kamctl sub: unknown subcommand %q\n", args[0])
		return 2
	}
}

// subAdd implements "kamctl sub add <user> <pass> [email]".
func subAdd(g *flags, conn db.DBConn, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: kamctl sub add <user> <pass> [email]")
		return 2
	}
	user, pass := args[0], args[1]
	email := ""
	if len(args) >= 3 {
		email = args[2]
	}
	values := []db.DBValue{
		db.NewStringValue(user),
		db.NewStringValue(g.realm),
		db.NewStringValue(pass),
		db.NewStringValue(ha1(user, g.realm, pass)),
		db.NewStringValue(ha1b(user, g.realm, pass)),
		db.NewStringValue(email),
	}
	if err := conn.Insert("subscriber", subscriberColumns, values); err != nil {
		fmt.Fprintf(stderr, "kamctl: insert subscriber: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "added subscriber %s@%s\n", user, g.realm)
	return 0
}

// subPasswd implements "kamctl sub passwd <user> <newpass>".
func subPasswd(g *flags, conn db.DBConn, args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: kamctl sub passwd <user> <newpass>")
		return 2
	}
	user, pass := args[0], args[1]
	keys := []db.DBKey{
		{Name: "password", Type: db.DBValString},
		{Name: "ha1", Type: db.DBValString},
		{Name: "ha1b", Type: db.DBValString},
	}
	values := []db.DBValue{
		db.NewStringValue(pass),
		db.NewStringValue(ha1(user, g.realm, pass)),
		db.NewStringValue(ha1b(user, g.realm, pass)),
	}
	where := []db.DBCondition{
		{Key: "username", Op: "=", Value: db.NewStringValue(user)},
	}
	n, err := conn.Update("subscriber", keys, values, where)
	if err != nil {
		fmt.Fprintf(stderr, "kamctl: update subscriber: %v\n", err)
		return 1
	}
	if n == 0 {
		fmt.Fprintf(stderr, "kamctl: no such subscriber %q\n", user)
		return 1
	}
	fmt.Fprintf(stdout, "updated password for %s@%s\n", user, g.realm)
	return 0
}

// subRm implements "kamctl sub rm <user>".
func subRm(conn db.DBConn, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: kamctl sub rm <user>")
		return 2
	}
	user := args[0]
	where := []db.DBCondition{
		{Key: "username", Op: "=", Value: db.NewStringValue(user)},
	}
	n, err := conn.Delete("subscriber", where)
	if err != nil {
		fmt.Fprintf(stderr, "kamctl: delete subscriber: %v\n", err)
		return 1
	}
	if n == 0 {
		fmt.Fprintf(stderr, "kamctl: no such subscriber %q\n", user)
		return 1
	}
	fmt.Fprintf(stdout, "removed %d subscriber(s) matching %q\n", n, user)
	return 0
}

// subList implements "kamctl sub list [user]".
func subList(conn db.DBConn, args []string) int {
	user := ""
	if len(args) >= 1 {
		user = args[0]
	}
	keys := []db.DBKey{
		{Name: "username", Type: db.DBValString},
		{Name: "domain", Type: db.DBValString},
		{Name: "email_address", Type: db.DBValString},
	}
	var where []db.DBCondition
	if user != "" {
		where = []db.DBCondition{
			{Key: "username", Op: "=", Value: db.NewStringValue(user)},
		}
	}
	res, err := conn.Query("subscriber", keys, where, "username ASC", 0, 0)
	if err != nil {
		fmt.Fprintf(stderr, "kamctl: query subscribers: %v\n", err)
		return 1
	}
	if res == nil || res.RowCount() == 0 {
		fmt.Fprintln(stdout, "(no subscribers)")
		return 0
	}
	fmt.Fprintf(stdout, "%-30s %-20s %s\n", "AOR", "DOMAIN", "EMAIL")
	for i := 0; i < res.RowCount(); i++ {
		row := res.Row(i)
		fmt.Fprintf(stdout, "%-30s %-20s %s\n",
			row.GetString("username"),
			row.GetString("domain"),
			row.GetString("email_address"))
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:]))
}
