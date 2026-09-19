# Agent Note: 2A 在 metacubex/sing-tun 补 go 栈与 SpliceSocket

Status: implemented

## Problem

Linux TUN 直连满速弱于 sing-box。根因不在 Clash YAML：`github.com/metacubex/sing-tun v0.4.24` 没有 `NewGo` / `SpliceSocket` / `GoConn.Splice` / `UDPNatConn.Splice` / `JudgeFlow`。Stage 1 已把编译器下限升到 Go 1.24（见 [Stage 1 编译器下限上移到 Go 1.24](../process/2026-09-18-go-compiler-floor-1.24.md)），默认栈仍是 `gvisor`（`config/general.go` `Tun.Stack = C.TunGvisor`）。gVisor `gonet.TCPConn` 不是 `syscall.Conn`，用户态 Relay 即使能走 sing v0.5.7 的 `copyDirect`/`unix.Splice`，TUN 引擎里的字节仍进用户态拷贝。

[对照 sing-box 的运行时与架构补齐](./2026-09-17-perf-parity.md) 与 [入站传输/安全层组合 + perf-parity 收尾](./2026-09-18-inbound-stack-and-perf-completion.md) 当时否决升 sing / sing-tun：`go 1.20` 钉住 replace 链，升主版本会拖 tun/vmess/quic。那一轮用本地 `copyWithIncrease` 隔离风险，并写明 TUN `SpliceSocket` 另开窗口、不预埋空接口。[Relay 走单向 copyDirect/splice](./2026-09-18-relay-socket-splice.md)（0a）只救 system/mixed 的本地 TCP listener，不解 TUN `go` 栈，也不让 gVisor 路径 splice。

不做的后果：DIRECT TUN 永远是双核用户态拷贝；`stack: go` 无法解析；auto-redirect 停在 v0.4.24 的无 `JudgeFlow` 表面。

## Decision

选路 **2A**：在 Acheirial fork（`github.com/Acheirial/sing-tun.git`，本地 `/home/dev/sing-tun-acheirial`，branch `go-stack`，module 仍 `github.com/metacubex/sing-tun`）从 sagernet HEAD 移植 go 栈。未切 `sagernet/sing` v0.9，sing 仍是 `github.com/metacubex/sing v0.5.7`。`HandlerEx` / `PacketOffload` / `internal/freelru` / `internal/maphash` 在 fork 内本地垫，不把这些符号推到 sing v0.9。

`NewStack("go")` 走 `NewGo`；空字符串仍按 `IncludeAllNetworks` / `WithGVisor` / GSO 在 gvisor、mixed、system 之间启发式，**never** 默认 `NewGo`。`mips` / `gvisor` / `system` / `mixed` 保留。`NewGo` 要求 `options.Handler` 实现 `HandlerEx`，否则报 `go stack requires HandlerEx`。

mihomo `go.mod`：`require github.com/metacubex/sing-tun v0.4.24` 不动；`replace github.com/metacubex/sing-tun => github.com/Acheirial/sing-tun v0.0.0-20260919165301-ba2bfb79e01a`（branch `go-stack`，commit `ba2bfb7`，含 Android AutoRedirect IPv4 loopback 监听与 IPv6 TPROXY、metacubex/meta 的 gVisor watcher / system NAT / mipstack batch）。CI 走可复现伪版本，不依赖本机路径。

YAML：`stack: go` / `Go` 映射为 `C.TunGo`（iota 4，`String()` 为 `"Go"`，`StackTypeMapping` 收小写）。默认仍 `TunGvisor`（`config/general.go` 与 `listener/parse.go`）。`inet4-address` 未删。`endpoint-independent-nat: true` → `UDPMapping = NATMappingEndpointIndependent`，否则 `NATMappingAddressAndPortDependent`。未加 `udp-mapping` 新键。

`*sing_tun.ListenerHandler` 实现 `tun.HandlerEx`：

- `JudgeFlow`：UDP（network==17）且 DNS dest 命中 `ShouldHijackDns` → `ActionHijackDNS`；TCP DNS、ICMP 与其余流量 → `ActionAccept`。
- `NewDNSPacket` 异步 `go relayDnsPacket`。
- `NewConnectionEx` / `NewPacketConnectionEx` 包一层现有 `NewConnection` / `NewPacketConnection`，`onClose != nil` 时 `defer onClose(err)`。
- TCP DNS 劫持仍在 `NewConnection` 里走 `resolver.RelayDnsConn`，不经 `JudgeFlow`。
- `PrepareConnection` 的 ICMP 路径未改：gvisor/system 仍走 `ping.ConnectDestination`。go 栈 ICMP echo 由 `goEngine.answerEcho` 本地回，不进 `PrepareConnection`。

