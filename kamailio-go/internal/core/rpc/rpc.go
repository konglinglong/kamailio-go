// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * JSON-RPC 2.0 over HTTP — operator endpoint.
 *
 * Exposes status / pike clear / msilo queued / htable set-get methods.
 * The endpoint is intentionally lightweight: no authentication, no
 * keep-alive, small body limits. It is meant to sit behind a reverse
 * proxy in production deployments.
 */

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/acc"
	"github.com/kamailio/kamailio-go/internal/core/config"
	"github.com/kamailio/kamailio-go/internal/core/dialog"
	"github.com/kamailio/kamailio-go/internal/core/htable"
	"github.com/kamailio/kamailio-go/internal/core/msilo"
	"github.com/kamailio/kamailio-go/internal/core/pike"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
	"github.com/kamailio/kamailio-go/internal/core/registrar"
	"github.com/kamailio/kamailio-go/internal/core/script"
	"github.com/kamailio/kamailio-go/internal/core/usrloc"
	"github.com/kamailio/kamailio-go/internal/modules/cfg_db"
	"github.com/kamailio/kamailio-go/internal/modules/cfg_rpc"
	"github.com/kamailio/kamailio-go/internal/modules/cfgutils"
)

// ServerConfig captures the subsystem dependencies for a Server. Any
// field may be nil — the corresponding RPC methods will return a
// JSON-RPC error instead of panicking.
type ServerConfig struct {
	Core          *proxy.ProxyCore
	Dialogs       *dialog.Manager
	Pike          *pike.Pike
	HTables       *htable.Manager
	Msilo         *msilo.Msilo
	Acc           *acc.AccountingService
	Usrloc        *registrar.Registrar
	CfgRPC        *cfg_rpc.CfgRPCModule
	CfgDB         *cfg_db.CfgDBModule
	CfgUtils      *cfgutils.CfgUtilsModule
	ConfigManager *config.Manager
}

// Server exposes a JSON-RPC 2.0 HTTP endpoint. Handlers are registered
// per method on an internal map. The zero value is not usable — call
// New() to wire subsystems.
type Server struct {
	mu         sync.RWMutex
	core       *proxy.ProxyCore
	dialogs    *dialog.Manager
	acc        *acc.AccountingService
	pike       *pike.Pike
	htables    *htable.Manager
	msilo      *msilo.Msilo
	usrloc     *registrar.Registrar
	cfgRPC     *cfg_rpc.CfgRPCModule
	cfgDB      *cfg_db.CfgDBModule
	cfgUtils   *cfgutils.CfgUtilsModule
	cfgMgr     *config.Manager
	httpServer *http.Server
	listener   atomic.Value
	handler    *http.ServeMux
	started    bool
}

// New constructs a Server wired to existing proxy subsystems. Kept for
// backwards compatibility — consider NewExtended() or passing a
// ServerConfig for access to dialog/accounting subsystems.
func New(p *proxy.ProxyCore, pk *pike.Pike, hm *htable.Manager, ms *msilo.Msilo) *Server {
	return NewExtended(ServerConfig{Core: p, Pike: pk, HTables: hm, Msilo: ms})
}

// NewExtended constructs a Server from a full ServerConfig. Use this
// constructor when dialog listing or accounting-aware RPC methods are
// needed.
func NewExtended(cfg ServerConfig) *Server {
	s := &Server{
		core:    cfg.Core,
		dialogs: cfg.Dialogs,
		acc:     cfg.Acc,
		pike:    cfg.Pike,
		htables: cfg.HTables,
		msilo:   cfg.Msilo,
		usrloc:  cfg.Usrloc,
		cfgRPC:  cfg.CfgRPC,
		cfgDB:   cfg.CfgDB,
		cfgUtils: cfg.CfgUtils,
		cfgMgr:  cfg.ConfigManager,
		handler: http.NewServeMux(),
	}
	s.handler.HandleFunc("/rpc", s.handleRPC)
	s.handler.HandleFunc("/healthz", s.handleHealthz)
	s.handler.HandleFunc("/status", s.handleStatus)
	return s
}

