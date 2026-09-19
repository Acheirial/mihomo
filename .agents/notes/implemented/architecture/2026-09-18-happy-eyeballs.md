# Agent Note: 默认 Happy Eyeballs 交错拨号与组指针缓存

Status: implemented

上一轮 [perf-parity](./2026-09-17-perf-parity.md) 明确本轮不做 splice、不做 Box DI；[inbound-stack 收尾](./2026-09-18-inbound-stack-and-perf-completion.md) 否决了 `CopyConn` 包 `Relay`。本篇不翻转那些决定，只补拨号热路径：默认不再傻等首 IP，组选择不再每拨加锁扫切片。

## Problem

`component/dialer.DialContext` 在 `tcp-concurrent=false`（默认）时对同族地址走 `serialDialContext`：第一个 A 记录若黑洞，整次拨号吃满 `DefaultTCPTimeout`（5s）。`tcp-concurrent=true` 则对全部 IP 同时 `go racer`，无 RFC 8305 间隔。双栈 `dualStackDialContext` 两个族的 racer 在 t=0 一起 SYN，300ms ticker 只决定何时*接受* fallback，不延迟*发起*。

TFO 的 `dialTFO` 用 `context.Background()+DefaultTCPTimeout`，调用方取消后仍占 fd 最多 5s。

组侧 `urltest.fast(true)` / `selector.selectedProxy` 每次 Dial 都 `GetProxies` + 扫名字；`GetProxies` 缓存命中仍互斥锁。Clash 面板 JSON（`now/all/testUrl`）与 `Set`/`ForceSet` 契约不能改。

## Decision

Happy Eyeballs 只发生在最终 `*net.Dialer`（`dialContext` 对非 `*net.Dialer` 的 `NetDialer` 仍一次性 `DialContext`）。**禁止**把 HE 做到 `C.Dialer` / `proxydialer`——那会并行打出多条完整代理握手。

`component/dialer`：

- 抽出 `DialSerial`（逐 IP）与 `DialParallel`（同族交错；`delay==0` 即旧全量竞速）。
- `DialContext`：`opt.tfo` → 只 `DialSerial`；否则双栈走 `dualStackDialContext`（主族立即、备族 `dualStackFallbackTimeout=300ms` 后才 `startRacer`；主族失败则立刻放备族）。`ip-version` 4/6（`opt.network`）仍单族。
- `tcp-concurrent=true`：同族 `DialParallel` delay=0；双栈备族仍延迟，不得在 t=0 SYN。
- `dialTFO`：`context.WithTimeout(ctx, DefaultTCPTimeout)`，禁止 `Background`。CMFA `DefaultSocketHook != nil` 仍忽略 iface/mark/tfo。

组：

- `selector` 缓存当前 `C.Proxy` 指针，`Set`/`ForceSet` 清空；provider version 变化失效。
- `urltest.fast`：`Touch` 与选路分离（`GetProxies(false)` + 单独 `Touch`）；version 变化 Reset `fastSingle`。
- `GroupBase.GetProxies` 缓存命中 `RLock`，重建才 `Lock`。

文档 `tcp-concurrent` 三语写明：false 不再傻等首 IP；不要加 `tcp-concurrent: serial`。

## Alternatives considered

- **默认 `tcp-concurrent=true`** — 最强论据是一行配置就「并行连上」。否决：全量 SYN 不是 Happy Eyeballs，移动网络伤电量/NAT；已有用户靠 false 避竞速。false 改为交错，true 保留 delay=0。
- **把 HE 做到 `C.Dialer` / proxydialer** — 最强论据是组链、detour 一起加速。否决：会并行多条完整 TLS/代理握手，破坏 `dialer-proxy`；近似 Box DI。
- **组改 Box 式 OutboundManager DI** — 最强论据是 `selectedOutbound*` 指针零开销。否决：Clash 组/provider/面板 API 不兼容，与 [perf-parity](./2026-09-17-perf-parity.md) 已否决的 Box DI 同一条红线。
- **照搬 `network_strategy` / 接口类型 fallback** — 最强论据是多 WAN/Android。否决：Clash YAML 无此模型；CMFA 已有 SocketHook。
- **恢复 relay 组** — 最强论据是旧配置迁移。否决：parser 已删，官方路径是 `dialer-proxy`。

## Consequences

- **收益**：同族 [死 IP, 活 IP] 默认在 ~300ms 连上，不再等 5s；备族 t=0 不再 SYN；TFO 取消随 caller ctx；selector/url-test 热路径命中缓存指针，`GetProxies` 命中不再排他锁。
- **代价与已知上限**：默认时序变化——依赖「永远连第一个 A」的环境会改走第二 A 或备族，用 `ip-version` 锁族。TFO 用户不再享受 HE（与 sing-box `!TCPFastOpen` 同权衡）。url-test 指针在 provider 热更新时靠 version 失效；若 version 不涨而切片替换，仍走 `GetProxies` 重建。`tcp-concurrent=true` 仍是同族 SYN 风暴，只是文档化为激进档，不当前默认。
- **未做**：fallback delay 不可配 YAML；不改 `C.Dialer` 签名；不在 proxydialer 层 HE。

## Verification

- `GOTMPDIR=/home/dev/tmp go test ./component/dialer -count=1`
- `GOTMPDIR=/home/dev/tmp go test ./adapter/outboundgroup -count=1 -run 'TestSelector|TestURLTest|TestGetProxies'`
- 死首 IP 测试断言耗时在 delay 量级；双栈测试断言备族 SYN 不在 t=0；TFO 测试取消 caller ctx 后 Write 失败。
