# Agent Note: 入站传输/安全层组合 + perf-parity 收尾与泄漏修复

Status: implemented

## Problem

出站侧已用 `adapter/outbound/streamstack.go`（`StreamStack`）把安全层与传输层收敛为可自由组合的栈，入站侧仍是每监听器各自手写。`InboundMap` 审计列出 18 类重复（D1–D18），其中真正阻碍「自由搭配」的是三组：

- **D1/D3**：TLS 选项构建（`tls.Config{Time:ntp.Now}` → `ca.NewTLSKeyPairLoader` → `GetCertificate` 闭包 → `ech.LoadECHKey` → `ClientAuthTypeFromString` → NoClientCert 提升 → `LoadCertificates` → `ClientCAs`）在 12 个监听器里逐字重复，其中 11 个是入站。
- **D5**：`securityModes` 切片 + `security modes are mutually exclusive: <joined>` 错误 + shadowTLS/restls/jls/reality/tlsMirror 构建器链，6 个入站站点各一份；出站已收敛为 `checkExclusiveSecurityModes` + `collectSecurityModes`。
- **D6**：`WsPath` → `http.NewServeMux` + `StreamUpgradedWebsocketConn`、`GrpcServiceName` → `gun.NewServerHandler`、vless 额外一份 `xhttp.NewServerHandler`，三向逐字重复，且 ALPN 设置（`http/1.1`、`h2` 顺序、mekya 归一化）散落各处。

同时用户报告上一轮 perf-parity 「没做尽而且有内存泄漏」。逐行复核后定位两处真实缺陷（其余已验证为良性）：

1. **`dns/client.go` 截断重试路径泄漏连接 + goroutine。** `ExchangeContext` 的 `defer` 在 `select` 的 `ctx.Done()` 分支把 `reuse=false`，随后 `Release(conn,false)` 关闭连接；但后台 goroutine 仍在该 conn 上跑 `ExchangeWithConn`（miekg/dns 不响应 ctx 取消），conn 被关闭后 goroutine 阻塞在已死连接的读上，5 秒 `Timeout` 后才返回——高 QPS + 频繁 ctx 超时下 goroutine 与底层 socket 堆积。截断重试路径（`dropUDP=true`）同理：UDP conn 已被 `Release(false)` 关闭，TCP 重试却可能还在用它。
2. **`common/net/sing.go` `copyWithIncrease` 的放大缓冲归还语义有误。** 放大后 `n=65535`，`pool.Get(65535)` 返回 cap 65536 的切片（index 10 档），`pool.Put` 能正确归档；但放大判定 `n == pool.RelayBufferSize && written > threshold` 在 `nr != nw` 的短写路径上已提前 return，放大后第一次 `written` 越阈时机不精确（非 bug）。真正的问题是：`io.EOF` 路径返回 `nil` error，调用方 `Relay` 因此走 `closeWrite` 而非 `Close`——对不支持半关闭的连接，远端永不 EOF，goroutine 与缓冲一直挂着。这在 `with_low_memory` 构建下放大效应更明显（`RelayBufferSize` 只有 16KiB）。

其余已核验为**良性**，不是泄漏：`udpConnPool.Acquire/Release` 的并发语义（单连接池，竞争时第二调用方新建连接，老连接被 `Release` 关闭）、`statistic` tracker 的 `joined` 标志（`Close` 只在 Join 过时 Leave）、`log.HasSubscribers`（无订阅者时跳过 `fmt.Sprintf` 与 channel 发送）、`match()` 锁收窄、`ApplyConfig` 的 `runtime.GC()` 已在 `mux.Unlock()` 之后。

perf-parity 「没做尽」的项：sing v0.5.7 的 `bufio.Copy` 已含 `copyDirect`/`splice`（Linux 上 syscall.Conn 双端自动 splice）与 `CopyExtended` 的缓存读路径，本地 `copyWithIncrease` 只实现了「越阈放大」，没拿到 splice 与 `CachedReader` 直写；`/memory` 与 `Snapshot.Memory` 只在 REST 拉取时更新（`updateMemory` 在 `Memory()`/`Snapshot()` 里同步读 `/proc/<pid>/statm`），没有周期采样；无 `GOMEMLIMIT`/GC 百分比调节出口。

## Decision