// ListenAndServe starts the HTTP server on addr. It blocks until the
// server stops, either via Shutdown or a fatal transport error. When
// binding to port "0" the kernel allocates a free port, which can be
// read via ListenerAddr().
func (s *Server) ListenAndServe(addr string) error {
	if s == nil {
		return fmt.Errorf("nil rpc server")
	}
	if addr == "" {
		return fmt.Errorf("empty rpc address")
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener.Store(ln)
	s.mu.Lock()
	s.httpServer = &http.Server{
		Addr:         ln.Addr().String(),
		Handler:      s.handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	s.started = true
	s.mu.Unlock()
	return s.httpServer.Serve(ln)
}

// Shutdown gracefully closes the HTTP listener. Subsequent calls are
// safe no-ops.
func (s *Server) Shutdown() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	s.started = false
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

// Addr returns the server's listening address (useful in tests when
// binding to port 0). Empty if the server has never been started.
func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.httpServer == nil {
		return ""
	}
	return s.httpServer.Addr
}

// ListenerAddr returns the host:port after a successful listen (useful
// when binding to port 0 so the caller can discover the actual port).
func (s *Server) ListenerAddr() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.httpServer == nil {
		return ""
	}
	if ln, ok := s.listener.Load().(net.Listener); ok && ln != nil {
		return ln.Addr().String()
	}
	return s.httpServer.Addr
}

