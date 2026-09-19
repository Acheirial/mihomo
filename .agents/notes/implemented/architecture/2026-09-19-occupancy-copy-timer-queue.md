# Agent Note: copy buffer reuse, dropConn timer, HealthCheck snapshot, xhttp/TUIC caps

Status: implemented

## Problem

Four bounded occupancy holes survived the 64KiB `PutBuffer` drop and DNS idle-pool work:

1. `copyPooledIncrease` `Get`/`Put` on every Read of a 32KiB (then 64KiB) relay buffer.
2. `dropConn.Read`/`ReadBuffer` used `time.After(DefaultDropTime)` (60s). `Close` unblocks the select but the timer still fires.
3. `HealthCheck.setProxies` wrote `proxies` unlocked; `check`/`execute` ranged `proxies` and `extra` while `registerHealthCheckTask` mutated `extra` under `mu`.
4. xhttp `UploadQueue` waited while `len > maxPackets` (so max+1 entries) and `Close` kept payloads. TUIC v5 `deFragger` LRU had age 10s and no size, so incomplete `PKT_ID`s accumulated.

## Decision

`copyPooledIncrease` Gets once for the current size and Puts+Gets only when growing from `RelayBufferSize` to 65535 after `copyIncreaseThreshold`. `defer Put` of the live buf.

`dropConn` uses `time.NewTimer` + `defer Stop`. `Close` still closes `closeCh`.

`HealthCheck.setProxies` locks `mu`. `check` copies `proxies` and clones `extra` (including `filters`) under `mu`, then unlocks before `URLTest`. `execute` iterates the snapshots.

`UploadQueue.Push` waits while `len >= maxPackets` and rejects `Seq > nextSeq+maxPackets`. `Close` `clear`s the map, nils `buf`/`reader`. TUIC `deFragger.init` adds `lru.WithSize(256)` next to `WithAge(10)`.

## Alternatives considered

- **sing `buf.Copy` / splice-only, drop pooled increase** — splice already covers TCP syscall.Conn. Rejected: TLS/mux still need the userspace copy; reuse is the occupancy fix, not a protocol change.
- **`time.Sleep` in dropConn instead of a timer** — Close already interrupts via `closeCh`. Rejected: Sleep is not selectable against Close; the previous Close+After mix was the leak.
- **Hold `hc.mu` across URLTest** — no snapshot. Rejected: health checks are slow; lock would stall `setProxies`/register on reload.
- **xhttp Close without clear** — GC of Conn would drop payloads. Rejected: Conn holds the reader after Close; 31×1MiB stays until that GC.

## Consequences

- **收益**：steady-state TCP copy holds one pooled buf per direction until the 512KiB bump; closed REJECT-DROP no longer parks a 60s timer; healthcheck has no unlocked slice/map race; xhttp packet-up sessions cap at `maxPackets` and drop payloads on Close; TUIC defrag is size-capped.
- **代价与已知上限**：copy still jumps 32KiB→64KiB after 512KiB with a Put+Get (one extra alloc per long stream). HealthCheck snapshot clones filter maps once per check interval. Seq-gap reject can tear down a lossy xhttp session (same as `ErrQueueTooLarge` already did at capacity). TUIC size 256 is a bound, not a correctness proof for reordering beyond that.

## Verification

`go test ./common/net -run 'TestCopy|TestRelay'` (includes `TestCopyPooledIncreaseReusesBuffer`). `go test ./transport/xhttp -run TestUploadQueue`. `go test ./transport/tuic/v5 -run TestDeFragger`. `dropConn` Read/ReadBuffer have `defer timer.Stop()`. `HealthCheck.check` copies under `mu`.
