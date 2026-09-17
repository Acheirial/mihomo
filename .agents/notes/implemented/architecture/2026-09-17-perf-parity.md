# Agent Note: 对照 sing-box 的运行时与架构补齐

Status: implemented

## Problem

对照 `/home/dev/sing-box@cb33600`（testing）与本仓库落地前 `@1120c5ae`。用户侧感知「占用高 / 性能低 / 架构受诟病」对应 4 类可指认差距：

1. DNS 每查询 `DialContext`（`dns/client.go` `ExchangeContext`），无 ConnPool；统计器无条件 Join。
2. TUN TCP 无 splice：钉死 `sing-tun v0.4.24` / `sing v0.5.7`，无 `SpliceSocket` / `CopyWithIncreateBuffer`。拷贝曾固定 `bufio.Copy`。
3. `match()` 全程 `configMux.RLock()`；UDP 64 槽满则静默 `packet.Drop()`。
4. 包级全局 + `ApplyConfig` 一把 `mux` 覆盖 provider `Initial()` 和 `runtime.GC()`。

二进制体积差和 transport/ 三份 ws/TLS 选择不是本笔记实施项——后者已在 [2026-09-17-transport-protocol-security-stack](./2026-09-17-transport-protocol-security-stack.md) 收敛为 `StreamStack` + `vmess.StreamTLSConn`。

## Decision

落地四条运行时 workstream 加最小架构切口。本轮不做 splice；本轮不做 DI。

- **DNS**：`dns/connpool.go` 的 `udpConnPool`（`Acquire`/`Release`，`dnsUDPIdleTimeout = 30s`）。UDP `ExchangeContext` 走 pool；TCP/DoT/DoH 仍每查询拨号。读超时/写失败 `Release(false)`，不叠一层重试。`ResetConnection` 关闭 pool。
- **日志与统计**：`Observable.HasSubscribers()`；`Infoln`/`Warnln`/`Errorln`/`Debugln` 在 level 不够且无订阅者时跳过 `fmt.Sprintf`。`experimental.disable-connection-tracking`（默认 false）经 `tunnel.SetTrackConnections`；`NewTCPTracker`/`NewUDPTracker` 仅在 `pushToManager` 时 Join，Close 仅在 `joined` 时 Leave。
- **UDP / 锁**：`udpDropped` 在 Drop 时累加；`GET /connections` 顶层 `udp-dropped`。`match()` 只在拷贝 rules slice header 与 proxies map 引用时持 `configMux.RLock()`。`runtime.GC()` 在 `ApplyConfig` 的 `mux.Unlock()` 之后。
- **拷贝**：`common/net/sing.go` `Relay` 两路 `copyWithIncrease`：累计写入超过 512KiB 后缓冲从 `pool.RelayBufferSize` 升到 65535。
- **架构**：`ApplyConfig` 注释锚到本笔记。不改 FakeIP 全局 mutex、DNS `D.Msg.Copy()`、transport 三份 ws 选择。

## Alternatives considered

- **升级到 sing v0.9 / sing-tun 带 SpliceSocket 的版本** — 最强论据是直接获得 splice + `CopyWithIncreateBuffer`，与 sing-box 同源。否决：mihomo 的 `go 1.20` 约束写在 `go.mod` 第 3 行，metacubex/sing 是 fork，升主版本会牵动 tun/vmess/quic 一组 replace；本轮用本地 `copyWithIncrease` 隔离风险。
- **把内核改成 Box 式 context DI** — 最强论据是从根上消灭全局 + 整表替换。否决：`tunnel`/`resolver`/`listener` 包级 var 是现有热重载契约，改 DI 等于重写 `ApplyConfig` 所有调用方；本轮只把 GC 移出锁并加 Note 锚点。
- **UDP 队列改为无界或增大到 4096** — 最强论据是不再丢包。否决：无界在 flood 下 OOM；默默加大只是推迟丢包。保留 64 槽 + 暴露计数，让面板可见。
- **默认关闭 connection tracking** — 最强论据是省掉每连接 UUID/Join。否决：Clash 面板开箱依赖 `/connections`；默认保持 true，仅提供 `experimental.disable-connection-tracking`。

## Consequences

- **收益**：UDP DNS 可复用 30s 内空闲连接；关闭 tracking 时 `/connections` 为空且不 Join；丢包可观测；规则匹配不再全程持读锁；大流量拷贝升到 64KiB 档；热重载 GC 不再占住 `mux`。
- **代价与已知上限**：复用 UDP DNS 连接会把 NAT 映射失效表现为偶发超时（Release(false) 必须覆盖 timeout）。`match()` 循环可能看到被替换的旧 slice（与整表替换窗口同类）。tracking 关闭后面板连接列表空。本地 `copyWithIncrease` 与未来 sing 的 `CopyWithIncreateBuffer` 会重复——升级 sing 时删本地实现。若未来 `tun.SpliceSocket` 出现，另开 proposed，不在本篇预埋空接口。

## Verification

- `go test ./dns/ -count=1`：两次连续 UDP Exchange 只 Dial 一次；idle>30s 再 Dial。
- `go test ./common/net -count=1 -run TestCopyWithIncrease`。
- `npx tsx .agents/skills/write-notes-like-deepseek/scripts/verify-agent-note-tree.ts` 与 `verify-agent-note-format.ts`。
