# Agent Note: DNS ExchangeContext timer + DoT cancel + DoQ pool/conn leaks

Status: implemented

## Problem

Four independent leaks in `dns/` survived the earlier ExchangeContext ownership fix in [inbound-stack-and-perf-completion](../architecture/2026-09-18-inbound-stack-and-perf-completion.md):

1. `client.ExchangeContext` waited on `time.After(dnsClientTimeout)` after a successful or cancelled exchange. The timer is not stopped, so every query that finishes early leaves a 5s timer until fire.
2. DoT `ExchangeContext` returns on `ctx.Done()` without closing the in-flight conn. miekg's `ExchangeWithConn` ignores ctx and blocks until its 5s Timeout, so cancelled queries pin a goroutine and a TLS conn.
3. DoQ `exchangeQUIC` does `pool.Get(2+MaxMsgSize)` (65537). The allocator's pooled max is 65536; Get falls to `make` and Put is a no-op.
4. `getConnection` released the write lock before opening. Two concurrent cache misses both `openConnection` and the first conn is overwritten and leaked.

The architecture note [atomic resolver + DNSDialer factory](../architecture/2026-09-19-atomic-resolver-dns-dialer-factory.md) also claimed `newDNSDialer` was already `atomic.TypedValue`; the code was still a plain var until this change.

## Decision

`client.ExchangeContext` uses `time.NewTimer(dnsClientTimeout)` with `defer timer.Stop()` for the bounded wait on `done`.

DoT shares the in-flight conn with the caller via a mutex-guarded `flight` slot. On `ctx.Done()` the caller Closes only that slot. The goroutine `takeFlight`s before Close-on-error or LIFO `PushBack`, so a conn already returned for reuse is never Closed by a late cancel.

DoQ packs into `pool.Get(MaxMsgSize)` (65535, pooled). The 2-byte length prefix is a `[2]byte` written separately; the response length is read into `[2]byte` then `ReadFull` into `buf[:respLen]`. `defer pool.Put` stays.

`getConnection` double-checks `useCached && doq.conn != nil` after taking `connMu.Lock` and returns the winner instead of opening a second conn.

`newDNSDialer` is `atomic.NewTypedValue(DialerFactory(defaultDNSDialer))`. `SetDialerFactory` Stores a non-nil factory; client/DoH/DoQ/DoT construction sites `Load()`.

## Alternatives considered

- **Keep `time.After`** — smallest diff and the wait is already bounded. Rejected: every fast-path query leaks a 5s timer; `NewTimer`+`Stop` is the stdlib pattern for this select.
- **Close every DoT conn on cancel, including one already PushBack'd** — would always unblock miekg. Rejected: the next waiter would inherit a closed pooled conn; only the in-flight conn is abandoned.
- **Bump the allocator past 64KiB so `2+MaxMsgSize` pools** — keeps a single contiguous write. Rejected: 65536 is the pooled ceiling everywhere else; a 2-byte stack header plus `Get(MaxMsgSize)` stays inside the existing 64KiB buckets.
- **singleflight around `openConnection`** — would also collapse concurrent misses. Rejected: the write-lock double-check is the existing mutex and is enough; a new group is extra API for one site.

## Consequences

- **收益**：cancelled UDP/TCP/DoT exchanges no longer pin a 5s timer or an in-flight TLS conn; DoQ pack/unpack returns buffers to the 64KiB pool; two concurrent DoQ misses no longer leak the first QUIC conn; reload vs construct no longer data-races the dialer factory.
- **代价与已知上限**：DoT cancel still races with `PushBack` by design — the loser of `takeFlight` must not Close. `getConnection` still holds the write lock across `openConnection` (dial + QUIC handshake); that was already true of the post-RUnlock Lock path. `PackBuffer` now writes at `buf[0:]` instead of `buf[2:]`; a future length-prefix-in-buffer rewrite must not reintroduce `Get(size>65536)`.

## Verification

No `time.After` in `dns/client.go` `ExchangeContext`. DoT `ctx.Done()` Closes `flight` only. `dns/doq.go` `pool.Get(MaxMsgSize)` only. `getConnection` double-checks under `connMu.Lock`. `newDNSDialer.Load()` at every construction site. Orchestrator runs package tests once after sibling edits land.
