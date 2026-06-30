package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kamailio/kamailio-go/internal/core/acc"
	"github.com/kamailio/kamailio-go/internal/core/config"
	"github.com/kamailio/kamailio-go/internal/core/dialog"
	"github.com/kamailio/kamailio-go/internal/core/htable"
	"github.com/kamailio/kamailio-go/internal/core/log"
	"github.com/kamailio/kamailio-go/internal/core/msilo"
	"github.com/kamailio/kamailio-go/internal/core/pike"
	"github.com/kamailio/kamailio-go/internal/core/proxy"
	"github.com/kamailio/kamailio-go/internal/core/registrar"
	"github.com/kamailio/kamailio-go/internal/core/rpc"
	"github.com/kamailio/kamailio-go/internal/core/script"
	"github.com/kamailio/kamailio-go/internal/modules/cfg_db"
	"github.com/kamailio/kamailio-go/internal/modules/cfg_rpc"
	"github.com/kamailio/kamailio-go/internal/modules/cfgutils"
	"github.com/kamailio/kamailio-go/internal/modules/tm"
)

// BootstrapOptions configures the bootstrap process.
type BootstrapOptions struct {
	ConfigFile      string
	LogLevel        string
	ShutdownTimeout time.Duration
	PrintConfig     bool
	RPCAddr         string
	ScriptFile      string
}

// Bootstrap holds the runtime state produced by a successful bootstrap.
type Bootstrap struct {
	Config       *config.Config
	ConfigMgr    *config.Manager
	CfgRPC       *cfg_rpc.CfgRPCModule
	CfgDB        *cfg_db.CfgDBModule
	CfgUtils     *cfgutils.CfgUtilsModule
	Server       *Engine
	ProxyCore    *proxy.ProxyCore
	HealthServer *proxy.HealthServer
	RPCServer    *rpc.Server
	Script       *script.Script

	ctx     context.Context
	cancel  context.CancelFunc
	sigChan chan os.Signal
	stopped bool
}

