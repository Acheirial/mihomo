# Agent Note: 对照 sing-box 的运行时与架构补齐

Status: proposed

## Problem

对照 `/home/dev/sing-box@cb33600`（testing）与本仓库 `@1120c5ae`。用户侧感知「占用高 / 性能低 / 架构受诟病」对应 4 类可指认差距：

1. DNS 每查询 `DialContext`（`dns/client.go` `ExchangeContext` 内 `c.dialer.DialContext`），无 ConnPool；统计器无条件常开（`tunnel/tunnel.go` `statistic.NewTCPTracker(..., true)`；`tunnel/dns_dialer.go` 三处 tracker）。
2. TUN TCP 无 splice：当前钉死 `github.com/metacubex/sing-tun v0.4.24`、`github.com/metacubex/sing v0.5.7`，模块内无 `SpliceSocket` / `CopyWithIncreateBuffer`。拷贝固定 `bufio.Copy`（`common/net/sing.go` `Relay`）。
3. `match()` 全程 `configMux.RLock()`（`tunnel/tunnel.go` `match`）；UDP 64 槽满则 `packet.Drop()` 静默丢（`HandleUDPPacket` select-default）。
4. 包级全局 + `ApplyConfig` 一把 `mux` 锁覆盖 provider `Initial()` 和 `runtime.GC()`（`hub/executor/executor.go`）。

二进制体积差（本机 79MB vs 58MB）和 transport/ 代码重复（vmess/vless/trojan 各一份 ws/TLS 选择）不作为本笔记实施项——那是协议自带引擎的产品取舍，只在 Alternatives 记录。

## Proposal

拟议 4 条可并行 workstream（验证上 2 依赖 1 的测试环境除外），外加一条最小架构切口。本轮不做 splice；本轮不做 DI。

### Workstream A — DNS 连接复用

在 `dns/` 新增 `connPool`（新文件 `dns/connpool.go`，本仓库无等价物；不要引入 sing-box 的泛型 `ConnPool[T]`，Go 1.20 兼容但风格不一致）。类型：

```
type udpConnPool struct {
    mu   sync.Mutex
    conn net.Conn
    idle time.Time
}
func (p *udpConnPool) Acquire(ctx context.Context, dial func(context.Context) (net.Conn, error)) (net.Conn, error)
func (p *udpConnPool) Release(conn net.Conn, reuse bool)
```

行为：`Acquire` 在 `conn != nil && time.Since(idle) < 30s` 时直接返回已有连接；否则 `dial`。`Release(reuse=false)` 关闭并置 nil。空闲超时 30s 写死常量 `dnsUDPIdleTimeout = 30 * time.Second`，不进配置。

改 `dns/client.go` `ExchangeContext`：UDP schema（`c.schema == "udp"`）走 pool；TCP/DoT/DoH 保持现有每查询拨号（这些协议已有自己的会话，不在本 workstream）。`ResetConnection` 目前是空实现（`dns/client.go`），改为关闭 pool 里的 conn。

错误处理：读超时 / 写失败 → `Release(conn, false)` 并返回原 error；不重试（现有 `resolver.go` 已有 3 次 Opcode 重试，不叠一层）。

验证：`go test ./dns/ -count=1` 加表驱动：两次连续 UDP Exchange 对 mock dialer 只 Dial 一次；idle>30s 后第二次 Dial。

### Workstream B — 日志惰性格式化 + 统计器可关

`log/log.go`：`Infoln`/`Warnln`/`Errorln`/`Debugln` 在 `newLog` 之前先比较 `level`。若 `logLevel < level` 且 `source` 无订阅者，直接 return，不 `fmt.Sprintf`、不 `logCh <-`。探测订阅：给 `common/observable` 加 `func (o *Observable[T]) HasSubscribers() bool`（当前 `observable.go` 无此方法）。有订阅者时仍推 channel（外部控制器依赖 log stream），但 stdout `print` 仍按 level 过滤。