落地后与提案有一处关键修订：**Workstream 1a 的「ctx.Done 不关闭 conn」方案被自己的回归测试证伪**——不关 conn 时，永不回复的 mock server 会让 goroutine 活满整个 5s 超时（`TestExchangeContextCancelReleasesGoroutine` 失败）。最终形态：`ExchangeContext` 持有 `mu` + `released` 标志的幂等 `releaseConn(keep)`，ctx 取消分支先 `releaseConn(false)` 主动关 conn——关闭中的 read 立即报错、goroutine 毫秒级退出、`done` 关闭后外层 defer 的 release 变为 no-op。所有权与互斥不再靠「goroutine 独占」约定，而靠幂等守卫。

**1a. `dns/client.go`：消除后台 goroutine 泄漏（最终实现）。**

落地为幂等 `releaseConn`：ctx 取消分支先 `releaseConn(false)` 关 conn 解除 goroutine 阻塞，外层 defer 等 `done` 后的第二次 release 是 no-op；截断重试路径在 goroutine 内部 `releaseConn(false)` 归还 UDP conn 后再拨 TCP，主 defer 永不双重释放。

**1b. `common/net/sing.go`：`copyWithIncrease` 的 EOF 语义修正。**

EOF 路径返回 `io.EOF` 而非 `nil`，让 `Relay` 的错误分支走 `Close()` 而非 `closeWrite()`——对端 EOF 后立即全关，不再让「永不半关闭的对端」把拷贝 goroutine 和池化缓冲钉死到远端超时。放大阈值常量与 `pool.RelayBufferSize` 解耦为显式 `copyIncreaseThreshold`（512KiB）。`with_low_memory` 下放大后 65535 仍走 index 10 档，无归还错误。

**已否决的 splice 快路径**：中途试过「两端均为 `syscall.Conn` 时把双向拷贝整体委托给 sing 的 `bufio.CopyConn`」——独立探针证明 CopyConn 等**两个**方向都 EOF 才返回，而 `Relay` 的契约是**任一**方向结束就关双端；照搬会把半关闭对端的等待变成新连接泄漏，已回退并连同测试重写（`TestRelayClosesOnEOFPeer` 用 net.Pipe 驱动单向流 + 排空读者，断言 2s 内返回且两端读均失败）。

### Workstream 2 — 入站安全/传输组合（InboundStack + InboundTransports）

新增 `listener/security/server.go`（新包 `security`，不放 `adapter/inbound`——后者被 `listener/inbound` 依赖，会成环；也不放 `listener/config`——那里只放纯数据）：

```go
package security

// TLSOption 是所有入站选项结构体共享的 TLS 字段面。
type TLSOption struct {
	Certificate    string
	PrivateKey     string
	ClientAuthType string
	ClientAuthCert string
	EchKey         string
}

// BuildTLS 构建服务端 tls.Config。certRequired=false 时允许无证书（tuic/hysteria2
// 的 QUIC 变体自己保证证书必填，调用方另加 MinVersion/NextProtos）。
func BuildTLS(opt TLSOption, certRequired bool) (*tls.Config, error)

// Modes 收集已启用的安全模式名，用于互斥校验。
func Modes(certificate, shadowTLS, restls, jls, reality, tlsMirror bool) []string

// CheckExclusive 报 "security modes are mutually exclusive: <joined>"。
func CheckExclusive(modes []string) error

// WrapListener 按优先级套安全层：shadowTLS > restls > jls > reality > tls > none。
func WrapListener(l net.Listener, builders Builders, tlsConfig *tls.Config) net.Listener
```

Builder 们（`*shadowtls.Builder` 等）由调用方构造后传入——`reality.Build` 与 `tlsmirror` 需要 `C.Tunnel`，不能在纯函数里完成。

传输层新增 `listener/security/transport.go`（同包）：

```go
// TransportOption 覆盖 ws/grpc/xhttp/mkcp/mekya。
type TransportOption struct {
	WsPath          string
	GrpcServiceName string
	XHTTP           *LC.XHTTPConfig
	MKCP            *LC.MKCPConfig
	Mekya           *LC.MekyaConfig
}

// ApplyTransport 把传输层挂到 httpServer 上（ws/grpc/xhttp），
// 并返回需要包在 listener 外的 mkcp/mekya 监听器构造函数。
// ALPN 归一化（h2 在 http/1.1 前；mekya 需两者皆有）在此一处完成。
func ApplyTransport(server *http.Server, tlsConfig *tls.Config, opt TransportOption,
	connHandler func(net.Conn)) (wrap func(net.Listener) net.Listener, err error)
```

