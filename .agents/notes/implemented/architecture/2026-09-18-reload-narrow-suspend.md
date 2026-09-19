# Agent Note: 热重载 I/O 出锁与 Suspend 收窄

Status: implemented

## Problem

Clash 热重载必须原地替换包级全局（面板 `PUT /configs`、SIGHUP、CMFA ABI），不能拆整个进程图。上一轮 [perf-parity](./2026-09-17-perf-parity.md) 已把 `runtime.GC()` 移出 `ApplyConfig` 的 `mux`，但 `OnSuspend` 仍在每次 Apply 开头执行，且 `loadProvider`/`Fetcher.Initial`（磁盘/HTTP）仍在持 `mux` 且 tunnel=`Suspend`/`Inner` 时 `wg.Wait`。刷新订阅期间新 TCP 被 `handleTCPConn` 因 `!isHandle` 直接 Close，UDP 被 Drop；Suspend 窗口随最慢 provider RTT 线性拉长。`resolveMetadata` 的 Direct/Global/`CheckPassRule`/`SpecialProxy` 仍无锁读包级 `proxies`，缩短 Suspend 后会暴露 map 并发读写。

## Decision

保持 `ApplyConfig(cfg, force)` 签名、包级 accessor、`force=true` 仍表示「按地址 diff 重建默认端口 listener」。落地四件事：

1. **proxies 快照。** `resolveMetadata` 与 `match()` 一样：`configMux.RLock` 拷 `proxies` map 引用后 Unlock，`DIRECT`/`GLOBAL`/`CheckPassRule`/`SpecialProxy` 都走副本。缩短 Suspend 必须与此同批，否则 Go map 并发读写。
2. **provider I/O 出锁。** `applyMu` 串行化整次 `ApplyConfig`（含 Initial）。`mux` 只围指针交换与 bind：交换完成后 `OnRunning` + `mux.Unlock`，再 `loadProvider`；`updateProfile` 单独短锁。第二次 Apply 在 `applyMu` 上等第一次 Initial 结束，不会交错写包级全局。失败仍只 log，不 fail-fast。Inner 阶段不再借用 Suspend 挡用户流量——`initInnerTcp` 只赋值 tunnel 指针，HTTP vehicle 在 Running 下走 INNER。
3. **Suspend 仅围 bind。** 规则/代理/DNS 纯指针交换不 `OnSuspend`。短 Suspend 只在 `WillRebind*` 为真时包住 `PatchInboundListeners` / `ReCreate*` / `ReCreateTun` / `PatchTunnel`：named inbound `Config().Equal` 变化或 dropOld 关闭、TUN `!Equal(LastTunConf)`、tunnel 差分、以及 `force=true` 且默认端口 RawAddress/配置字符串将变。`force=false` 且端口未变：不 Suspend、不进入 Close 分支。禁止同端口先 Listen 再 Close 旧（EADDRINUSE）。
4. **DNS 深等跳过。** `updateDNS` 比较 nameserver Equal、policy Domain+NS、fake-ip prefix、listen 等；相等则不 `NewResolver`/`NewEnhancer`，因此纯代理/规则重载不 Flush fake-ip、不重建 UDP pool。未做 resolver `atomic.Pointer`（读点已局部拷贝指针；写仍在 mux 内）。未做 reloadQuiet（urltest 门闩留给后续；WSD 拥有 group 文件）。

`isHandle` 仍是 Running 或 (Inner && INNER)。本轮 ApplyConfig 不再进入 Inner。

## Alternatives considered

- **Box Close+New（抄 `StartOrReloadService`）** — 最强论据是无半新半旧、实现简单。否决：Clash 用户预期「重载配置不断线」；整图 Close 会拆 TUN/混合端口，比现在更差。包级全局是面板/CMFA ABI，不是疏忽。
- **同端口先 Listen 新再 Close 旧** — 最强论据是零 bind 空洞。否决：Clash 单端口无 SO_REUSEPORT 约定，会 EADDRINUSE。地址不变 skip 已给出零空洞。
- **FakeIP 改 xsync.Map / 去掉 LRU** — 最强论据是无全局 `Pool.mux`。否决（本轮）：Persistence/cachefile 与 size 淘汰是 Clash fake-ip 语义；本笔记不碰 FakeIP 锁（WSB 负责拆锁）。
- **把 pause.Manager 经 ctx 传到 outbound** — 最强论据是与 sing urltest/WG 同源。否决：ctx 穿透是已否决 DI 的薄切片。需要时用包级门闩，本轮未做 reloadQuiet。

## Consequences

- **收益**：规则-only / 代理-only `PUT force=false` 不 Suspend，在途 TCP 不被 `!isHandle` Close，监听 fd 不被 unbind；10 个慢 HTTP provider 的 Suspend 窗口不再随 RTT 增长；proxies 热路径不再与 `UpdateProxies` 竞态；DNS 未变时不重建 resolver/enhancer。
- **代价与已知上限**：Initial 在 Running 下跑，healthcheck 可能立刻拨号（可接受；reloadQuiet 未做）。`closeAllConnections` 仍在 `proxySetProvider.Initial` 里按 provider 名关连接——同名 provider 内容刷新仍会关走该 provider 链上的在途连接，这是订阅更新语义，不是 Suspend。DNS 相等比较对 Matcher 走 Stringer/Payload/Foreach，未知 Matcher 类型视为不等并重建（偏保守）。TUN Equal 算法未改。

## Verification

- `go test ./hub/executor/ -count=1 -run 'TestApplyConfig'`（慢 provider 不持 mux；规则-only 不 Suspend）。
- `go test ./tunnel/ -count=1 -run TestResolveMetadata`（若存在）。orchestrator 合入后跑规则-only PUT + `-race`。