统计器：`config.Experimental`（`config/config.go` `type Experimental`）新增字段：

```
FindProcessMode 保持不动
DisableConnectionTracking bool  // yaml: disable-connection-tracking
```

YAML 键名 `disable-connection-tracking`，默认 false（行为不变）。`config` 解码沿用现有 Experimental 字段映射（`config/config.go` parseExperimental 附近）。

`hub/executor` 把该 flag 写入 `tunnel` 包级 atomic bool `var trackConnections = atomic.NewBool(true)`（`common/atomic` 已有，`tunnel/tunnel.go` 顶部与 `status` 并列）。`handleTCPConn` / `handleUDPConn` / `dns_dialer.go` 三处 `NewTCPTracker`/`NewUDPTracker`：`pushToManager` 参数改为 `trackConnections.Load()`。

`NewTCPTracker`/`NewUDPTracker`：`if pushToManager { manager.Join(t) }`（当前 `manager.Join(t)` 无条件执行，`pushToManager` 只控制流量计数 Push）。Close 里 `Leave` 也只在 Join 过时调用（给 tcpTracker 加 `joined bool` 字段，Join 时置 true）。当 tracking 关闭时仍构造 tracker（UUID/Metadata 仍分配——刻意最小改动，不拆 TrackerInfo；只是不 Join）。

默认关闭 tracking 时：REST `/connections` 返回空列表。

文档：`docs/docs/config/` 里 Experimental 节加一行中英混排注释，键名 `disable-connection-tracking`。

### Workstream C — UDP 背压可见 + match 锁收窄

`tunnel/tunnel.go` `HandleUDPPacket` 的 `default: packet.Drop()` 保留丢包（避免无界队列），但增加包级 atomic 计数 `var udpDropped atomic.Int64`（与 `status` 一样走 `common/atomic`）。Drop 时 `udpDropped.Add(1)`。REST：向已有 snapshot/traffic 响应加 `udp-dropped` int64，不新开 endpoint。若现有结构没有合适宿主，加在 `GET /connections` 的顶层 sibling 字段 `udp-dropped`。

`match()`：把 `configMux.RLock()` 范围从整个函数缩到「拷贝 `getRules(metadata)` 返回的 slice 引用 + `proxies` map 引用」两行，然后 Unlock，再循环 Match。规则 slice 本身只在 `UpdateRules` 时整体替换（`tunnel.go` `UpdateRules`），拷贝 slice header 即可，不深拷贝。`proxies[ada]` 在 Unlock 之后读：reload 窗口可能读到旧 adapter 或 miss；miss 时 `continue`（已有 `if !ok { continue }`）。不引入 snapshot 结构体。

`ApplyConfig`：`runtime.GC()` 从 `mux` 临界区移到 `mux.Unlock()` 之后。改法：把 `defer mux.Unlock()` 改为显式 Unlock，放在 `tunnel.OnRunning()` 之后、`updateUpdater` 之前；`runtime.GC()` 放 Unlock 之后、`updateUpdater` 之前。`loadProvider` 仍在锁内（provider Initial 失败只打日志的行为不变）。

### Workstream D — 自适应拷贝

`github.com/metacubex/sing v0.5.7` 无 `CopyWithIncreateBuffer`。不要升级 sing（会牵动 Go 1.20 兼容与 fork 差异）。在 `common/net/sing.go` 把 `Relay` 的两处 `bufio.Copy` 换成本地实现 `copyWithIncrease`（新函数，同文件）：

```
func copyWithIncrease(dst io.Writer, src io.Reader) (int64, error)
```

实现：循环 `pool.Get(n)` / `src.Read` / `dst.Write` / `pool.Put`；累计写入超过 `512*1024` 后把 n 从 `pool.RelayBufferSize`（32KiB，`common/pool/buffer_standard.go`）升到 `65535`。方向结束不把 enlarged buffer 放回错误档位：`pool.Put` 已按 cap 分档（`common/pool/alloc.go`），65535 会进 64KiB 池，可接受。错误：Read/Write err 原样返回；`io.EOF` 当成功。`closeWrite` 逻辑保持 `Relay` 现有。

