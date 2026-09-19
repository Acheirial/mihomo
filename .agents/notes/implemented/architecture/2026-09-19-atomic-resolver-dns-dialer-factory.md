# Agent Note: Atomic resolver globals + DNSDialer factory inversion

Status: implemented

## Problem

`resolver.DefaultResolver`, `ProxyServerHostResolver`, `DirectHostResolver`, `DefaultHostMapper`, `DefaultService`, and `DefaultHosts` were bare package variables. Reload (`executor.updateDNS` / `updateHosts`) wrote them while lookups on other goroutines read the same word. For the interface globals that is a data race on the interface word: a torn read can panic or observe a half-written (type, data) pair. For `DefaultHosts` it is a data race on the struct (embedded trie pointer) vs `Search`.

Separately, `dns` imported `tunnel` solely to alias `tunnel.DNSDialer` / `NewDNSDialer`. That inverted the layering: DNS clients live below the tunnel, but constructing a nameserver dialer pulled the whole tunnel graph into `dns`. [2026-09-16-maintainability-pass](../simplification/2026-09-16-maintainability-pass.md) lifted `DnsRespectRules` into `constant` and left this import as explicit leftover.

## Decision

The reloadable resolver globals are `atomic.TypedValue[T]` (`Store` / `Load`). `SystemResolver` stays a plain `Resolver` because it is assigned once at dns init. For interface `T`, `Store(nil)` then `Load()` is nil, so disable still clears the slot. Callers that passed the global as a `Resolver` now `.Load()` first. Writes go only through `.Store(...)`; there is no remaining `resolver.DefaultResolver =` except comments and `net.DefaultResolver` (stdlib). `DefaultHosts` is `atomic.TypedValue[Hosts]` initialized with `atomic.NewTypedValue(NewHosts(trie.New[HostValue]()))`; `updateHosts` `Store`s `NewHosts(tree)`; every `Search` goes through `Load()`. `Hosts.Search` is a value receiver because `Hosts` only holds a trie pointer, so `Load().Search(...)` is addressable without `TypedValue[*Hosts]`.

`dns` no longer imports `tunnel`. It owns a small `Dialer` interface (DialContext + ListenPacket) and a `DialerFactory`. `SetDialerFactory` installs the constructor. The package-level `newDNSDialer` is `atomic.TypedValue[DialerFactory]` (defaults to a direct `component/dialer` path so `dns.init` can construct clients before `ApplyConfig`). Proxy / `respect-rules` nameservers still panic until `SetDialerFactory` installs `tunnel.NewDNSDialer`. `hub/executor.ApplyConfig` registers that factory before `updateDNS`. UDP/DoH/DoQ/DoT constructors call `newDNSDialer.Load()(...)`; their `dialer` fields are the `Dialer` interface, not `*tunnel.DNSDialer`.

This does not Box-DI the rest of the stack and does not change FakeIP geometry or Resolver/Enhancer/Service method sets.

## Alternatives considered

- **Keep bare interface globals, document “reload is exclusive”** — cheapest. Rejected: `updateDNS` already runs under `mux`, but lookups are not on that lock. The race is real, not theoretical; `atomic.TypedValue` already exists in-tree for exactly this shape (`nat.writeBackProxy`).
- **Box / constructor injection of Resolver** — would delete the globals. Rejected: every outbound, dialer, listener, and route handler reads the package-level default; a Box cut is a different architecture and was explicitly out of scope.
- **`dns` import `tunnel` forever, only atomic-wrap the globals** — smaller diff. Rejected: the remaining `dns→tunnel` edge was already named leftover; a factory at the one package that already imports both (`hub/executor`) is the minimum inversion that actually breaks the cycle.
- **Move `DNSDialer` into `dns`** — would drop the factory. Rejected: `DNSDialer` calls `tunnel.resolveMetadata` and proxy adapters; moving it into `dns` just relocates the cycle.

## Consequences

- **收益**：reload vs lookup no longer data-races the interface pointer or the hosts trie pointer; `dns` compiles without `tunnel`; `dns.init` 直连默认工厂能建系统解析器，proxy / respect-rules 在 `SetDialerFactory` 之前仍会 panic。
- **代价与已知上限**：every read of the six globals must `.Load()`. A test that assigned `resolver.DefaultResolver = x` or `resolver.DefaultHosts = x` must `Store`. Direct-only nameservers work before `ApplyConfig`; proxy / `respect-rules` clients still panic until `dns.SetDialerFactory`. `SetDialerFactory` Stores; construction sites Load. A nil factory still panics in `SetDialerFactory`.

## Verification

`go list ./component/resolver/ ./dns/ ./hub/executor/ ./tunnel/` typechecks. `go test -c ./component/resolver/` succeeds. `grep github.com/metacubex/mihomo/tunnel dns/*.go` is empty. Remaining `DefaultResolver =` hits are `net.DefaultResolver` in `component/easytier/platform_test.go`. There is no remaining `DefaultHosts.Search` without `Load()`.