// NewBootstrap performs full initialization:
//  1. Loads and validates the configuration
//  2. Initializes the log subsystem
//  3. Creates the core Engine + ProxyCore
//  4. Starts listeners (UDP/TCP)
//  5. Starts the health HTTP server
func NewBootstrap(opts BootstrapOptions) (*Bootstrap, error) {
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 10 * time.Second
	}

	var (
		cfg *config.Config
		err error
	)
	if opts.ConfigFile != "" {
		cfg, err = config.Load(opts.ConfigFile)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
	} else {
		cfg = config.DefaultConfig()
	}
	if opts.LogLevel != "" {
		cfg.Core.LogLevel = opts.LogLevel
	}

	report := cfg.ValidateStrict()
	if report.HasErrors() {
		return nil, fmt.Errorf("invalid configuration: %s", report.Error())
	}
	for _, warn := range report.Warnings {
		_ = warn
	}

	if opts.PrintConfig {
		fmt.Printf("Realm: %s\nListen: %v\nLogLevel: %s\nWorkers: %d\n",
			cfg.Realm, cfg.Core.Listen, cfg.Core.LogLevel, cfg.Core.Workers)
		fmt.Printf("HealthListenAddr: %s\n", cfg.HealthListenAddr)
	}

	if err := log.Init(&log.Config{Level: cfg.Core.LogLevel, Encoding: "console"}); err != nil {
		return nil, fmt.Errorf("init log: %w", err)
	}

	// Install a config.Manager so SIGHUP / cfg.reload RPC can re-read
	// this file at runtime. Subscribers below propagate the relevant
	// fields into the live subsystems (log level, registrar bounds).
	cfgMgr := config.NewManager(cfg, opts.ConfigFile)

	// cfg_rpc exposes a flat key/value namespace over RPC for ad-hoc
	// runtime overrides. Seed it with the current values of the most
	// commonly-tweaked knobs so cfg.list/cfg.get report something useful
	// out of the box and cfg.reset has a baseline to restore.
	cfgRPC := cfg_rpc.New()
	cfgRPC.SetDefault("core.log_level", cfg.Core.LogLevel)
	cfgRPC.SetDefault("core.realm", cfg.Realm)
	cfgRPC.SetDefault("core.workers", fmt.Sprintf("%d", cfg.Core.Workers))
	cfgRPC.SetDefault("core.ims_enabled", fmt.Sprintf("%v", cfg.IMS.Enabled))

	// cfg_db is a separate persistent KV store (in this Go port it is
	// in-memory; a future DB-backed implementation would persist here).
	// cfgutils backs the $shv shared variables and $cnt named counters
	// that scripts and operators use for cross-request state.
	cfgDB := cfg_db.New()
	cfgUtils := cfgutils.NewCfgUtilsModule()

	// Hot-reload subscriber: log level — re-initialise the logger when
	// the level changes. (The registrar & cfg_rpc subscribers are
	// registered later, once the registrar has been constructed.)
	cfgMgr.Subscribe(func(_, new *config.Config) error {
		if new.Core.LogLevel != "" {
			log.SetLevel(new.Core.LogLevel)
			log.Info("log level reloaded", log.String("level", new.Core.LogLevel))
		}
		return nil
	})

	engine := NewEngine(cfg)
	if err := engine.Start(); err != nil {
		log.Sync()
		return nil, fmt.Errorf("start engine: %w", err)
	}

	pcoreCfg := &proxy.ProxyConfig{
		Realm:               cfg.Realm,
		AuthRequired:        false,
		NATDetectionEnabled: false,
		MediaProxyEnabled:   cfg.EnableMediaProxy,
		PresenceEnabled:     false,
		RecordRouteEnabled:  false,
	}
	if cfg.AuthEnabled {
		pcoreCfg.AuthRequired = true
	}
	if cfg.NATEnabled {
		pcoreCfg.NATDetectionEnabled = true
	}
	if cfg.PresenceEnabled {
		pcoreCfg.PresenceEnabled = true
	}
	pcore := proxy.NewProxyCore(pcoreCfg)

	pk := pike.New(20, 5*time.Second)
	pcore.SetPike(pk)
	hm := htable.NewManager()
	pcore.SetHTables(hm)
	ms := msilo.New(nil, "msilo")
	pcore.SetMsilo(ms)
	dm := dialog.NewManager()
	pcore.SetDialogs(dm)
	ac := acc.NewAccountingService()
	pcore.SetAccounting(ac)

	// Phase 44: wire registrar and transaction manager into ProxyCore.
	tmMgr := tm.NewManager(1024)
	pcore.SetTM(tmMgr)
	reg := registrar.New(&registrar.Config{
		Realm:          cfg.Realm,
		DefaultExpires: 3600 * time.Second,
		MaxExpires:     86400 * time.Second,
		MinExpires:     60 * time.Second,
	})
	pcore.SetRegistrar(reg)

	// Hot-reload subscribers that need the registrar handle.
	// 1. Registrar — refresh expires bounds & realm. Existing bindings
	//    are preserved; only the policy values are swapped in.
	cfgMgr.Subscribe(func(_, new *config.Config) error {
		reg.SetConfig(&registrar.Config{
			Realm:          new.Realm,
			DefaultExpires: 3600 * time.Second,
			MaxExpires:     86400 * time.Second,
			MinExpires:     60 * time.Second,
		})
		return nil
	})
	// 2. cfg_rpc defaults — keep the advertised snapshot in sync so a
	//    post-reload cfg.list reflects the new file values.
	cfgMgr.Subscribe(func(_, new *config.Config) error {
		cfgRPC.SetDefault("core.log_level", new.Core.LogLevel)
		cfgRPC.SetDefault("core.realm", new.Realm)
		cfgRPC.SetDefault("core.workers", fmt.Sprintf("%d", new.Core.Workers))
		cfgRPC.SetDefault("core.ims_enabled", fmt.Sprintf("%v", new.IMS.Enabled))
		return nil
	})

	var sc *script.Script
	if opts.ScriptFile != "" {
		content, err := os.ReadFile(opts.ScriptFile)
		if err != nil {
			log.Sync()
			return nil, fmt.Errorf("read script file: %w", err)
		}
		sc, err = script.Parse(string(content))
		if err != nil {
			log.Sync()
			return nil, fmt.Errorf("parse script: %w", err)
		}
		pcore.SetScript(sc)
	}

	started, err := engine.StartListeners(pcore)
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("start listeners: %w", err)
	}
	if started == 0 {
		log.Warn("No SIP listeners were started — check Listen config")
	} else {
		log.Info("SIP listeners started", log.Int("count", started))
	}

	hs := proxy.NewHealthServer(pcoreCfg, pcore)
	addr := "127.0.0.1:0"
	if cfg.HealthListenAddr != "" {
		addr = cfg.HealthListenAddr
	}
	go hs.ListenAndServe(addr)

	time.Sleep(20 * time.Millisecond)
	if hsAddr := hs.Addr(); hsAddr != "" {
		log.Info("Health server listening", log.String("addr", "http://"+hsAddr+"/status"))
	}

	var rpcServer *rpc.Server
	if opts.RPCAddr != "" {
		rpcServer = rpc.NewExtended(rpc.ServerConfig{
			Core:          pcore,
			Dialogs:       dm,
			Pike:          pk,
			HTables:       hm,
			Msilo:         ms,
			Acc:           ac,
			Usrloc:        reg,
			CfgRPC:        cfgRPC,
			CfgDB:         cfgDB,
			CfgUtils:      cfgUtils,
			ConfigManager: cfgMgr,
		})
		go func() {
			_ = rpcServer.ListenAndServe(opts.RPCAddr)
		}()
		time.Sleep(20 * time.Millisecond)
		if rpcAddr := rpcServer.Addr(); rpcAddr != "" {
			log.Info("JSON-RPC server listening", log.String("addr", "http://"+rpcAddr+"/rpc"))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	b := &Bootstrap{
		Config:       cfg,
		ConfigMgr:    cfgMgr,
		CfgRPC:       cfgRPC,
		CfgDB:        cfgDB,
		CfgUtils:     cfgUtils,
		Server:       engine,
		ProxyCore:    pcore,
		HealthServer: hs,
		RPCServer:    rpcServer,
		Script:       sc,
		ctx:          ctx,
		cancel:       cancel,
	}
	return b, nil
}

// WaitForSignal blocks until SIGINT or SIGTERM is received. SIGHUP is
// intercepted separately and triggers a configuration reload via the
// ConfigManager (if one was installed); the call then returns to the
// signal loop so the process keeps running.
func (b *Bootstrap) WaitForSignal() {
	if b.sigChan == nil {
		b.sigChan = make(chan os.Signal, 1)
		signal.Notify(b.sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	}
	for {
		sig := <-b.sigChan
		if sig == syscall.SIGHUP {
			b.handleReload()
			continue
		}
		log.Info("Signal received", log.String("signal", sig.String()))
		return
	}
}

// handleReload invokes the ConfigManager reload and logs the outcome.
// When no manager is wired up the call is a no-op. Errors are logged
// but never fatal — the previous configuration remains in effect.
func (b *Bootstrap) handleReload() {
	if b.ConfigMgr == nil {
		log.Warn("SIGHUP received but no config manager installed — ignoring")
		return
	}
	log.Info("SIGHUP received — reloading configuration", log.String("path", b.ConfigMgr.Path()))
	newCfg, err := b.ConfigMgr.Reload()
	if err != nil {
		log.Error("Configuration reload failed", log.ErrField(err))
		return
	}
	log.Info("Configuration reloaded",
		log.String("realm", newCfg.Realm),
		log.String("log_level", newCfg.Core.LogLevel),
		log.Int("workers", newCfg.Core.Workers))
}

// Shutdown cleanly stops the server: proxy core → SIP listeners →
// health server → logging.
func (b *Bootstrap) Shutdown() {
	if b.stopped {
		return
	}
	b.stopped = true
	b.cancel()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	if b.HealthServer != nil {
		if err := b.HealthServer.Shutdown(stopCtx); err != nil {
			log.Warn("Health server shutdown error", log.ErrField(err))
		}
	}
	if b.RPCServer != nil {
		if err := b.RPCServer.Shutdown(); err != nil {
			log.Warn("RPC server shutdown error", log.ErrField(err))
		}
	}
	if b.Server != nil {
		b.Server.Stop()
	}
	if b.ProxyCore != nil {
		if err := b.ProxyCore.Shutdown(stopCtx); err != nil {
			log.Warn("Proxy core shutdown error", log.ErrField(err))
		}
	}
	log.Info("Kamailio-Go stopped")
	log.Sync()
}
