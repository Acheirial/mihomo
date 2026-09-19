# Agent Note: Hysteria reconnect closes old QUIC; faketcp cleaner loops

Status: implemented

## Problem

Hysteria1 `connectToServer` replaced `c.quicSession` / `c.udpSessionMap` without `CloseWithError` on the previous `*quic.Conn`. Outbound keep-alive is 10s with default quic-go idle 30s, so PING keeps the orphan alive. Each reconnect stacked a 15/64 MiB advertised window plus a `handleMessage` goroutine until process exit.

`faketcp.TCPConn.cleaner` took one ticker tick then returned. After that, `lockflow` inserted every source into `flowTable` forever.

## Decision

`connectToServer` `CloseWithError`s a non-nil `c.quicSession` after the new handshake succeeds, then swaps `udpSessionMap` and starts `handleMessage` on the new conn. The old `ReceiveDatagram` loop exits on close. `openStreamWithReconnect` still holds `reconnectMutex` and still calls `connectToServer` on permanent `OpenStream` errors.

`cleaner` is `for { select die → return; ticker → expire flowTable }`. `defer ticker.Stop()`. The existing `expire` constant is unchanged.

## Alternatives considered

- **Close the old conn before dialing the new one** — smallest window of two live sessions. Rejected: a failed handshake would leave the client with no session; the leak is only on successful replace.
- **Let quic-go idle timeout reap orphans** — no code. Rejected: KeepAlivePeriod 10s defeats the 30s idle timeout.
- **Restart cleaner from lockflow when the table is non-empty** — would recover after the one-shot return. Rejected: a loop from start is the actual intent of a reaper goroutine.

## Consequences

- **收益**：reconnect no longer orphans a KeepAlive QUIC session; faketcp flows expire every minute until `die`.
- **代价与已知上限**：a successful reconnect still overlaps old and new conns for the handshake duration. `Client.Close` still closes only the current pointer — that is correct after this swap. Windows/non-linux faketcp files are unchanged (no `cleaner`).

## Verification

`go test ./transport/hysteria/core -count=1`. `connectToServer` has `if c.quicSession != nil { CloseWithError }` before the swap. `tcp_linux.go` `cleaner` is a `for` loop with `defer ticker.Stop()`.
