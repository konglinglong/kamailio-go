# IMS 分角色部署设计

- 日期: 2026-06-25
- 状态: 已批准（设计阶段），待实现规划
- 关联代码: `kamailio-go/internal/ims/{pcscf,scscf,icscf}`, `kamailio-go/internal/core/{app,proxy,config}`

## 1. 背景与目标

### 1.1 现状

`kamailio-go` 当前只有一个 SIP 服务器二进制 `cmd/kamailio`，通过 `app.NewBootstrap` 装配通用 pipeline（pike/htable/dialog/acc/tm/registrar/script）。`internal/ims/{pcscf,scscf,icscf}` 三个业务层包虽然存在且互相无 import 依赖，但**未接入** live bootstrap：

- `app.NewBootstrap` 不导入也不实例化任何 IMS 包。
- `IMSConfig.SCSCF/PCSCF/ICSCF` 三个布尔角色开关是死代码（从未被读取）。
- `proxy.ProxyCore` 无 IMS 类型的字段或 setter。
- 跨 CSCF 转发根本未实现：`icscf.dispatch` 是 no-op stub；`scscf.HandleRegister` 把 MAR/SAR 短路为本地 `auth.GenerateAuthVector()`；`pcscf` 从不向 icscf 发任何消息。

三个 IMS 包之间已互相解耦，且 `icscf.SCSCFTable` 把 S-CSCF 建模为**名称字符串**（network-friendly），这对拆分部署有利。

### 1.2 目标

支持将 P-CSCF / I-CSCF / S-CSCF 作为**独立进程**部署，通过标准 SIP 互联，构成完整的 IMS 注册与会话路由链路。

### 1.3 非目标

- 不重写 `internal/ims/{pcscf,scscf,icscf}` 现有业务逻辑与 API。
- 不动 `internal/modules/ims_*` 旧模块（仅作历史保留）。
- 不实现 IPSec 安全策略（`ims_ipsec_pcscf` 占位）。
- 不优化同进程三跳的性能（仅作部署/测试便利）。

## 2. 部署模型

**单二进制 + 角色标志**：单一 `kamailio-go` 二进制，通过 `--role pcscf|scscf|icscf|all`（或 config 中 `ims.role`）选择启动哪些 CSCF 子系统。

- `--role all`（默认，向后兼容）：三者同进程。
- `--role pcscf|scscf|icscf`：仅启动该角色。

不同实例通过网络 SIP 互联。`--role all` 与单角色模式走**同一条**网络化转发路径，只是 `next_hop` 不同（all 模式下指向 localhost 端口）。一套代码、两种部署、一致的测试覆盖。

## 3. 架构总览

```
┌─────────────────────────────────────────────────────────┐
│  cmd/kamailio  →  app.NewBootstrap(role, cfg)           │
│                         │                               │
│         ┌───────────────┼───────────────┐               │
│         ▼               ▼               ▼               │
│   pcscf.Adaptor   icscf.Adaptor   scscf.Adaptor         │
│  (CSCF 适配层 — 方案 A 的核心)                          │
│         │               │               │               │
│         ▼               ▼               ▼               │
│   pcscf 业务       icscf 业务       scscf 业务          │
│  (HandleInvite   (SendUAR/LIR    (HandleRegister        │
│   +route lookup)  +SCSCFTable)    +AKA+session)         │
│         │               │                               │
│         ▼               ▼                               │
│   forwarder ──SIP──▶ forwarder ──SIP──▶ forwarder       │
│   (pcscf 进程)    (icscf 进程)    (scscf 进程)          │
└─────────────────────────────────────────────────────────┘
```

**关键原则**：

- 适配层是"业务 ↔ 网络的桥"：调用 IMS handler 拿到结构化结果（如 `*RegisterResult`），然后**要么**（终接角色）把它翻译成 SIP 响应回送，**要么**（中转角色）用 `forward.Forwarder` 把请求 SIP 转发到 `next_hop`。
- 三个 IMS 业务包保持现有 API 与单元测试不动；新增的全部在适配层与 bootstrap。
- 复用现有基础设施：`ProxyCore`、`tm.Manager`、`forward.Forwarder`、`usrloc.Registrar`、`cdp.TransactionManager`。不重写这些。

## 4. 配置 Schema

### 4.1 YAML 结构

