# Agent Note: 入站 BufferedConn unwrap、Peek 默认关与 tproxy UDP writeback 复用

Status: implemented

## Problem

Linux 上 sing v0.5.7 的 `bufio.Copy` 已能对双端 `syscall.Conn` 走 `copyDirect`/`unix.Splice`，但入站热路径把每条 TCP 包进 `BufferedConn` 且不实现 `SyscallConn`。`UnwrapCountReader` 停在不可 replace 的包装上之后，源端做 `source.(syscall.Conn)` 失败，DIRECT/tproxy/redir 满速仍走用户态 Read/Write。

同时 `handleTCPConn` 在嗅探默认关闭时仍为每条连接起 200ms Peek goroutine，只为 NeedHandshake 出站缓存首字节；`NewConnContext` 无条件 `NewUUIDV4()`，tracking 关闭也付这笔分配。tproxy UDP `WriteBack` 在 NAT miss 时 `dialUDP` 后再挂一条 `Read` 循环，一流一 goroutine。上一轮 [perf-parity](./2026-09-17-perf-parity.md) 明确本轮不做 splice；[inbound-stack 收尾](./2026-09-18-inbound-stack-and-perf-completion.md) 否掉了用 `bufio.CopyConn` 包 `Relay`。本篇只补包装与 Peek，让 A 的单向 `bufio.Copy` 能看到 raw socket。

## Decision

入站仍走 `C.ConnContext.Conn() *N.BufferedConn`。splice 不靠跳过包装，靠缓存排空后的 `SyscallConn`。

- **`BufferedConn.SyscallConn`**：仅当 `ReaderReplaceable()` 为真（`r==nil` 或 `Buffered()==0`）才转发到内层 `syscall.Conn`（`ExtendedConn` 自身或 `Upstream()`）。有残留 Peek 时返回错误，禁止 splice 越过未消费缓存。写侧始终 `WriterReplaceable()==true`。
- **`NewConnContext`**：继续 `N.NewBufferedConn(conn)`；`id` 初始为 `uuid.Nil`，`ID()` 用 `sync.Once` 生成一次。面板 Join 前调用 `ID()` 得到非空值。
- **Peek goroutine**：只在 `sniffingEnable && snifferDispatcher.Enable() && !conn.Peeked()` 时启动 200ms `Peek(1)`。MITM / `TCPSniff` 路径不变。`NeedHandshake(remoteConn)` 在 Dial 之后、缓存为空时同步 `Peek(1)`（200ms deadline）再写出站；写出 `n>0` 且失败时走 `errIfHandshakeWritten`，避免已写出站后再重拨。无嗅探且非 early-handshake 时不启 Peek goroutine。`peekMutex` 仍串 NeedHandshake 写出与停未完成 Peek。
- **mixed/socks/http**：mixed `Peek(1)` 后把同一个 `*BufferedConn` 交给 socks 或 HTTP。`NewBufferedConn` 对已有 `*BufferedConn` 直接返回，HTTP 不再套第二层 reader。redir/tproxy/tunnel TCP 本来就不预包，经 `HandleTCPConn` → `NewConnContext` 包一次。
- **tproxy UDP 0b**：`WriteBack` 仍按 `(lAddr, rAddr)` 从 `NatTable.GetForLocalConn` 复用 `*net.UDPConn`；miss 时只 `dialUDP`（Linux 上 `IP_TRANSPARENT` bind+connect），不再为每个流起 `listenLocalConn` 读循环。后续入站包走主 tproxy UDP listener。不 bump sing-tun、不引入 `UDPNat`。

## Alternatives considered

- **入站完全跳过 BufferedConn，让 tproxy/redir 直传 `*net.TCPConn`** — 最强论据是内核路径零 bufio、splice 无需 unwrap。否决：`C.ConnContext.Conn()` 的导出类型是 `*N.BufferedConn`；mixed 首字节 demux 与 sniff/MITM/NeedHandshake 都要同一条 Peek 缓存。splice 经 `SyscallConn` + `ReadCached` 排空即可，不必改导出类型。
- **升级 sing-tun 使用 `UDPNat`/`TProxyWriteBack`** — 最强论据是与 sing-box tproxy 同源、writeback 与入站 NAT 一套。否决：本阶段钉死 `sing-tun v0.4.24` 且该版本无 `UDPNat`；本地 NAT map + `dialUDP` 已复用 writeback socket，升 pin 留给工具链窗口。
- **删除 ConnContext UUID** — 最强论据是 tracking 关闭时分配完全浪费。否决：Clash 面板 `/connections` 依赖非空 id；tracking 默认开。改成懒分配，Join 前第一次 `ID()` 才生成。

## Consequences

- **收益**：嗅探关闭且远端非 NeedHandshake 时不再每 TCP 一条 Peek goroutine；DIRECT 在 A 接上单向 `bufio.Copy` 后，入站缓存排空即可成为 `syscall.Conn`。mixed 首字节仍只 Peek 一次。tproxy UDP 流数不再线性带读 goroutine。`ID()` 在 Join 前非空。
- **代价与已知上限**：ConnContext 仍为每条 TCP 分配默认 4KiB `bufio.Reader`，直到 `ReadCached` 把 `r` 置 nil。tproxy writeback socket 改为只写；若某内核把后续客户端包 demux 到这条 connected socket 而不是主 TPROXY listener，那些包不会再被 `listenLocalConn` 回灌——信号是多包 UDP 会话只有首包。那时应改成 unconnected `WriteTo` 或恢复有界读循环，而不是升 sing-tun。NeedHandshake 出站仍依赖 Dial 后的 Peek，不能把握手写出再提前删掉。

## Verification

- `go test ./common/net -count=1 -run 'TestBufferedConnSyscallConn|TestNewBufferedConnReusesExisting'`：残留 Peek 时 `SyscallConn` 报 `errBufferedConnPeekResidual`；Discard 后对 `*net.TCPConn` 转发成功；`NewBufferedConn` 对已有包装是同一指针。
- `go test ./context -count=1 -run 'TestConnContext'`：构造时 `id==uuid.Nil`；`ID()` 非空且并发调用得到同一值；`Conn()` 非 nil `*BufferedConn`。
- mixed 首字节 socks5 vs HTTP 仍走 `handleConn` 的同一 `Peek(1)`；HTTP `HandleConn` 注释标明 reuse。
- `TestRelayClosesOnEOFPeer` 仍由 A 守 Relay 任一端 EOF 双关；本篇不改 `sing.go`。