splice：当前 `sing-tun v0.4.24` 无 `SpliceSocket`/`GoConn.Splice`。本轮不做 splice。若未来升级 sing-tun 出现 `tun.SpliceSocket`，再开一篇新 proposed 接 `handleSocket` 前的 fast path；本篇不预埋空接口。

### Workstream E — 架构层最小切口

不把包级全局改成 context DI。只做两件：

1. `ApplyConfig` 的 `mux` 不再覆盖 `runtime.GC()`（C 已含）。
2. 在 `hub/executor/executor.go` `ApplyConfig` 顶部注释一行：`// Note: 包级全局 + 整表替换；热重载 = 全量 ApplyConfig。见 .agents/notes/proposed/architecture/2026-09-17-perf-parity.md`。

DNS middleware 的 `D.Msg.Copy()`、FakeIP 全局 mutex、transport/ 三份 ws 选择逻辑：本笔记不改。

## Workstreams

落地顺序：A/B/C/D/E 代码可并行；验证时 B 的 `/connections` 空列表契约与 A 的 DNS 测试互不依赖。符号锚点：`udpConnPool`、`disable-connection-tracking`、`udpDropped`、`copyWithIncrease`。

## Alternatives considered

- **升级到 sing v0.9 / sing-tun 带 SpliceSocket 的版本** — 最强论据是直接获得 splice + `CopyWithIncreateBuffer`，与 sing-box 同源。否决：mihomo 的 `go 1.20` 约束写在 `go.mod` 第 3 行，metacubex/sing 是 fork，升主版本会牵动 tun/vmess/quic 一组 replace；本轮用本地 `copyWithIncrease` 隔离风险。
- **把内核改成 Box 式 context DI** — 最强论据是从根上消灭全局 + 整表替换。否决：`tunnel`/`resolver`/`listener` 包级 var 是现有热重载契约，改 DI 等于重写 `ApplyConfig` 所有调用方；本轮只把 GC 移出锁并加 Note 锚点。
- **UDP 队列改为无界或增大到 4096** — 最强论据是不再丢包。否决：无界在 flood 下 OOM；默默加大只是推迟丢包。保留 64 槽 + 暴露计数，让面板可见。
- **默认关闭 connection tracking** — 最强论据是省掉每连接 UUID/Join。否决：Clash 面板开箱依赖 `/connections`；默认保持 true，仅提供 `experimental.disable-connection-tracking`。

体积差与 transport 三份 ws/TLS 选择：产品取舍，本轮不做。

## Acceptance criteria

- 代码落地后：UDP DNS 连续两次 Exchange 只 Dial 一次；idle>30s 再 Dial。
- `disable-connection-tracking: true` 时 REST `/connections` 为空列表。
- `GET /connections`（或现有 traffic snapshot）含 `udp-dropped`。
- `match()` 不再全程持 `configMux.RLock()`；`runtime.GC()` 不在 `ApplyConfig` 的 `mux` 内。
- `Relay` 走 `copyWithIncrease`；本轮不做 splice / 不做 DI。
- 笔记路径恰好是 `proposed/architecture/2026-09-17-perf-parity.md`，且通过 note tree/format 校验。

## Risks

- DNS pool 复用连接会把偶发 NAT 映射失效表现为「偶发超时」而非「每次新建」；Release(false) 路径必须覆盖 timeout。
- `match()` 提前 Unlock 后，规则循环可能看到被替换的旧 slice（旧 adapter 仍被 in-flight 连接引用，与今天整表替换窗口同类）。
- `disable-connection-tracking: true` 时外部控制器连接列表为空，必须在文档写明。
- 本地 `copyWithIncrease` 与未来升级 sing 的 `CopyWithIncreateBuffer` 会重复；升级 sing 时删本地实现。