```yaml
ims:
  enabled: true
  role: pcscf           # pcscf | scscf | icscf | all (默认 all)
  realm: "home.net"     # 共享默认值，角色节可覆盖

  # --- P-CSCF 节 ---
  pcscf:
    listen:             # 该角色的 SIP 监听（覆盖 core.listen）
      - "udp:5060"
      - "tcp:5060"
    realm: "home.net"   # 可选，覆盖 ims.realm
    visited_network_id: "visited.home.net"
    # 转发到 I-CSCF（注册/初始请求）和 S-CSCF（已选路由后）
    icscf_addr: "sip:icscf.home.net:5060"
    scscf_addr: "sip:scscf.home.net:5060"   # 仅当已知时
    ipsec:
      enabled: false

  # --- I-CSCF 节 ---
  icscf:
    listen:
      - "udp:5060"
    realm: "home.net"
    # HSS Diameter 对端
    diameter_peers:
      - host: "hss.home.net"
        ip: "10.0.0.5"
        port: 3868
    forced_peer: ""        # 可选，C 源 cxdx_forced_peer
    # 选定 S-CSCF 后转发到这里
    scscf_addr: "sip:scscf.home.net:5060"
    # S-CSCF 能力目录
    scscf_capabilities:
      - id: 1
        name: "sip:scscf1.home.net"
        mandatory_caps: [1]
        optional_caps: [2, 3]
    entry_expiry: 300       # 候选列表存活秒数
    preferred_scscf: []      # 优先 S-CSCF 名单

  # --- S-CSCF 节 ---
  scscf:
    listen:
      - "udp:5060"
    realm: "home.net"
    # HSS Diameter 对端（MAR/SAR）
    diameter_peers:
      - host: "hss.home.net"
        ip: "10.0.0.5"
        port: 3868
    aka_algorithm: "AKAv1-MD5"
    default_expires: 3600
    min_expires: 60
    max_expires: 86400

  # --- 共享 AKA 配置 ---
  aka:
    algorithm: "AKAv1-MD5"
```

### 4.2 Go 类型

```go
type IMSConfig struct {
    Enabled bool   `yaml:"enabled"`
    Role    string `yaml:"role"`      // pcscf|scscf|icscf|all，默认 all
    Realm   string `yaml:"realm"`

    PCSCF *PCSCFConfig `yaml:"pcscf,omitempty"`
    ICSCF *ICSCFConfig `yaml:"icscf,omitempty"`
    SCSCF *SCSCFConfig `yaml:"scscf,omitempty"`

    // 向后兼容字段（旧配置仍可工作）
    SCSCF_ bool `yaml:"scscf"`   // 旧布尔开关，等价 role 含 scscf
    PCSCF_ bool `yaml:"pcscf"`
    ICSCF_ bool `yaml:"icscf"`
    // 旧平铺字段映射到各角色节默认值
    AKAAlgorithm      string `yaml:"aka_algorithm"`
    DefaultExpires    int    `yaml:"default_expires"`
    VisitedNetworkID  string `yaml:"visited_network_id"`
}

type PCSCFConfig struct {
    Listen             []string    `yaml:"listen,omitempty"`
    Realm              string      `yaml:"realm,omitempty"`
    VisitedNetworkID   string      `yaml:"visited_network_id,omitempty"`
    ICSCFAddr          string      `yaml:"icscf_addr"`
    SCSCFAddr          string      `yaml:"scscf_addr,omitempty"`
    IPSEC              IPSECConfig `yaml:"ipsec,omitempty"`
}

type ICSCFConfig struct {
    Listen            []string              `yaml:"listen,omitempty"`
    Realm             string                `yaml:"realm,omitempty"`
    DiameterPeers     []DiameterPeerConfig  `yaml:"diameter_peers,omitempty"`
    ForcedPeer        string                `yaml:"forced_peer,omitempty"`
    SCSCFAddr         string                `yaml:"scscf_addr"`
    SCSCFCapabilities []SCSCFCapConfig      `yaml:"scscf_capabilities,omitempty"`
    EntryExpiry       int                   `yaml:"entry_expiry,omitempty"`
    PreferredSCSCF    []string              `yaml:"preferred_scscf,omitempty"`
}

type SCSCFConfig struct {
    Listen          []string              `yaml:"listen,omitempty"`
    Realm           string                `yaml:"realm,omitempty"`
    DiameterPeers   []DiameterPeerConfig  `yaml:"diameter_peers,omitempty"`
    AKAAlgorithm    string                `yaml:"aka_algorithm,omitempty"`
    DefaultExpires  int                   `yaml:"default_expires,omitempty"`
    MinExpires      int                   `yaml:"min_expires,omitempty"`
    MaxExpires      int                   `yaml:"max_expires,omitempty"`
}

type DiameterPeerConfig struct {
    Host string `yaml:"host"`
    IP   string `yaml:"ip"`
    Port int    `yaml:"port"`
}

type SCSCFCapConfig struct {
    ID            int    `yaml:"id"`
    Name          string `yaml:"name"`
    MandatoryCaps []int  `yaml:"mandatory_caps,omitempty"`
    OptionalCaps  []int  `yaml:"optional_caps,omitempty"`
}

type IPSECConfig struct {
    Enabled bool `yaml:"enabled"`
}

type AKAConfig struct {
    Algorithm string `yaml:"algorithm"`
}
```