**迁移范围**（D1/D3/D4/D5/D6 全部入站站点）：

- TLS 构建：`listener/{http/server.go, mixed/mixed.go, socks/tcp.go, trusttunnel/server.go, anytls/server.go, hysteria2_realm/server.go, sing_vless/server.go, sing_vmess/server.go, trojan/server.go, sing_hysteria2/server.go, tuic/server.go}` + `hub/route/server.go`（外部控制器，非监听器，但同一份 TLS 逻辑）。
- 互斥校验 + 构建器链 + 监听器套壳：`anytls`、`sing_vless`、`sing_vmess`、`trojan`、`sing_shadowsocks`、`shadowsocks/tcp.go`、`snell`。
- 传输层接线：`sing_vmess`（ws/grpc/mekya ALPN 归一化）、`sing_vless`（grpc/xhttp）、`trojan`（ws/grpc）。

**自由搭配扩展**（`InboundTransports`）：给 `TrojanServer`/`TrojanOption`、`VlessServer`/`VlessOption`（已有 xhttp）、`AnyTLSServer`/`AnyTLSOption`、`SnellServer`/`SnellOption`、`ShadowsocksServer`/`ShadowsocksOption` 加 `MKCPConfig`/`MekyaConfig`（vless 加 `MekyaConfig`；mkcp 与 ShadowTLS/Restls/JLS 互斥，与出站 `StreamStack` 的 `"%s only supports TCP transports"` 同一规则）。YAML 键名 `mkcp-config`/`mekya-config` 与出站完全一致，默认零值不启用——老配置行为不变。

### Workstream 3 — perf-parity 收尾（PerfParity）

- `tunnel/statistic/manager.go`：`handle()` 的秒级 ticker 里追加 `m.updateMemory()`，`Snapshot()`/`Memory()` 直接读缓存值——把 `/proc` 读移出 REST 请求路径。
- `config.Experimental` 加 `GOMemoryLimit uint64`（YAML `go-memory-limit`，单位 MiB，默认 0=不设）→ `debug.SetMemoryLimit`；加 `GOGCPercent int`（YAML `go-gc-percent`，默认 0=不设）→ `debug.SetGCPercent`，在 `hub/executor` 的 `updateExperimental` 应用。
- **未做**：sing 的 `CopyWithCounters`/splice 直写——落地中发现 `bufio.CopyConn` 语义与 `Relay` 不兼容（见上），且 sing 的 `UnwrapCountReader` 会绕开统计器计数，需要先给 tracker 补 CountFunc 包装才安全，本轮回退保留本地 `copyWithIncrease`。

### Workstream 4 — 文档（InboundDocs）

- 新增 `docs/docs/config/inbound/listeners/transport.{md,en.md,ru.md}`（入站传输层配置，对齐出站 `config/proxies/transport.md` 的风格，mkcp/mekya 字段集从 vmess.md 逐键复制），加入 mkdocs nav「通用字段」之后。
- `docs/docs/config/inbound/listeners/{trojan,vless,anytls,snell,ss}.{md,en.md,ru.md}` 各加一行 `mkcp-config`/`mekya-config` 注释（三语本地化）。
- `experimental.{md,en.md,ru.md}` 的 `go-memory-limit`/`go-gc-percent` 已补三语文档（YAML `0` = 不设置）。

## Workstreams

1a/1b/2/3/4 代码互不重叠：1a 只动 `dns/client.go`+`dns/connpool.go`；1b 只动 `common/net/sing.go`；2 的 `listener/security/*` 是新包，迁移只改各监听器的 `New`/`NewWithConfig`；3 只动 `tunnel/statistic/manager.go`+`config/config.go`+`hub/executor`；4 只动 docs。并行派发，互斥文件集。

## Alternatives considered