// -------------------------------------------------------------
// HTTP handlers

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{"ok": true}
	if s.pike != nil {
		ips := s.pike.ActiveIPs()
		resp["pike"] = map[string]interface{}{
			"active_ips": ips[:minInt(len(ips), 10)],
			"count":      len(ips),
		}
	}
	if s.htables != nil {
		tables := s.htables.TableNames()
		resp["htables"] = map[string]interface{}{
			"table_count": len(tables),
			"tables":      tables,
		}
	}
	if s.msilo != nil {
		resp["msilo"] = map[string]interface{}{"total_queued": s.msilo.TotalQueued()}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// JSONRPCRequest represents an incoming JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params,omitempty"`
	ID      interface{}   `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// RPCError is the JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface so RPCError values can be
// returned directly from Client.Call.
func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Standard JSON-RPC 2.0 error codes.
const (
	ErrParse     = -32700
	ErrInvalid   = -32600
	ErrMethod    = -32601
	ErrParams    = -32602
	ErrInternal  = -32603
	ErrDisabled  = -32001
	ErrMsilo     = -32002
	ErrHTables   = -32003
)

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, req.ID, ErrParse, "parse error")
		return
	}
	result, mErr := s.dispatch(req.Method, req.Params)
	if mErr != nil {
		writeRPCError(w, req.ID, mErr.Code, mErr.Message)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", Result: result, ID: req.ID})
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	})
}

type methodErr struct {
	Code    int
	Message string
}

func (e *methodErr) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

// dispatch routes a JSON-RPC method to the right subsystem.
func (s *Server) dispatch(method string, params []interface{}) (interface{}, *methodErr) {
	switch method {
	case "kamailio.ping":
		return map[string]interface{}{
			"pong": true,
			"at":   time.Now().Format(time.RFC3339),
		}, nil

	case "kamailio.stats":
		if s.core == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "proxy core not wired up"}
		}
		return map[string]interface{}{
			"ok":      true,
			"metrics": s.core.MetricsSnapshot(),
		}, nil

	case "kamailio.dialog.list":
		if s.dialogs == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "dialog manager not wired up"}
		}
		limit := 0
		if len(params) >= 1 {
			switch v := params[0].(type) {
			case float64:
				limit = int(v)
			case int:
				limit = v
			}
		}
		list := s.dialogs.List(limit)
		out := make([]map[string]interface{}, 0, len(list))
		for _, d := range list {
			entry := map[string]interface{}{
				"call_id":       d.CallID,
				"from":          d.RemoteURI,
				"to":            d.LocalURI,
				"state":         d.State.String(),
				"duration_ms":   int64(time.Since(d.CreatedAt).Milliseconds()),
				"direction":     d.Direction.String(),
				"local_tag":     d.LocalTag,
				"remote_tag":    d.RemoteTag,
				"remote_target": d.RemoteTarget,
			}
			out = append(out, entry)
		}
		return map[string]interface{}{
			"ok":     true,
			"count":  len(out),
			"dialogs": out,
		}, nil

	case "kamailio.script.reload":
		if s.core == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "proxy core not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [script_text]"}
		}
		text, ok := params[0].(string)
		if !ok {
			return nil, &methodErr{Code: ErrParams, Message: "script_text must be a string"}
		}
		parsed, err := script.Parse(text)
		if err != nil {
			return map[string]interface{}{
				"ok":    false,
				"error": err.Error(),
			}, nil
		}
		s.core.SetScript(parsed)
		return map[string]interface{}{
			"ok":    true,
			"error": "",
		}, nil

	case "kamailio.shutdown":
		if s.core != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.core.Shutdown(ctx)
			cancel()
		}
		return map[string]interface{}{"ok": true}, nil

	case "kamailio.pike.status":
		if s.pike == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "pike disabled"}
		}
		ips := s.pike.ActiveIPs()
		return map[string]interface{}{
			"active_ips": ips,
			"count":      len(ips),
		}, nil

	case "kamailio.pike.clear":
		if s.pike == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "pike disabled"}
		}
		if len(params) == 1 {
			if ip, ok := params[0].(string); ok {
				s.pike.Clear(ip)
				return map[string]interface{}{"cleared": ip}, nil
			}
		}
		// No arg — clear all.
		for _, ip := range s.pike.ActiveIPs() {
			s.pike.Clear(ip)
		}
		return map[string]interface{}{"cleared": "all"}, nil

	case "kamailio.msilo.queued":
		if s.msilo == nil {
			return nil, &methodErr{Code: ErrMsilo, Message: "msilo disabled"}
		}
		if len(params) == 1 {
			if user, ok := params[0].(string); ok {
				return map[string]interface{}{
					"user":   user,
					"queued": s.msilo.QueueLength(user),
				}, nil
			}
		}
		return map[string]interface{}{"total": s.msilo.TotalQueued()}, nil

	case "kamailio.htable.list":
		if s.htables == nil {
			return nil, &methodErr{Code: ErrHTables, Message: "htables manager disabled"}
		}
		return map[string]interface{}{"tables": s.htables.TableNames()}, nil

	case "kamailio.htable.set":
		if s.htables == nil {
			return nil, &methodErr{Code: ErrHTables, Message: "htables manager disabled"}
		}
		if len(params) < 3 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [table, key, value]"}
		}
		tableName, _ := params[0].(string)
		key, _ := params[1].(string)
		val := fmt.Sprintf("%v", params[2])
		tbl := s.htables.Get(tableName)
		if tbl == nil {
			tbl = s.htables.Create(tableName, 0)
		}
		tbl.Set(key, val, 0)
		return map[string]interface{}{"table": tableName, "key": key, "value": val}, nil

	case "kamailio.htable.get":
		if s.htables == nil {
			return nil, &methodErr{Code: ErrHTables, Message: "htables manager disabled"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [table, key]"}
		}
		tableName, _ := params[0].(string)
		key, _ := params[1].(string)
		tbl := s.htables.Get(tableName)
		if tbl == nil {
			return map[string]interface{}{"found": false}, nil
		}
		v, ok := tbl.Get(key)
		return map[string]interface{}{"found": ok, "value": v}, nil

	case "kamailio.status":
		out := map[string]interface{}{"ok": true}
		if s.pike != nil {
			out["pike_active"] = len(s.pike.ActiveIPs())
		}
		if s.msilo != nil {
			out["msilo_total"] = s.msilo.TotalQueued()
		}
		if s.htables != nil {
			out["htables"] = s.htables.TableNames()
		}
		if s.core != nil {
			out["metrics"] = s.core.MetricsSnapshot()
		}
		return out, nil

	case "kamailio.cfg.get":
		// Mirrors C's cfg.get(group_name, var_name). The Kamailio-Go
		// cfg_rpc module exposes a single flat key namespace, so the
		// call collapses to cfg.get(key). Returns the current value
		// (or the registered default) and a not-found flag.
		if s.cfgRPC == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_rpc module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [key]"}
		}
		key, _ := params[0].(string)
		val, err := s.cfgRPC.Get(key)
		if err != nil {
			return map[string]interface{}{"found": false, "key": key}, nil
		}
		return map[string]interface{}{"found": true, "key": key, "value": val}, nil

	case "kamailio.cfg.set":
		// Mirrors C's cfg.sets/cfg.seti. Values are stored as strings;
		// numeric values are stringified by fmt.
		if s.cfgRPC == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_rpc module not wired up"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [key, value]"}
		}
		key, _ := params[0].(string)
		val := fmt.Sprintf("%v", params[1])
		if err := s.cfgRPC.Set(key, val); err != nil {
			return nil, &methodErr{Code: ErrInternal, Message: err.Error()}
		}
		return map[string]interface{}{"ok": true, "key": key, "value": val}, nil

	case "kamailio.cfg.list":
		// Mirrors C's cfg.list. Returns every known key (defaults
		// overlaid with current overrides).
		if s.cfgRPC == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_rpc module not wired up"}
		}
		all := s.cfgRPC.List()
		out := make([]map[string]interface{}, 0, len(all))
		for k, v := range all {
			out = append(out, map[string]interface{}{"key": k, "value": v})
		}
		return map[string]interface{}{"ok": true, "count": len(out), "entries": out}, nil

	case "kamailio.cfg.reset":
		// Mirrors C's cfg.reset — restores the registered default for
		// the named key, removing any runtime override.
		if s.cfgRPC == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_rpc module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [key]"}
		}
		key, _ := params[0].(string)
		if err := s.cfgRPC.Reset(key); err != nil {
			return nil, &methodErr{Code: ErrInternal, Message: err.Error()}
		}
		return map[string]interface{}{"ok": true, "key": key}, nil

	case "kamailio.cfg.reload":
		// Mirrors C's cfg.reload (often issued via SIGHUP). Re-reads
		// the source config file, validates it, swaps the live
		// configuration and notifies subscribers.
		if s.cfgMgr == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "config manager not wired up"}
		}
		newCfg, err := s.cfgMgr.Reload()
		if err != nil {
			return map[string]interface{}{"ok": false, "error": err.Error()}, nil
		}
		return map[string]interface{}{
			"ok":      true,
			"path":    s.cfgMgr.Path(),
			"realm":   newCfg.Realm,
			"workers": newCfg.Core.Workers,
			"level":   newCfg.Core.LogLevel,
		}, nil

	case "kamailio.cfg.snapshot":
		// Returns a JSON snapshot of the currently-installed
		// configuration. Useful for operators verifying the effect of a
		// reload without re-reading the file.
		if s.cfgMgr == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "config manager not wired up"}
		}
		c := s.cfgMgr.Get()
		if c == nil {
			return map[string]interface{}{"ok": false, "error": "no config installed"}, nil
		}
		return map[string]interface{}{
			"ok":       true,
			"realm":    c.Realm,
			"log_level": c.Core.LogLevel,
			"workers":  c.Core.Workers,
			"listen":   c.Core.Listen,
			"ims":      c.IMS.Enabled,
			"path":     s.cfgMgr.Path(),
		}, nil

	case "kamailio.cfgdb.load":
		// cfg_db: load a single key. Returns {found:false} when the
		// key is absent.
		if s.cfgDB == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_db module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [key]"}
		}
		key, _ := params[0].(string)
		val, err := s.cfgDB.Load(key)
		if err != nil {
			return map[string]interface{}{"found": false, "key": key}, nil
		}
		return map[string]interface{}{"found": true, "key": key, "value": val}, nil

	case "kamailio.cfgdb.store":
		// cfg_db: store (create or overwrite) a key/value pair.
		if s.cfgDB == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_db module not wired up"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [key, value]"}
		}
		key, _ := params[0].(string)
		val := fmt.Sprintf("%v", params[1])
		if err := s.cfgDB.Store(key, val); err != nil {
			return nil, &methodErr{Code: ErrInternal, Message: err.Error()}
		}
		return map[string]interface{}{"ok": true, "key": key, "value": val}, nil

	case "kamailio.cfgdb.delete":
		// cfg_db: delete a key. Returns {ok:false} when absent.
		if s.cfgDB == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_db module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [key]"}
		}
		key, _ := params[0].(string)
		if err := s.cfgDB.Delete(key); err != nil {
			return map[string]interface{}{"ok": false, "key": key}, nil
		}
		return map[string]interface{}{"ok": true, "key": key}, nil

	case "kamailio.cfgdb.list":
		// cfg_db: list all stored key/value pairs.
		if s.cfgDB == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfg_db module not wired up"}
		}
		all := s.cfgDB.List()
		out := make([]map[string]interface{}, 0, len(all))
		for k, v := range all {
			out = append(out, map[string]interface{}{"key": k, "value": v})
		}
		return map[string]interface{}{"ok": true, "count": len(out), "entries": out}, nil

	case "kamailio.shv.get":
		// cfgutils $shv: read a shared string variable.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [name]"}
		}
		name, _ := params[0].(string)
		return map[string]interface{}{
			"name":    name,
			"exists":  s.cfgUtils.VarExists(name),
			"value":   s.cfgUtils.GetVar(name),
		}, nil

	case "kamailio.shv.set":
		// cfgutils $shv: assign a shared string variable.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [name, value]"}
		}
		name, _ := params[0].(string)
		val := fmt.Sprintf("%v", params[1])
		s.cfgUtils.SetVar(name, val)
		return map[string]interface{}{"ok": true, "name": name, "value": val}, nil

	case "kamailio.shv.list":
		// cfgutils $shv: list all shared variables. The module does
		// not expose a direct List method, so we surface existence +
		// value for each name passed in. With no params, returns the
		// count of registered variables is not tracked separately; we
		// return an empty list when no names are queried.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		queried := make([]map[string]interface{}, 0, len(params))
		for _, p := range params {
			name, ok := p.(string)
			if !ok {
				continue
			}
			queried = append(queried, map[string]interface{}{
				"name":   name,
				"exists": s.cfgUtils.VarExists(name),
				"value":  s.cfgUtils.GetVar(name),
			})
		}
		return map[string]interface{}{"ok": true, "count": len(queried), "entries": queried}, nil

	case "kamailio.cnt.get":
		// cfgutils $cnt: read a named counter.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [name]"}
		}
		name, _ := params[0].(string)
		return map[string]interface{}{"name": name, "value": s.cfgUtils.GetCount(name)}, nil

	case "kamailio.cnt.set":
		// cfgutils $cnt: assign a named counter.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [name, value]"}
		}
		name, _ := params[0].(string)
		var val int64
		switch v := params[1].(type) {
		case float64:
			val = int64(v)
		case int:
			val = int64(v)
		case int64:
			val = v
		case string:
			fmt.Sscanf(v, "%d", &val)
		default:
			fmt.Sscanf(fmt.Sprintf("%v", v), "%d", &val)
		}
		s.cfgUtils.SetCount(name, val)
		return map[string]interface{}{"ok": true, "name": name, "value": val}, nil

	case "kamailio.cnt.inc":
		// cfgutils $cnt: atomically add delta (default +1) to a counter.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [name, delta?]"}
		}
		name, _ := params[0].(string)
		delta := int64(1)
		if len(params) >= 2 {
			switch v := params[1].(type) {
			case float64:
				delta = int64(v)
			case int:
				delta = int64(v)
			case string:
				fmt.Sscanf(v, "%d", &delta)
			}
		}
		newVal := s.cfgUtils.IncCount(name, delta)
		return map[string]interface{}{"ok": true, "name": name, "value": newVal}, nil

	case "kamailio.cnt.reset":
		// cfgutils $cnt: reset a named counter to 0.
		if s.cfgUtils == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "cfgutils module not wired up"}
		}
		if len(params) < 1 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [name]"}
		}
		name, _ := params[0].(string)
		s.cfgUtils.ResetCount(name)
		return map[string]interface{}{"ok": true, "name": name, "value": int64(0)}, nil

	case "kamailio.ul.dump":
		// Mirrors C's "ul.dump" operator command. With no params,
		// dumps every domain; with one param, dumps only that domain.
		if s.usrloc == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "usrloc/registrar not wired up"}
		}
		domainFilter := ""
		if len(params) >= 1 {
			if v, ok := params[0].(string); ok {
				domainFilter = v
			}
		}
		domains := []interface{}{}
		_ = s.usrloc.ForEachDomain(func(domName string, d *usrloc.Domain) error {
			if domainFilter != "" && domName != domainFilter {
				return nil
			}
			aors := []interface{}{}
			for _, aorKey := range d.AORs() {
				aor := d.GetAOR(aorKey)
				if aor == nil {
					continue
				}
				contacts := []interface{}{}
				for _, c := range aor.ActiveContacts() {
					contacts = append(contacts, map[string]interface{}{
						"uri":     c.URI,
						"q":       c.Q,
						"expires": c.Expires.Format(time.RFC3339),
						"path":    c.Path,
					})
				}
				aors = append(aors, map[string]interface{}{
					"aor":      aorKey,
					"contacts": contacts,
				})
			}
			domains = append(domains, map[string]interface{}{
				"name": domName,
				"aors": aors,
			})
			return nil
		})
		return map[string]interface{}{
			"ok":      true,
			"domains": domains,
		}, nil

	case "kamailio.ul.lookup":
		// Mirrors C's "ul.lookup <domain> <aor>". Returns the AOR's
		// active contacts or {"found": false} when the AOR is unknown.
		if s.usrloc == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "usrloc/registrar not wired up"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [domain, aor]"}
		}
		domName, _ := params[0].(string)
		aorKey, _ := params[1].(string)
		aor := s.usrloc.LookupAOR(domName, aorKey)
		if aor == nil {
			return map[string]interface{}{"found": false}, nil
		}
		contacts := []interface{}{}
		for _, c := range aor.ActiveContacts() {
			contacts = append(contacts, map[string]interface{}{
				"uri":     c.URI,
				"q":       c.Q,
				"expires": c.Expires.Format(time.RFC3339),
				"path":    c.Path,
			})
		}
		return map[string]interface{}{
			"found":    true,
			"domain":   domName,
			"aor":      aorKey,
			"contacts": contacts,
		}, nil

	case "kamailio.ul.rm":
		// Mirrors C's "ul.rm <domain> <aor>". Removes the entire AOR
		// (all its contacts) from the named domain.
		if s.usrloc == nil {
			return nil, &methodErr{Code: ErrDisabled, Message: "usrloc/registrar not wired up"}
		}
		if len(params) < 2 {
			return nil, &methodErr{Code: ErrParams, Message: "usage: [domain, aor]"}
		}
		domName, _ := params[0].(string)
		aorKey, _ := params[1].(string)
		ok := s.usrloc.RemoveAOR(domName, aorKey)
		return map[string]interface{}{
			"ok":      ok,
			"domain":  domName,
			"aor":     aorKey,
			"removed": ok,
		}, nil
	}
	return nil, &methodErr{Code: ErrMethod, Message: fmt.Sprintf("method not found: %s", method)}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