### 4.3 角色解析逻辑

`IMSConfig.ResolveRole(flagRole string) []Role` 在 bootstrap 早期调用：

1. 取 `flagRole`（来自 `--role`）；为空则取 `cfg.IMS.Role`；仍为空则默认 `"all"`。
2. 对 `all`：检查三个旧布尔 `SCSCF_/PCSCF_/ICSCF_`，全为 false 则三者都启用（向后兼容旧行为）。
3. 返回角色集合 `[]Role{RolePCSCF, RoleICSCF, RoleSCSCF}` 或子集。

### 4.4 校验

`ValidateStrict` 新增：

- `role` 取值在 `{pcscf, scscf, icscf, all}` 内。
- 启用的每个角色节非空，`listen` 非空（除非 `role==all` 时复用 `core.listen`）。
- `pcscf.icscf_addr` 当 `pcscf` 启用时必填（除非同进程内 `icscf` 也启用）。
- `icscf.scscf_addr` 当 `icscf` 启用且 `scscf` 不在同进程时必填。
- `icscf.diameter_peers` / `scscf.diameter_peers` 当对应角色启用且非 `all` 同进程模式时必填。

### 4.5 转发地址解析

适配层在装配时通过 `nextHop(role)` 取得下一跳：

- 同进程（`role==all` 且该下一跳角色也在集合内）：返回 `sip:127.0.0.1:<该角色 listen 端口>`。
- 跨进程：返回配置节里的 `icscf_addr` / `scscf_addr`。

## 5. 适配层接口与 SIP 转发流程

### 5.1 共享接口

新增 `internal/ims/cscf` 包定义角色适配器契约：

```go
// internal/ims/cscf/adaptor.go
package cscf

// Role 标识一个 CSCF 角色
type Role int
const (
    RolePCSCF Role = iota
    RoleICSCF
    RoleSCSCF
)

// Adaptor 是一个 CSCF 角色在 ProxyCore dispatch 路径上的挂载点。
type Adaptor interface {
    Role() Role
    // HandleRegister 处理 REGISTER。返回 Action 指示 ProxyCore 后续动作。
    HandleRegister(ctx context.Context, msg *parser.SIPMsg) Action
    // HandleInvite 处理非 REGISTER 初始请求。
    HandleInvite(ctx context.Context, msg *parser.SIPMsg) Action
    // HandleInDialog 处理 in-dialog 请求（BYE/UPDATE/PRACK 等）。
    HandleInDialog(ctx context.Context, msg *parser.SIPMsg) Action
}

// Action 是适配器返回给 ProxyCore 的指令。
type Action struct {
    Kind     ActionKind
    Response *SIPResponse      // 当 Kind == Respond
    Forward  *ForwardTarget     // 当 Kind == Forward
}

type ActionKind int
const (
    ActRespond  ActionKind = iota  // 直接回送 SIP 响应
    ActForward                      // 转发到 next_hop
    ActDrop                         // 丢弃（已自处理）
)

type SIPResponse struct {
    StatusCode uint16
    Reason     string
    Headers    map[string]str.Str
    Body       []byte
}

type ForwardTarget struct {
    URI    string          // 下一跳 SIP URI
    Branch string          // 可选 Via branch，空则由 tm 生成
}
```

设计意图：让 IMS 业务层**不必**直接操纵 `tm.Manager` / `forward.Forwarder`。业务层只声明"我要转发到 X"或"我要回 401"，由 ProxyCore 的薄 dispatch 执行实际网络动作。这让业务包保持纯函数式、可单测。

### 5.2 ProxyCore 接入

`ProxyCore` 新增字段与方法（不改现有字段语义，纯追加）：

```go
// internal/core/proxy/proxy.go
type ProxyCore struct {
    // ... 现有字段不动 ...

    // IMS 角色适配器（按 role 装配，可同时挂多个）
    cscfAdaptors []cscf.Adaptor
}

func (p *ProxyCore) SetCSCFAdaptors(a []cscf.Adaptor) { p.cscfAdaptors = a }

// dispatchRegister 改造：先走 IMS 适配器，未命中再走通用 registrar。
func (p *ProxyCore) dispatchRegister(msg *parser.SIPMsg) {
    for _, a := range p.cscfAdaptors {
        act := a.HandleRegister(p.ctx, msg)
        if p.applyAction(act, msg) { return }
    }
    // 退回原有通用 registrar 逻辑
    p.registrar.HandleRegister(msg)
}
```

