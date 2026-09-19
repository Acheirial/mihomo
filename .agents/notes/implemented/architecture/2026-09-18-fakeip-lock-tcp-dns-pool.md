# Agent Note: FakeIP lock split + TCP DNS idle pool + Copy cuts

Status: implemented

## Problem

FakeIP `LookBack`/`Exist` shared `Pool.mux` with allocation, so TUN/redir reverse lookup queued behind `Lookup`. TCP nameservers dialed per query. FakeIP/hosts middleware and DNS cache copied whole `miekg/dns.Msg` trees on the hot path. DNS INNER dials joined `/connections` via `statistic.New*Tracker`.

This supersedes the “leave FakeIP mutex and `D.Msg.Copy` alone” slice of [2026-09-17-perf-parity](./2026-09-17-perf-parity.md). UDP `udpConnPool` and the ExchangeContext leak fix in [2026-09-18-inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) stay.

## Decision

`allocMu` protects only `offset`/`cycle` and the allocate path in `Lookup`/`get`/`FlushFakeIP`/`StoreState`. `LookBack`/`Exist` talk only to `store`. `get` `PutByIP`s before publishing the new offset so a concurrent `LookBack` cannot see a recycled IP with no host. Geometry is unchanged: `first = gateway.Next().Next().Next()` (`.4`), gateway and broadcast excluded.

`memoryStore.GetByIP` uses LRU `Get` (MoveToBack) and does not touch `cacheIP`. Twin-map OnEvict and why Peek was reverted: [fakeip memoryStore LRU coupling](../bug-fix/2026-09-19-fakeip-memory-lru-evict.md). `GetByHost` still heats both maps on Lookup so LRU eviction stays host-aligned.

`udpConnPool` is now `idleConnPool`: UDP stays single-slot (`max=1`, 30s idle); TCP is LIFO ≤8. `client.ExchangeContext` Acquire/Release for both schemas. Truncated UDP→TCP uses a per-client `tcpIdle` pool. Error and ctx cancel `Release(false)` close the conn. No pipelining.

FakeIP and hosts hit paths `new(Msg)` + `SetReply`. Cache Copies on get (and on put only if Extra contains OPT). DoH/DoQ set Id=0 via header shallow copy. ECS Copies only when Extra already has OPT. `resolver.go` singleflight shared branch still Copies.

`tunnel/dns_dialer.go` no longer wraps `statistic.NewTCPTracker`/`NewUDPTracker`. `respect-rules` still calls `resolveMetadata`. `dns` no longer imports `tunnel`; the factory inversion is in [atomic resolver + DNSDialer factory](./2026-09-19-atomic-resolver-dns-dialer-factory.md).

## Alternatives considered

- **sing-box `queryMultiplexer` + reuse probe** — one primitive for UDP/TCP/DoT pipelining. Rejected: probe/epoch/demote is the wrong cost for Clash multi-NS race; DoT pipelining breaks one-query servers; IdleConnectionKeeper needs Box lifecycle. Idle pool first.
- **sing-box `MemoryStorage` async store** — RLock on LookBack. Rejected: async Put lets LookBack miss immediately after Lookup (`fake DNS record missing`). Mapping must be synchronously visible.
- **default sequential nameserver** — kills goroutine fan-out. Rejected: YAML `nameserver:` is a race; must stay opt-in.
- **default-off connection tracking** — DNS would vanish from `/connections` globally. Rejected: panel open-box; only DNS INNER skips trackers.

## Consequences

- **收益**：LookBack no longer waits on allocMu; two sequential `tcp://` exchanges share a handshake; FakeIP A/AAAA answers allocate a reply header instead of cloning the query tree; DNS INNER sockets stay off `/connections`.
- **代价与已知上限**：UDP is still one chair, concurrent queries extra-dial. Cache put without Copy means a caller that mutates the *upstream* msg after `putMsgToCache` can corrupt the cache — only get returns a copy. Truncated TCP retry is per-client, not shared across UDP clients. Persistence offset/cycle keys unchanged.

## Verification

`GOTMPDIR=/home/dev/tmp go test ./dns/ ./component/fakeip/ -count=1 -timeout 120s`. `TestPool_LookBackDoesNotTakeAllocMu` blocks Lookup on store.GetByHost while LookBack returns. `TestTCPConnPoolReuse` asserts two TCP exchanges dial once. `TestPool_Basic` still starts at `.4`.