- **把入站组合器放进 `adapter/outbound/streamstack.go` 复用 `StreamStack`** — 最强论据是出站已验证该抽象。否决：出站组合的是「拨号器 → 安全 → 传输」，入站组合的是「监听器 → 安全 → 传输」，方向相反；`StreamStack` 的 `Dial`/`Wrap`/`Session` 语义与 `net.Listener` 套壳不匹配，强行复用会引入 `Listen` 假实现。共用的是 `checkExclusiveSecurityModes`/`collectSecurityModes` 这对纯函数——它们被移入新 `listener/security` 包后，`adapter/outbound/streamstack.go` 改为引用，不再自持一份。
- **把组合器放进 `listener/config`** — 最强论据是 LC 结构体已在此。否决：`config` 是纯数据包，而构建 TLS/reality/shadowtls builder 需要 `C.Tunnel` 与 `ca`/`ech` 组件，放这里会引入循环（`listener/reality` 已被 `listener/config` 引用）。
- **删掉 LC 结构体层，让 `listener/inbound` 的选项结构体直接喂给叶子监听器** — 最强论据是消除 D7/D8 的字段三重拷贝。否决：`LC.*Server` 是 Path A（`config.General` 的端口/URL 配置）与 Path B（`listeners:` 数组）的公共契约，`ReCreate*` 靠它的 `String()` 做变更检测。本轮只收敛 TLS/安全/传输，结构体层留到下一批。
- **升级 sing 到 v0.9.5 拿 `CopyWithIncreateBuffer`** — 最强论据是与 sing-box 同源、直接获得 splice。否决同上一轮：`go 1.20` 约束 + metacubex fork 的 replace 链；v0.5.7 的 `copyDirect` 已能在 Linux 双 syscall.Conn 时 splice，本轮改用 `CopyWithCounters` 即可吃到。
- **`GOMEMLIMIT` 默认开启一个保守值（如 512MiB）** — 最强论据是直接压住 RSS。否决：mihomo 跑在路由器/容器里，宿主内存差异巨大，硬编码默认值会在小内存设备上触发频繁 GC 反而劣化；默认 0（不设），让需要的用户显式开。

## Verification

实际执行结果（本机 build 慢，全部命令带 `GOTMPDIR=/home/dev/tmp` 并按包分批跑）：

- `go build ./...` → RC=0（362s）。
- `go vet ./listener/security/ ./listener/inbound/ ./listener/config/ ./common/net/ ./dns/ ./config/ ./hub/executor/ ./tunnel/statistic/` → RC=0。
- `go test`（`-count=1`）：`common/net` ok（含新增 `TestRelayClosesOnEOFPeer`，0.4s）；`common/convert` ok；`dns` ok 0.4s（7 个 connpool/Exchange 测试全过，`TestExchangeContextCancelReleasesGoroutine` 从 6.08s 失败降到 0.38s 通过）；`config` ok；`common/convert`/`hub/executor` ok（`tunnel/statistic` 无测试文件）。
- 错误字符串 `security modes are mutually exclusive: …` 逐字未变（RepairMigration 逐文件核对）。
- 笔记通过 `verify-agent-note-tree.ts`（9 notes）与 `verify-agent-note-format.ts`（9 notes）。

## Consequences

- `listener/security` 只被叶子监听器包（`listener/http` 等）引用，未被 `listener/config` 依赖——无环。`adapter/outbound/streamstack.go` 删除了自持的 `checkExclusiveSecurityModes`/`collectSecurityModes`，改引用本包，行为逐字不变。
- `hub/route/server.go`（外部控制器 TLS）本轮**未**迁移，避免给 `route` 加反向依赖；它继续用本地实现，留待后续。
- trojan/vless/anytls/snell/ss 入站选项新增 `mkcp-config`/`mekya-config` 字段，零值（不写）完全不启用、不创建 UDP socket——老配置行为不变。mkcp/mekya 与 ShadowTLS/Restls/JLS 的互斥目前只靠文档，服务端运行时强制是后续项。
- `copyWithIncrease` 返回 `io.EOF` 使 `Relay` 在对端 EOF 时全关双端；`listener/http/upgrade.go` 与 `sudoku/server.go` 这两个 `N.Relay` 调用方随全树构建验证，无行为破坏。
- `go-memory-limit`/`go-gc-percent` 默认 0 时两个 debug 调用都不执行，行为与旧版本一致；实验文档三语已补。
- 内存采样改为秒级后台 tick 后，`GET /memory` 返回的是最近一次采样值（≤1s 陈旧），换来了 REST 路径零 `/proc` 读。
- 升 Go / bump sing-tun 拿 `SpliceSocket` 当时不在本篇范围；该重访已落地为 2A，见 [2A 在 metacubex/sing-tun 补 go 栈与 SpliceSocket](./2026-09-18-go-stack-sing-tun-2a.md)。本轮未做的 `CopyWithCounters`/tracker CountFunc 已由 [relay-socket-splice](./2026-09-18-relay-socket-splice.md) 落地（单向 Copy，不是 CopyConn）。