`applyAction(act, msg)` 是新增的薄执行器：

- `ActRespond`：用现有 `sl`/响应构造路径回送。
- `ActForward`：调用 `p.forward.Forward(msg, target.URI)`（复用已有 Forwarder）。
- `ActDrop`：不做任何事。

### 5.3 各角色适配器与 SIP 流程

#### P-CSCF Adaptor — `internal/ims/pcscf/adaptor.go`

```
收到 REGISTER:
 1. 解析 IMPU/Contact/Path
 2. 调用 RouteFromRegistration(reg, msg)  →  返回 contacts 或 error
    - 若 contacts 非空（已注册）: Forward → scscf_addr
    - 否则（初始注册）: Forward → icscf_addr
 3. 不终接，永远 ActForward

收到 INVITE:
 1. pcscf.session.HandleInvite(msg) → 记录会话, 建 100 Trying
 2. Forward → icscf_addr
```

P-CSCF 是纯中转：从不直接回 4xx/5xx 终响应（除 100 Trying），永远转发到 icscf 或 scscf。

#### I-CSCF Adaptor — `internal/ims/icscf/adaptor.go`

```
收到 REGISTER:
 1. 取 callID, IMPU, visited_network_id
 2. icscf.SendUAR(ctx, callID, req) → *UAAResult
    - RegistrationCase == ErrorUserUnknown: Respond 403 Forbidden
    - FirstRegistration / SubsequentRegistration:
        a. tbl.Select(callID) → SCSCFCandidate{Name}
        b. 若无候选: Respond 500 Server-Unavailable
        c. Forward → candidate.Name (S-CSCF URI)
    - ServerSelection: 同上但 UAR 已请求 capabilities，先选再转
 3. 选定后 DropList(callID)（释放候选列表）

收到 INVITE (LIR/LIA):
 1. icscf.SendLIR(ctx, callID, req) → *LIAResult
 2. Select → Forward 到选定 S-CSCF
 3. DropList
```

I-CSCF 既有终接（错误情形）又有转发（正常选路）。`SCSCFCandidate.Name` 直接作为 `ForwardTarget.URI`。

#### S-CSCF Adaptor — `internal/ims/scscf/adaptor.go`

```
收到 REGISTER:
 1. scscf.registrar.HandleRegister(msg) → *RegisterResult
    - StatusCode == 401: Respond 401 + WWW-Authenticate
    - StatusCode == 200: Respond 200 + Service-Route + Path + P-Associated-URI
    - StatusCode == 403: Respond 403 Forbidden
 2. S-CSCF 是终接角色，注册流程到此为止

收到 INVITE (in-dialog 或初始):
 1. scscf.session.routeInvite(msg):
    - 查询 registrar.IsRegistered(impu)
    - 命中: Forward → contact.URI (已注册用户)
    - 未命中: Respond 404 Not Found
```

S-CSCF 是终接角色（注册）+ 末端转发角色（会话路由到已注册 contact）。

### 5.4 `--role all` 的同进程链路

`all` 模式下，三个适配器同时挂在 `ProxyCore`。一个进来的 REGISTER 会先到 P-CSCF 适配器（返回 `Forward → icscf_addr`，此处 `icscf_addr = sip:127.0.0.1:<icscf-listen-port>`），由 `forward.Forwarder` 发回本机 I-CSCF listener，再走 I-CSCF 适配器，依此类推到 S-CSCF。

即便同进程，仍然走真实 SIP 网络往返。这让 `--role all` 与 `--role pcscf` 跑的是同一条代码路径，只是 `next_hop` 不同。

### 5.5 错误处理与超时

- Diameter 事务超时（`icscf.SendUAR` 等待 UAA）：适配器返回 `ActRespond 480 Temporarily-Unavailable`，并 `DropList`。
- Forwarder 失败（S-CSCF 不可达）：由 `tm.Manager` 现有重传/超时机制处理，最终产生 408。
- AKA 验证失败：S-CSCF 返回 403（已有逻辑）。
- 候选列表耗尽：I-CSCF 返回 500。

## 6. Bootstrap 改造与文件清单

### 6.1 CLI 改造

`cmd/kamailio/main.go` 的 `run` 子命令新增 `--role` 标志：

```go
case "--role", "-r":
    if i+1 < len(args) {
        opts.Role = args[i+1]
        i++
    }
```

`app.BootstrapOptions` 增字段：

