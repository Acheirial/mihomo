# Agent Note: 热重载关闭陈旧 Provider 与 GOMemoryLimit 卸限

Status: implemented

## Problem

`Provider` 实现（`ProxySetProvider` 的 Fetcher 拉取循环 / healthCheck、`RuleSetProvider` 的 Fetcher）靠 finalizer 做兜底 Close。热重载 `UpdateProxies` / `UpdateRules` 整表替换包级 map 后，被换下的对象只靠 GC 才停 goroutine 与 watcher，订阅刷新期间堆积。`Shutdown` 在 `listener.Cleanup` 之后同样不关当前 provider。

`experimental.go-memory-limit` 在 [inbound-stack Workstream 3](./2026-09-18-inbound-stack-and-perf-completion.md) 落地为「`>0` 才 `SetMemoryLimit`，`0` 不调用」。进程一旦设过上限，后续重载把该项改回 `0` 卸不掉，runtime 软限钉在旧值。

## Decision

`constant/provider.Provider` 增加 `Close() error`。`ProxyProvider` / `RuleProvider` 自动带上。实现侧：代理 `ProxySetProvider` / `InlineProvider` / `CompatibleProvider` 与规则 `RuleSetProvider` 原本已有 Close（finalizer + Fetcher / healthCheck）；规则 `InlineProvider` 补 no-op Close（无 Fetcher、无 healthCheck）。

`tunnel.UpdateProxies` / `UpdateRules` 在 `configMux` 下快照旧 map、换指针、解锁，再对旧表里每一个「新表 value 中不存在同一 interface 值」的对象调用 `Close`。比较的是指针身份（`any(old)==any(cur)` 扫新表全部 value），不是名字：同对象换 key 或二次 Apply 复用同一指针不得 Close。Close 在锁外；错误 `log.Warnln("[Provider] close %s error: %s", name, err)`。

`executor.Shutdown` 在 `listener.Cleanup` 之后、`tproxy.CleanupTProxyIPTables` 之前，遍历 `tunnel.Providers()` 与 `tunnel.RuleProviders()` 全部 Close。

`updateExperimental`：`GOMemoryLimit > 0` 仍是 `SetMemoryLimit(MiB<<20)`；`== 0` 改为 `SetMemoryLimit(math.MaxInt64)`，这是 Go 文档里卸掉软限的写法。`GOGCPercent == 0` 仍不调用 `SetGCPercent`（负值是合法 GOGC）。

这不把 provider 改成 Box 式生命周期，也不在 `configMux` 里做 Close。

## Alternatives considered

- **继续只靠 finalizer** — 最强论据是现有 `runtime.SetFinalizer(wrapper, Close)` 已经能停 Fetcher。否决：finalizer 触发时机不确定，热重载高频替换时拉取循环与 healthCheck 会在新旧对象上并行跑到下一次 GC。
- **按名字 Close 旧 key** — 最强论据是实现更短。否决：同名新对象是订阅刷新的常态，必须关旧；但解析层会把同一指针再塞进新 map（Compatible / 未变的 inline），按名字会把仍在用的对象关掉。
- **Close 放在 `configMux` 内** — 最强论据是与交换原子。否决：Fetcher.Close 取消 ctx、停 watcher，不能占住热路径读锁窗口；与 [reload-narrow-suspend](./2026-09-18-reload-narrow-suspend.md) 的「I/O 出锁」同一条线。
- **`GOMemoryLimit == 0` 继续不调用** — 最强论据是保持「默认不碰 runtime」的冷启动语义。否决：冷启动 `0` 与「曾经设过再卸」不可区分；`SetMemoryLimit(MaxInt64)` 对从未设过的进程是 no-op（初始值就是 MaxInt64），对热重载卸限是唯一可观察的正确行为。

## Consequences

- **收益**：被换下的 HTTP/File provider 立即停拉取与 healthCheck；进程退出不再把 Fetcher 交给 finalizer；`go-memory-limit: 0` 的重载真正卸掉软限。
- **代价与已知上限**：Close 与新表 Initial 无屏障——`applyMu` 仍串行化整次 Apply，所以不会一边 Initial 一边 Close 同一指针。不同指针的同名 provider：旧 Close 与新 Initial 先后发生，healthCheck 拨号可能与旧连接关闭交错，这是订阅替换语义。`GOGCPercent` 卸限仍未做（`SetGCPercent(-1)` 会关掉 GC，不能当 0 的语义）。

## Verification

- `go test ./hub/executor/ -count=1 -run 'TestApplyConfig|TestGOMemoryLimitZeroUnsets'`：陈旧 provider Close 一次；复用指针不 Close；`GOMemoryLimit=0` 后 `SetMemoryLimit` 读回 `math.MaxInt64`。
- `go test ./adapter/outboundgroup/ -count=1 -run 'TestSelector|TestURLTest|TestGetProxies'`：`staticProvider` 补 Close 后仍编译通过。
