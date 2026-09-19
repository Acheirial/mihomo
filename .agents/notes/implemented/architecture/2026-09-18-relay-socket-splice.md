# Agent Note: Relay 走单向 copyDirect/splice，UDP NAT-hit 绕过 64 槽

Status: implemented

## Problem

Linux DIRECT 两端都是真实 TCP socket 时，sing v0.5.7 的 `bufio.Copy` 已经能走 `copyDirect`/`unix.Splice`，但 `common/net/sing.go` 的 `copyWithIncrease` 只做池化 Read/Write 放大。上一轮 [perf-parity](./2026-09-17-perf-parity.md) 明确「本轮不做 splice」；[inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) 试过把双向拷贝整体交给 `bufio.CopyConn`，因「等双 EOF」与 `Relay`「任一端结束就关双端」冲突而回退。结果是：DIRECT 热路径仍用户态拷贝；tracker 虽有 `UnwrapReader/Writer` CountFunc，splice 路径吃不到；UDP 已建 NAT 的后续包仍挤 64 槽 worker，burst 时 `udpDropped` 随 PPS 涨。

不做会怎样：Linux 上与 sing-box 的 socket splice 差距继续存在；握手已写出站 payload 后 retry 最多再拨 10 次；同 key UDP burst 默认丢。

## Decision

`Relay(left, right net.Conn)` 签名与「任一端 EOF → 2s 内双关」不变。`copyWithIncrease` 先 `collectCountReader/Writer` 剥 CountFunc（sing 的 `UnwrapCountReader` 会先 `UnwrapReader`，Replaceable 的 tracker 会被跳过、计数丢失），再循环 `UnwrapCount*` + `CachedReader.ReadCached` 排空 Peek/缓存，然后：

- 两端都是 `syscall.Conn`：单向 `bufio.CopyWithCounters`（内部 `copyDirect`/`splice`）。**从不**调用 `bufio.CopyConn`。
- 否则，或 splice `handed=false`（Linux EINVAL/ENOSYS）：既有池化 Read/Write，512KiB 后 `n=65535`。
- `Copy`/`CopyWithCounters` 把源 EOF 映射成 `err==nil`；函数对外仍返回 `io.EOF`，让 `Relay` 走 `Close()` 而不是 `closeWrite()`。

unwrap 链：

- `tcpTracker`：`ReaderReplaceable`/`WriterReplaceable` 恒 true；`SyscallConn` 转发给内层 `C.Conn`；既有 `UnwrapReader`（download）/`UnwrapWriter`（upload）保留。
- `udpTracker`：Replaceable 恒 true；`UnwrapPacketReader/Writer` 在内层实现 sing `PacketReader/Writer` 时带 CountFunc。
- `outbound.conn`：`SyscallConn` 经 `FindWithUpstream[syscall.Conn]` 只沿 `WithUpstream` 走，**不**跟 `tls.Conn.NetConn()`（那是密文 TCP）。`FindUpstream` 仍走 `NetConn()`，JLS `UserFromConn` 靠它找到内层 `*tls.Conn`。`*net.TCPConn` 仍不包 `deadline.Conn`。
- `BufferedConn.SyscallConn` 由入站工作流实现：仅 `ReaderReplaceable()`（无残留 Peek）时转发，否则 error，禁止 splice-over-peek。

retry：上限 10→3。`errHandshakeWritten` + `errIfHandshakeWritten(n, err)` 给 NeedHandshake 写路径：peek 已写出站后失败不再重拨。UDP 0b：`HandleUDPPacket` 对 `natTable.Get(key)` 命中直接 `sender.Send`，不进 64 槽；miss 仍排队，`senderCapacity` 128 仍有界，`udpDropped` 只计 worker-miss 满槽。

## Alternatives considered

- **`bufio.CopyConn` 包整段 Relay** — 最强论据是少一层自写双向循环、与 sing-box 同源。否决：CopyConn 等两个方向都 EOF 才返回，半关闭对端会把拷贝 goroutine 钉死；[inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) 已用 `TestRelayClosesOnEOFPeer` 证伪并回退。本轮只走**单向** `Copy`/`CopyWithCounters`。
- **升级 sing-tun 拿 `SpliceSocket`** — 最强论据是 TUN 入站也能内核转发，不必等 socket unwrap。否决：pin 在 v0.4.24 / `go 1.20`；升 0.9 要 Go≥1.22 且 CMFA 同意，归工具链窗口，不在本轮。
- **UDP 队列改无界或默认加大到 4096** — 最强论据是 burst 不再 Drop。否决：无界在 flood 下 OOM；默默加大只推迟丢包。NAT-hit 直送绕过 worker，miss 仍 64 槽 + 有界 sender 128。
- **默认关闭 connection tracking** — 最强论据是少 UUID/Join，splice 也不必 CountFunc。否决：Clash 面板 `/connections` 开箱依赖；tracking 默认开，CountFunc 必须在 splice 路径继续累加 upload/download。

## Consequences

- **收益**：Linux DIRECT 双 `*net.TCPConn`（经 tracker/outbound unwrap、BufferedConn 无残留 Peek）走 splice；面板流量在 CountFunc 下仍涨；握手已写 payload 后最多再试 2 次且可立即停；同 key UDP burst 不再默认挤满 64 槽。
- **代价与已知上限**：非 Linux / 非 syscall.Conn / splice EINVAL 仍走池化拷贝，放大阈值未变。`BufferedConn` 有残留 Peek 时 `SyscallConn` 失败，Copy 走用户态——嗅探/握手未 Discard 完不能 splice。`FindWithUpstream` 不跟 `NetConn()`，TLS / Reality / uTLS 出站不会被 splice 打穿；Linux 入站互操作因此才能绿。`FindUpstream` 仍跟 `NetConn()`，不能拿它做 splice 判定。`collectCountReader` 是对 sing `UnwrapCountReader` 顺序的本地补丁，升 sing 若改顺序需重访。TUN 设备 splice 仍要等 sing-tun `SpliceSocket`，见 [perf-parity](./2026-09-17-perf-parity.md) 的已知上限。retry 从 10 降到 3 会让瞬时拨号失败更快放弃，这是刻意的。

## Verification

- `GOTMPDIR=/home/dev/tmp go test ./common/net/ ./tunnel/statistic/ ./adapter/outbound/ -count=1 -timeout 120s`（禁止 `./...`）。
- `TestRelayClosesOnEOFPeer`：net.Pipe 单向 EOF，2s 内 Relay 返回且两端读失败。
- `TestCopyWithIncreaseCountFuncOnReplaceable`：Replaceable 包装的 CountFunc 在拷贝后仍累加。
- `TestBufferedConnResidualPeekBlocksSyscall`：残留 Peek 时 `SyscallConn` 报 `errBufferedConnPeekResidual`，Copy 不得 splice-over-peek。
- `TestFindWithUpstreamSkipsNetConn`：只实现 `NetConn()` 的包装不能被当成 splice 目标；`TestFindUpstreamStillWalksNetConn` 仍能剥到 raw TCP（JLS）。`TestNewConnSyscallConnSkipsNetConn`：出站 `NewConn` 包一层 `NetConn()` 后 `SyscallConn` 失败；裸 `*net.TCPConn` 仍成功。
- Linux DIRECT 验收（编排者）：`strace -e splice` 命中；tracking 开时 `/connections` upload/download 仍涨。