```go
type BootstrapOptions struct {
    ConfigFile string
    LogLevel   string
    RPCAddr    string
    ScriptFile string
    Role       string  // 新增：pcscf|scscf|icscf|all
}
```

### 6.2 Bootstrap 改造

`internal/core/app/bootstrap.go` 的 `NewBootstrap` 在现有通用 pipeline 装配后，新增"IMS 角色装配"段（仅追加，不改动现有 pike/htable/dialog/acc/tm/registrar 装配顺序）：

```go
// 新增：IMS 角色装配
roles := cfg.IMS.ResolveRole(opts.Role)
if len(roles) > 0 {
    adaptors := p.buildIMSAdaptors(roles, cfg.IMS, tmMgr, regMgr)
    pcore.SetCSCFAdaptors(adaptors)
}
```

新增 `internal/core/app/ims_bootstrap.go`（分离 IMS 装配逻辑，保持 bootstrap.go 简洁）：

```go
package app

// buildIMSAdaptors 按角色构造 CSCF 适配器列表。
// 顺序固定为 [pcscf, icscf, scscf]，保证 dispatch 优先级一致。
func (b *Bootstrap) buildIMSAdaptors(
    roles []cscf.Role,
    cfg config.IMSConfig,
    tmMgr *tm.Manager,
    regMgr *registrar.Registrar,
) []cscf.Adaptor {
    var out []cscf.Adaptor
    fwd := b.forwarder  // 已有的 *forward.Forwarder

    for _, r := range roles {
        switch r {
        case cscf.RolePCSCF:
            sh := pcscf.NewSessionHandler()
            icscfAddr := cfg.PCSCF.ICSCFAddr
            if icscfAddr == "" && hasRole(roles, cscf.RoleICSCF) {
                icscfAddr = loopbackSIP(cfg.ICSCF.Listen)
            }
            out = append(out, pcscf.NewAdaptor(sh, regMgr, icscfAddr, cfg.PCSCF.SCSCFAddr, fwd))
        case cscf.RoleICSCF:
            tbl := icscf.NewSCSCFTable()
            tbl.LoadSCSCFs(toCapabilities(cfg.ICSCF.SCSCFCapabilities))
            tbl.SetPreferredSCSCFs(cfg.ICSCF.PreferredSCSCF, true)
            if cfg.ICSCF.EntryExpiry > 0 {
                tbl.SetEntryExpiry(time.Duration(cfg.ICSCF.EntryExpiry) * time.Second)
            }
            txn := cdp.NewTransactionManager(cdp.DefaultCDP(), b.cdpTransport)
            i := icscf.New(&icscf.Config{
                OriginHost:       "icscf." + cfg.Realm(),
                OriginRealm:      cfg.Realm(),
                DestinationRealm: cfg.Realm(),
                ForcedPeer:       cfg.ICSCF.ForcedPeer,
                DefaultTimeout:   5 * time.Second,
                VisitedNetworkID: cfg.ICSCF.VisitedNetworkID,
            }, tbl, txn)
            scscfAddr := cfg.ICSCF.SCSCFAddr
            if scscfAddr == "" && hasRole(roles, cscf.RoleSCSCF) {
                scscfAddr = loopbackSIP(cfg.SCSCF.Listen)
            }
            out = append(out, icscf.NewAdaptor(i, scscfAddr, fwd))
        case cscf.RoleSCSCF:
            reg := scscf.NewRegistrar(cfg.SCSCF.Realm)
            sess := scscf.NewSessionHandler(reg)
            out = append(out, scscf.NewAdaptor(reg, sess, fwd))
        }
    }
    return out
}
```

**Bootstrap 选项**：新增 `Bootstrap.cdpTransport` 字段（仅当任一启用角色含 Diameter 对端时才创建 `cdp.Transport` 并 `ListenAndServe`），避免无 IMS 的部署也被绑定到 Diameter 端口。

### 6.3 角色监听器复用策略

- `--role all`：三个适配器共享 `core.listen` 的端口（同一 ProxyCore，多适配器 dispatch 串联）。
- `--role pcscf` 等：仅该角色的 `listen` 生效，替换 `core.listen` 注入 ProxyCore。

新增 `IMSConfig.ListenFor(roles []Role) []string`：返回该角色集合应使用的 listen 列表（角色节 `listen` 优先于 `core.listen`）。

### 6.4 新增/修改文件清单

| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/ims/cscf/adaptor.go` | 新增 | `Role`/`Adaptor`/`Action`/`ActionKind` 接口与类型 |
| `internal/ims/pcscf/adaptor.go` | 新增 | `pcscf.Adaptor`，调用 `RouteFromRegistration`/`HandleInvite`，返回 `ActForward` |
| `internal/ims/pcscf/adaptor_test.go` | 新增 | 单测：REGISTER/INVITE 路径的 Action 断言 |
| `internal/ims/icscf/adaptor.go` | 新增 | `icscf.Adaptor`，调用 `SendUAR`/`SendLIR`+`Select`，返回 `ActForward`/`ActRespond` |
| `internal/ims/icscf/adaptor_test.go` | 新增 | 单测：UAA 各 RegistrationCase → Action |
| `internal/ims/scscf/adaptor.go` | 新增 | `scscf.Adaptor`，调用 `HandleRegister`/`routeInvite`，返回 `ActRespond`/`ActForward` |
| `internal/ims/scscf/adaptor_test.go` | 新增 | 单测：401/200/403/404 路径 |
| `internal/core/config/config.go` | 修改 | 扩展 `IMSConfig`（角色节、`ResolveRole`、`ListenFor`、向后兼容） |
| `internal/core/config/validator.go` | 修改 | 新增 IMS 角色配置校验规则 |
| `internal/core/config/config_test.go` | 修改 | 新增角色解析、listen 解析、向后兼容测试 |
| `internal/core/app/bootstrap.go` | 修改 | `BootstrapOptions.Role`、调用 `buildIMSAdaptors`、`SetCSCFAdaptors` |
| `internal/core/app/ims_bootstrap.go` | 新增 | `buildIMSAdaptors`、`loopbackSIP`、`hasRole` 辅助 |
| `internal/core/proxy/proxy.go` | 修改 | `cscfAdaptors` 字段、`SetCSCFAdaptors`、`applyAction`、`dispatchRegister/Invite` 接入 |
| `internal/core/proxy/proxy_test.go` | 修改 | `applyAction` 单测、多适配器 dispatch 测试 |
| `cmd/kamailio/main.go` | 修改 | `--role` 标志解析、`opts.Role` 传递 |
| `configs/ims-pcscf.yaml` | 新增 | 示例：单角色 P-CSCF 配置 |
| `configs/ims-icscf.yaml` | 新增 | 示例：单角色 I-CSCF（含 Diameter peer） |
| `configs/ims-scscf.yaml` | 新增 | 示例：单角色 S-CSCF |
| `configs/ims-all.yaml` | 新增 | 示例：三角色同进程 |
| `internal/integration/ims_split_deployment_e2e_test.go` | 新增 | `--role all` 端到端 REGISTER+INVITE+BYE 三跳验证 |

### 6.5 既有代码不动

- `internal/ims/{pcscf,scscf,icscf}` 业务包的现有 API、字段、单测保持原样。
- `internal/modules/ims_*` 旧模块（含错误的 ims_icscf stub）本期不动，仅作历史保留。
- `forward.Forwarder`、`tm.Manager`、`usrloc.Registrar`、`cdp.TransactionManager` 接口不动，仅被新适配器复用。

### 6.6 实施顺序（每步可独立编译验证）

1. `internal/ims/cscf/adaptor.go` 接口定义 + 单测占位。
2. `internal/core/config` 角色节扩展 + 校验 + 测试。
3. `internal/core/proxy` `SetCSCFAdaptors`/`applyAction` + dispatch 接入 + 测试。
4. 三个 `adaptor.go` + 各自单测（用 stub 业务层）。
5. `internal/core/app/ims_bootstrap.go` + `bootstrap.go` 改造。
6. `cmd/kamailio/main.go` `--role` 标志。
7. 4 个示例配置文件。
8. 端到端集成测试 `ims_split_deployment_e2e_test.go`。

## 7. 测试与验收准则

### 7.1 测试金字塔

```
                    ┌──────────────────────┐
                    │  E2E: 多进程分布式    │  (可选, 慢)
                    ├──────────────────────┤
                    │  E2E: --role all 三跳  │  (必跑, 中)
                    ├──────────────────────┤
                    │  集成: ProxyCore+适配器 │  (必跑, 快)
                    ├──────────────────────┤
                    │  单元: 各 Adaptor       │  (必跑, 极快)
                    └──────────────────────┘
