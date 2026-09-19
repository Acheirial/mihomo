# Agent Note: FakeIP memoryStore LRU twin eviction

Status: implemented

## Problem

`memoryStore` keeps two same-sized LRUs (`cacheIP` host→ip, `cacheHost` ip→host) with no OnEvict coupling. `GetByIP` used `Peek`, so `LookBack` did not refresh `cacheHost`. After enough inserts, `cacheHost` can drop ip→host while `cacheIP` still has host→ip: `Exist(ip)==false` and `get()` reallocates that IP to a new host, but `GetByHost` of the old host still returns the stolen IP.

This is a mapping-consistency hole left by [FakeIP lock split](../architecture/2026-09-18-fakeip-lock-tcp-dns-pool.md), which chose Peek so LookBack would not take the opposite LRU lock.

## Decision

`newMemoryStore` builds `cacheHost` then `cacheIP`. Each `lru.WithEvict` `Delete`s the twin key on the other map. OnEvict runs under that LRU's mutex, so the callback never calls `DelByIP` (that re-locks `cacheHost` while already inside `cacheHost.OnEvict`). Nil other-cache is guarded because the second map does not exist until after the first `New`.

`Delete` on the twin also fires *its* OnEvict, which would `Delete` back into the LRU whose lock is still held — same-goroutine deadlock. `inEvict` CAS skips that nested callback. Pool `Set`/`Delete` already run under `allocMu`, so two goroutines cannot OnEvict at once through `Pool`; the flag only breaks the nested same-stack `Delete`.

`GetByIP` uses `cacheHost.Get` (MoveToBack) so LookBack keeps the reverse mapping alive. It still does not `cacheIP.Get` — that recouples LookBack onto the Lookup/evict lock.

## Alternatives considered

- **`GetByIP` also `cacheIP.Get`** — heats both ranks on LookBack, eviction stays aligned without OnEvict. Rejected: that is the lock coupling the 2026-09-18 split removed; LookBack would take `cacheIP.mu` on every TUN reverse lookup.
- **Keep Peek, only add OnEvict** — twins stay consistent on overflow, but a hot LookBack IP is still the next `cacheHost` eviction, so a live connection loses its reverse mapping while `cacheIP` still serves the host. Rejected: LookBack must MoveToBack `cacheHost`.
- **One mutex around both maps** — trivial consistency. Rejected: LookBack would share that mutex with Lookup Sets; the lock split exists to avoid that.

## Consequences

- **收益**：evicting either LRU drops the twin; LookBack of a live IP keeps ip→host; `GetByHost` cannot return an IP `Exist` says is free.
- **代价与已知上限**：LookBack takes `cacheHost.mu` with MoveToBack (not a second map). Nested OnEvict is skipped, which is the already-deleted pair. `Clear`/`CloneTo` do not fire OnEvict; both maps are written together. Persistence `cachefileStore` is unchanged.

## Verification

`GOTMPDIR=/home/dev/tmp CGO_ENABLED=0 go test ./component/fakeip/ -count=1 -timeout 60s`. `TestMemoryStore_EvictDeletesTwin` overflows each side alone. `TestPool_LookBackKeepsReverseMapping` heats via LookBack then asserts the IP is not stolen. `TestPool_DoubleMapping` still drops the cold host.
