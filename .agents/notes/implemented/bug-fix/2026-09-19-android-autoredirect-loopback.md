# Agent Note: Android AutoRedirect listens on 127.0.0.1 and ::1

Status: implemented

## Problem

`8db4b44` enabled Android IPv6 AutoRedirect (`ip6tables` + `enableIPv6`) and changed the redirect server listen from hardcoded `127.0.0.1` to `::` / `0.0.0.0`. iptables `OUTPUT -p tcp -o <tun> -j REDIRECT --to-ports` rewrites the destination to localhost (`127.0.0.1` / `::1`), not to an unspecified address. A `tcp6` listener with default `IPV6_V6ONLY` does not accept that IPv4 REDIRECT, so every local IPv4 TCP connection through AutoRedirect failed. IPv6-only listen on `::` also never received IPv4 `SO_ORIGINAL_DST`.

## Decision

Android keeps the IPv4 redirect server on `127.0.0.1:ephemeral`. When `Inet6Address` is set and `/system/bin/ip6tables` exists, a second server binds `::1` on the **same port**. Linux (nftables/iptables) still listens on `::` or `0.0.0.0` as before.

Missing `ip6tables` or a failed `::1` bind / IPv6 nat table setup turns `enableIPv6` off and logs, instead of failing the whole `Start`. `Close` closes both servers.

sing-tun replace is `github.com/Acheirial/sing-tun v0.0.0-20260919145333-e71268e4e6ac` (`e71268e`, branch `go-stack`). That commit also cherry-picks metacubex/meta `8c8d293` / `af345a7` / `54cba48` (gVisor handshake watcher, system TCP NAT checksum, mipstack input batching) and drops the duplicate `rewriteIPv4TCP`/`rewriteIPv6TCP` that `af345a7` added on top of go-stack's `stack_rewrite.go`.

## Alternatives considered

- **Keep a single `::` listener and disable `IPV6_V6ONLY`** — one socket, dual-stack. Rejected: Android still REDIRECTs to `127.0.0.1`, which a `::` bind does not own; mapped `::ffff:127.0.0.1` is not what iptables writes.
- **Listen on `0.0.0.0` / `::` (the 8db4b44 choice)** — covers LAN-forwarded REDIRECT. Rejected for Android: local OUTPUT never hits those addresses; exposing the redirect port off-loopback is unnecessary.
- **Fail Start when IPv6 setup fails** — makes IPv6 misconfig visible. Rejected: Android IPv4 AutoRedirect must keep working when ip6tables/nat6 is absent.

## Consequences

- **收益**：Android IPv4 AutoRedirect works again; IPv6 is extra, not a requirement. IPv6 nat failures no longer take down IPv4.
- **代价与已知上限**：two loopback sockets on Android dual-stack. `::1` bind failure silently disables IPv6 redirect (logged). Hotspot/tethering still needs VPNHotspot; this does not add PREROUTING on Android (iptables path still returns after OUTPUT, same as upstream).

## Verification

`go test -c .` and `go test -tags with_gvisor -run TestMip` in `/home/dev/sing-tun-acheirial`. `Start` uses `127.0.0.1` on Android and optionally `::1` on the same port. `setupIPTables` IPv6 errors degrade when IPv4 is already up.