```

### 7.2 单元测试（每个 Adaptor）

**P-CSCF Adaptor** — `pcscf/adaptor_test.go`

| 测试 | 输入 | 期望 Action |
|---|---|---|
| `TestPCSCFAdaptor_InitialRegister_ForwardsToICSCF` | REGISTER, 无注册记录 | `Forward{URI=icscf_addr}` |
| `TestPCSCFAdaptor_ReRegister_ForwardsToSCSCF` | REGISTER, 有注册记录 | `Forward{URI=scscf_addr}` |
| `TestPCSCFAdaptor_Invite_ForwardsToICSCF` | INVITE | `Forward{URI=icscf_addr}` |
| `TestPCSCFAdaptor_LoopbackNextHop` | role==all, icscf 在集合内 | `Forward{URI=sip:127.0.0.1:<icscf-port>}` |
| `TestPCSCFAdaptor_NoICSCFAddr_Errors` | icscf_addr 空 + 跨进程 | `Respond{500}` |

用 stub `*usrloc.Registrar`（返回预设 contacts）+ stub `pcscf.SessionHandler`，断言 Action 字段，不触网。

**I-CSCF Adaptor** — `icscf/adaptor_test.go`

| 测试 | UAAResult 场景 | 期望 Action |
|---|---|---|
| `TestICSCFAdaptor_FirstReg_SelectsAndForwards` | ExCodeFirstRegistration + ServerCapabilities | `Forward{URI=选定的 candidate.Name}` |
| `TestICSCFAdaptor_SubsequentReg_ForwardsToNamedSCSCF` | ExCodeSubsequentRegistration + ServerName | `Forward{URI=ServerName}` |
| `TestICSCFAdaptor_UserUnknown_Responds403` | ExCodeErrorUserUnknown | `Respond{403}` |
| `TestICSCFAdaptor_NoCandidate_Responds500` | UAA 成功但 tbl.Select 返回 ErrNoCandidateList | `Respond{500}` |
| `TestICSCFAdaptor_DiameterTimeout_Responds480` | SendUAR 返回 context.DeadlineExceeded | `Respond{480}` |
| `TestICSCFAdaptor_DropListAfterSelect` | 任意成功路径 | 断言 `tbl.ListCount()==0` |
| `TestICSCFAdaptor_Invite_LIRPath` | LIA 成功 | `Forward{URI=选定 S-CSCF}` |

用 stub `*ICSCF`（注入预设 UAAResult/LIAResult）+ 真实 `SCSCFTable`（LoadSCSCFs 预填），断言 Action 与候选列表状态。

**S-CSCF Adaptor** — `scscf/adaptor_test.go`

| 测试 | registrar 返回 | 期望 Action |
|---|---|---|
| `TestSCSCFAdaptor_InitialReg_Responds401` | RegisterResult{401, WWW-Authenticate} | `Respond{401, headers含WWW-Authenticate}` |
| `TestSCSCFAdaptor_AuthSuccess_Responds200` | RegisterResult{200, Service-Route+Path} | `Respond{200, headers含Service-Route/Path/P-Associated-URI}` |
| `TestSCSCFAdaptor_AuthFail_Responds403` | RegisterResult{403} | `Respond{403}` |
| `TestSCSCFAdaptor_InviteRegistered_ForwardsToContact` | IsRegistered=true, GetContact=URI | `Forward{URI=contact}` |
| `TestSCSCFAdaptor_InviteUnknown_Responds404` | IsRegistered=false | `Respond{404}` |

用 `scscf.NewRegistrar` + `SetRecordForTest` 预填注册记录。

### 7.3 集成测试（ProxyCore + 适配器）

**`internal/core/proxy/proxy_ims_test.go`**

| 测试 | 场景 | 断言 |
|---|---|---|
| `TestProxyCore_ApplyAction_Respond` | Action{Respond,200} | 通过 sl 发出 200 响应 |
| `TestProxyCore_ApplyAction_Forward` | Action{Forward,URI} | 通过 forwarder 发出请求（用 LoopbackTransport 捕获） |
| `TestProxyCore_ApplyAction_Drop` | Action{Drop} | 无网络动作 |
| `TestProxyCore_DispatchRegister_RoutesToFirstAdaptor` | 两适配器，第一个返回 Forward | 只调用第一个 |
| `TestProxyCore_DispatchRegister_FallsBackToRegistrar` | 无适配器 | 走通用 registrar |
| `TestProxyCore_MultiAdaptorOrder` | pcscf+icscf 同时挂载 | REGISTER 先到 pcscf |

用 `LoopbackTransport`（cdp 已有）+ stub 适配器，验证 ProxyCore 的 dispatch 编排。

### 7.4 E2E：`--role all` 三跳

**`internal/integration/ims_split_deployment_e2e_test.go`**

启动单个 ProxyCore，挂载三适配器，三角色 listen 在 localhost 不同端口。用 SIP UAC stub 发完整流程：

```
UE → P-CSCF(:5060) → I-CSCF(:5061) → S-CSCF(:5062)
                                   ← 401 Challenge
UE → P-CSCF → I-CSCF → S-CSCF
                         ← 200 OK + Service-Route
