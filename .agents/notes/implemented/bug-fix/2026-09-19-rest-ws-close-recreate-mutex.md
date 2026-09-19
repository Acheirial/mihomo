# Agent Note: REST WS conn close + ReCreateServer pointer mutex

Status: implemented

## Problem

REST API WebSocket handlers hijack the HTTP conn and then return without `Close`. `getConnections` also feeds a query `interval` into `time.NewTicker` with no floor: `interval<=0` panics (or, on some stdlib paths, spins). Concurrent `ReCreateServer` launches `start`/`startTLS`/`startUnix`/`startPipe` as overlapping goroutines that read/write the same `httpServer`/`tlsServer`/`unixServer`/`pipeServer` pointers without a lock, so two recreates can `Serve` the same `*http.Server` after the previous `Close` has already run.

## Decision

`getConnections` defers `conn.Close()` after a successful upgrade and clamps a parsed interval `<=0` to 1000 ms. `traffic`/`memory`/`getLogs` do the same `defer wsConn.Close()` after upgrade (they tick at 1s and have no interval query). JSON snapshot shape is unchanged.

One `serverMu` serializes the four REST `*http.Server` slots via `replaceServer`. Each `start*` builds the new `*http.Server` (or `nil` on empty addr), swaps under the mutex, unlocks, `Close`s the old pointer if any, then `ListenAndServe`s the new one. The mutex is not held across `Serve`. Empty addr still closes the old server and returns.

This is the REST controller in `hub/route`, not `dns.ReCreateServer` / inbound listeners.

## Alternatives considered

- **Per-slot mutex instead of one `serverMu`** — four recreates are independent and a per-slot lock would let HTTP and TLS swap in parallel. Rejected: the race is two goroutines on the *same* slot; one mutex is smaller and the four `go start*` already run in parallel for Listen.
- **Keep Close-then-nil-then-Listen as today, only add the mutex around the old block** — cheapest sync. Rejected: the assigned order is build → lock → save old → assign new → unlock → `old.Close()` → Serve, so a second recreate cannot `Serve` a pointer that another goroutine already closed.
- **SO_REUSEPORT / listen-new-then-close-old** — zero bind gap. Rejected in [reload-narrow-suspend](../architecture/2026-09-18-reload-narrow-suspend.md): Clash has no SO_REUSEPORT convention on this port; same-addr listen while old is up is EADDRINUSE. Close still happens before Listen on the same addr; the mutex only orders the pointer.

## Consequences

- **收益**：hijacked WS conns are closed when the handler exits; `interval<=0` no longer panics; two `ReCreateServer` calls cannot double-`Serve` one `*http.Server`.
- **代价与已知上限**：same-addr recreate still has a Close→Listen hole (accepted; not SO_REUSEPORT). Listen error after swap leaves the new `*http.Server` in the slot but not serving — next recreate Closes it. `replaceServer` is REST-controller only.

## Verification

Scoped smoke in `/tmp` (not a suite): WS upgrade with `interval=0` and `interval=-1` must not panic and must close the hijacked conn; concurrent `start` on `127.0.0.1:0` must Close the previous `*http.Server` before a second `Serve` on that pointer.