面板 `/connections` 仍走现有 tracker。CountFunc 不被 splice unwrap 吃掉——0a 已在 [relay-socket-splice](./2026-09-18-relay-socket-splice.md) 落地；本篇 MUST NOT 把跟踪改成只信 splice `OnClose`。

## Related notes

写本篇前检索（排除 `archived/`）：

- 本窗口原先的 proposed 篇 `2026-09-18-go-upgrade-sing-tun-splice.md` — 完全吸收后物理删除，不归档。本篇接管 Stage 2 2A；Stage 1 已有独立 process 篇。
- [2026-09-18-go-compiler-floor-1.24](../process/2026-09-18-go-compiler-floor-1.24.md) — 部分重叠。Stage 1 只动编译器下限，`require` 仍 v0.4.24。本篇不改写那条 Decision。
- [2026-09-17-perf-parity](./2026-09-17-perf-parity.md) — 部分重叠。当时否决升 sing-tun / SpliceSocket，改本地 `copyWithIncrease`。本篇不改写那条 Decision。
- [2026-09-18-inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) — 部分重叠。再次否决升 sing 到 v0.9.5；`CopyConn` 已回退。本篇不改写。
- [2026-09-16-modernization-pass](../process/2026-09-16-modernization-pass.md) — 部分重叠。该批钉住 `go 1.20` 是当时正确取舍；本篇是另一次发布窗口的 Stage 2，不是把那条 Decision 改写成反面。
- [2026-09-18-relay-socket-splice](./2026-09-18-relay-socket-splice.md) — 部分重叠。0a 的 CountFunc / 单向 splice 是本篇 TUN splice 的前置，不解 TUN `go` 栈。

## Alternatives considered

- **永远停在 sing-tun v0.4.24** — 最强论据是 Win7 / 老 macOS 兼容面、CMFA gomobile 与一组 pinned 依赖都不用动，0a 的 socket splice 已能救 system 栈 TCP。否决为终局：gVisor / 无 `NewGo` 的 TUN 没有 `syscall.Conn`，不移植就没有 `GoConn.Splice`；`JudgeFlow` / `NewDNSPacket` 也编不过。0a 是必要前置，不是 TUN 追平本身。

- **单步跳到 Go 1.25 + sagernet sing / sing-tun / gvisor 全家桶** — 最强论据是一次拿到 SpliceSocket、`go` 栈、JudgeFlow、`CopyWithIncreateBuffer`，与 sing-box 同源。否决为单步：metacubex 的 vmess/mux/quic replace 链与 sagernet 包名一次性冲突；mips 会在 sagernet 树上消失；回滚粒度变成「编译器 + 协议 + TUN」三件套。Stage 1 已把下限停在 1.24；本篇只在 metacubex module 路径上垫 go 栈，sing 仍 v0.5.7。

- **2B 切 sagernet、丢掉 mips 栈** — 最强论据是 sagernet `NewStack` 没有 `mips`，少一条要 vendor 的引擎，少一处双栈测试，也能直接吃上游 `go` 栈而不用本地 fork。否决：`stack: mips` 是现网 YAML 值（`constant/tun.go` `TunMips`），`github.com/metacubex/mipstack` 已在 `go.mod`；删栈等于静默破坏老配置。2A 继续 vendor mipstack，YAML 枚举保留。

## Consequences

- **收益**：Linux 上显式 `stack: go` 的 DIRECT TCP/UDP 可走 `GoConn.Splice` / `UDPNatConn.Splice`；UDP DNS dest 经 `JudgeFlow`+`NewDNSPacket` 劫持；gvisor/system/mixed/mips 与 `inet4-address` / `endpoint-independent-nat` 老配置仍解析。
- **代价与已知上限**：默认栈仍是 gvisor，不改配置吃不到 go 栈满速——改默认属于下一个大版本。replace 绑 Acheirial `go-stack` 伪版本，不绑 metacubex 上游 tag；fork 变了要另 bump 伪版本。go 栈 ICMP echo 本地回，不走 `PrepareConnection` 直连 ping，gvisor/system 的 ping 路径与 go 栈不一致。`JudgeFlow` 不劫持 TCP DNS（仍走 `NewConnection`）。Windows `receiveFrom` 原先误用未 import 的 `M.SocksaddrFromNetIP`，`GOOS=windows` 编不过；已改为本地 `addrPortFromRawSockaddr`，replace 伪版本 bump 到 `v0.0.0-20260919005719-e67875be6b79`。
- **重访**：默认栈改 `go` 须单独发布说明。metacubex 上游若合并 go 栈则删 replace、改 require。要切 sagernet 全家桶或升 sing v0.9 则另开窗口，不在本篇加码。
