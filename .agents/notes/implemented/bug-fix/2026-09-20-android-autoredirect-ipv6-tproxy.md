# Agent Note: Android AutoRedirect IPv6 uses TPROXY

Status: implemented

## Problem

`e71268e` 在 Android 上为 IPv6 加了 `::1` 监听，并把 `ip6tables -t nat OUTPUT REDIRECT` 当成和 IPv4 对称的路径。Android 内核普遍没有 IPv6 nat 表：`ip6tables -t nat -N` 失败后 `Start` 把 `enableIPv6` 关掉，IPv6 TCP 继续进 TUN 栈。有 nat6 时 REDIRECT 也只改写到 `::1`，不是 sagernet 用来接本地 IPv6 的机制。IPv4 已经能进 Redir；IPv6 还在走 TUN。

## Decision

Android IPv4 仍走 `nat OUTPUT REDIRECT` → `127.0.0.1:port`，目的地址用 `SO_ORIGINAL_DST`。

Android IPv6 改 TPROXY，不走 ip6tables NAT：

- 在与 IPv4 相同的端口上听 `[::]`（默认 `IPV6_V6ONLY`，不抢 IPv4）。套接字设 `IPV6_TRANSPARENT` 和 `SO_MARK=AutoRedirectOutputMark`（缺省 `0x2024`）。
- 接受的 IPv6 连接目的地址取 `conn.LocalAddr()`，不用 `GetOriginalDestination`。
- `ip6tables -t mangle OUTPUT`：`-p tcp -o <tun> -j MARK --set-mark <tproxyMark>`。
- `ip6tables -t mangle PREROUTING`：跳过 tun 入站，对其余 `-p tcp -m mark --mark <tproxyMark> -j TPROXY --on-port <port> --tproxy-mark <tproxyMark>`。
- `ip -6 route add local default dev lo table <IPRoute2TableIndex+1>`，`ip -6 rule add fwmark <tproxyMark> lookup <T> priority 1`。`tproxyMark` 缺省 `0x2026`。
实现在 Acheirial `sing-tun` `go-stack`（commit `ba2bfb7`）。mihomo 只 bump `replace` 伪版本，并改 `docs/docs/config/inbound/tun*.md` 的 Android 段。
- 非 Android 的 iptables/nftables IPv6 仍是原来的 NAT/nft REDIRECT。不移植 sagernet 的 nfqueue、bypass 路由、VPNService 规则克隆。

实现在 Acheirial `sing-tun` `go-stack`。mihomo 只 bump `replace` 伪版本，并改 `docs/docs/config/inbound/tun*.md` 的 Android 段。

与 [Android AutoRedirect listens on 127.0.0.1 and ::1](./2026-09-19-android-autoredirect-loopback.md) 部分重叠：那篇仍约束 IPv4 必须听 `127.0.0.1`；IPv6 监听从 `::1` NAT 改成 `[::]` TPROXY，由本篇接管。go 栈 replace 链见 [2A 在 metacubex/sing-tun 补 go 栈与 SpliceSocket](../architecture/2026-09-18-go-stack-sing-tun-2a.md)。

## Alternatives considered

- **继续用 `ip6tables -t nat REDIRECT` 到 `::1`** — 和 IPv4 同一套代码，改动最小。否决：Android 通常没有 IPv6 nat 表；有表时 REDIRECT 也只改写到 loopback，不是透明代理语义。
- **整棵移植 sagernet `ac3845c`（nfqueue + bypass 路由 + VPNService 规则）** — 与上游 Android 行为完全对齐。否决：两千行级改动，还依赖 `HandlerEx`/`JudgeFlow` 契约，会把 metacubex fork 绑死 sagernet auto-redirect 重写。
- **IPv6 永远走 TUN** — 不碰 Android netfilter。否决：auto-redirect 的目的就是把本机 TCP 从 TUN 栈挪到 Redir；IPv4 已经做到，IPv6 再漏回 TUN 就是半套实现。

## Consequences

- **收益**：有 `ip6tables` mangle/TPROXY 的 Android 设备上，本机 IPv6 TCP 进 Redir，不再进 TUN 用户态栈。IPv4 路径不动。TPROXY 失败时 IPv4 仍可用。
- **代价与已知上限**：要内核 TPROXY 和 `ip -6 rule`；没有 mangle/TPROXY 的设备 IPv6 仍走 TUN（打日志）。热点/共享仍要 VPNHotspot；Android iptables 路径仍然没有完整 PREROUTING NAT。`tproxyMark` `0x2026` 若和设备已有 fwmark 冲突必须改 `AutoRedirectTProxyMark`。

## Verification

`GOOS=android GOARCH=arm64 go build` 在 `/home/dev/sing-tun-acheirial`。Android IPv6 安装 mangle MARK/TPROXY 和 `local default` 路由，不再执行 `ip6tables -t nat ... REDIRECT`。IPv4 仍是 `nat OUTPUT REDIRECT` 到 `127.0.0.1`。