UE → P-CSCF → I-CSCF → S-CSCF → 已注册 contact
                                   ← 200 OK (INVITE)
UE → BYE 链路
```

| 测试用例 | 断言 |
|---|---|
| `TestE2E_Register_SuccessFlow` | UE 收到 200 + Service-Route/Path/P-Associated-URI |
| `TestE2E_Register_UserUnknown` | UE 收到 403（HSS stub 返回 ExCodeErrorUserUnknown） |
| `TestE2E_Invite_Registered` | UE 收到 200，会话建立 |
| `TestE2E_Invite_Unregistered` | UE 收到 404 |
| `TestE2E_Bye_TearsDownSession` | BYE 走完整链路，会话清除 |
| `TestE2E_DiameterTimeout` | HSS stub 不应答 UAR，UE 收到 480 |
| `TestE2E_RaceConcurrentRegisters` | 并发 10 个 REGISTER 不同 IMPU | 全部正确终接，无候选列表串扰 |

HSS 用 stub `cdp.MessageHandler` 返回预设 UAA/LIA。SIP UAC 用现有 `internal/integration` 测试工具。

### 7.5 E2E：多进程分布式（可选，慢层）

**`internal/integration/ims_multi_process_e2e_test.go`**（标记 `//go:build integration_slow`）

用 `os/exec` 启动三个 `kamailio-go run --role pcscf/icscf/scscf -f <各自配置>` 子进程，验证真分布式链路。仅在本机或 CI 慢任务跑。

### 7.6 配置测试

**`internal/core/config/config_test.go`** 新增：

| 测试 | 输入 | 断言 |
|---|---|---|
| `TestIMSConfig_ResolveRole_FlagOverridesConfig` | flag=pcscf, cfg.Role=all | `[RolePCSCF]` |
| `TestIMSConfig_ResolveRole_AllWithLegacyBooleans` | flag="", cfg.SCSCF_=true | `[RoleSCSCF]` |
| `TestIMSConfig_ResolveRole_AllDefaultsToAll` | flag="", cfg.Role="", 无旧布尔 | `[PCSCF,ICSCF,SCSCF]` |
| `TestIMSConfig_ListenFor_SingleRole` | role=pcscf, pcscf.listen=[udp:5060] | `[udp:5060]` |
| `TestIMSConfig_ListenFor_AllReusesCore` | role=all, core.listen=[udp:5060] | `[udp:5060]` |
| `TestIMSConfig_BackwardCompat_OldFlatFields` | 旧配置（ims.realm 等） | 解析成功，角色节用默认值填充 |
| `TestValidate_IMSRoleInvalid` | role=foo | 报错 |
| `TestValidate_PCSCFMissingICSCFAddr` | role=pcscf 跨进程, icscf_addr 空 | 报错 |
| `TestValidate_ICSCFDiameterPeersRequired` | role=icscf 单进程, diameter_peers 空 | 报错 |

### 7.7 验收准则（Definition of Done）

实施完成需全部满足：

1. **构建**：`go build ./...` 通过，无新增 lint 警告。
2. **全量测试**：`go test -race ./...` 通过（不含预存的 `TestTransportListenBadAddress` root 环境问题）。
3. **单元覆盖**：三个 Adaptor 各自 ≥85% 行覆盖（`go test -cover`）。
4. **角色解析**：`--role pcscf|scscf|icscf|all` 四种模式均能启动对应适配器，其余不装配。
5. **配置兼容**：旧 YAML 配置（无角色节）在 `--role all` 下行为不变。
6. **E2E 三跳**：`TestE2E_Register_SuccessFlow` 等 7 个必跑用例全过。
7. **示例配置**：4 份 `configs/ims-*.yaml` 经 `kamailio-go check-config -f <文件>` 校验通过。
8. **文档**：README 增"IMS 分角色部署"小节，含 `--role` 用法与配置示例链接。
9. **无业务包回归**：`internal/ims/{pcscf,scscf,icscf}` 现有单测全过，无 API 改动。

### 7.8 风险与回退

| 风险 | 缓解 |
|---|---|
| `applyAction` 接入破坏现有 dispatch | 保留 `cscfAdaptors` 为空时退回原逻辑；分步提交，每步可独立验证 |
| 同进程三跳性能（SIP 往返开销） | 仅是部署/测试便利，生产用真分布式；不优化 |
| Diameter stub 在 E2E 不够真实 | 保留 `cdp.LoopbackTransport` 真实往返，仅替换 HSS 端应答内容 |
| 旧 `internal/modules/ims_*` 与新业务层混淆 | README 注明 `internal/ims/` 为现行业务层；本期不动旧模块 |
