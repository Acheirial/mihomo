# Agent Note: 主规则列表 DOMAIN 段编译（保 first-match）

Status: implemented

关联：[2026-09-17-perf-parity](./2026-09-17-perf-parity.md) 的运行时补齐未覆盖主 `rules:` DOMAIN O(n)；[2026-09-18-inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) 收尾了 splice/入站栈，规则段仍是一行一对象。本篇不翻转那两篇的 Decision（不做 splice、不 `CopyConn`、不默认 GOMEMLIMIT）。

## Problem

Clash YAML 的 `rules:` 是有序列表，第一条命中即停，不同 `DOMAIN` 可以指向不同出站。主列表却是一行一个 `C.Rule`：数千条广告/直连域名等于每条连接数千次 `RuleHost() ==` / `HasSuffix` 加一层 wrapper Miss 写时间戳。RULE-SET 的 domain 策略已经走 `trie.DomainSet`，主列表没有。PROCESS 每次 netlink+/proc，无 TTL。Wrapper `Miss()` 对未命中的前 N-1 条每次 `missAt.Store(time.Now())`。

不做的后果：大规则集每连接匹配停在线性扫描；开 PROCESS 或 `find-process-mode: always` 时短连接重复 syscall。

## Decision

主列表与 `sub-rules` 在 `parseRules` 之后做**连续段**编译，不改 `C.Rule` 导出签名，`tunnel.match` 仍扫 `[]C.Rule`。

- `rules.CompileDomainSpans` 把连续 `DOMAIN` / `DOMAIN-SUFFIX`（长度 ≥ 2）收成内部 `domainSpan`。`KEYWORD` / `REGEX` / `WILDCARD` / `GEOSITE` / 其它类型打断。叶子上的 `RuleWrapper` 保留，hit/miss 仍记在 YAML 那一行。
- DomainSet 编码与 `rules/provider/domain_strategy.go` / `DomainTrie.Insert` 同一套：精确 `DOMAIN` 插 host；`DOMAIN-SUFFIX` 插 `"+."+suffix`（自身 + 子域）。host 不在 set → 整段 miss；在 → **原序**扫叶子，返回第一条命中的 adapter。`DOMAIN a→PROXY` 后 `DOMAIN a→REJECT` 仍 PROXY。
- `RuleType` 为新增的 `C.DomainSpan`（接在 iota 末尾，不位移已有类型）。`Payload` 是 `"N domains"` 计数摘要；`Adapter` 取第一片叶子；`ProviderNames` 是子规则并集。面板 `/rules` 把一段显示成一条，叶子计数仍可从 wrapper 读。
- `component/process.FindProcessName` 加 key=`{network,srcIP,srcPort}` 的 LRU，容量 256、TTL 200ms。`common/lru.WithAge` 粒度是秒，200ms 放在 entry 里自管过期。Always 与 Strict 共用（都走 `FindProcessName`）。
- `RuleWrapper.Miss()` 每次加 `missCount`；`missAt` 用 CAS 最多 1s 写一次。`Hit()` 仍每次更新时间。

`parseFakeIPRules` 不走这段编译。不把整表打成一个 DomainSet。

## Alternatives considered

- **整表一个 DomainSet，命中后查 `map[host]adapter`** — 最强论据是 O(1) 且实现更短。否决：破坏 first-match（同 host 两条不同出站）以及 SUFFIX 与精确的优先级；constraints 已禁止。
- **把 SRS / MRS 当主规则格式** — 最强论据是 mmap 与 RULE-SET 已有编译器。否决：Clash 订阅和面板编辑的是 YAML 一行；SRS 当主格式已禁止。
- **匹配时并行扫规则** — 最强论据是多核能吃满。否决：first-match 必须顺序；并行会数据竞争且无法保证「第一条」。
- **删 GEOIP / 改 SRS 国家码** — 最强论据是 sing-box 1.12 已删、少一次 MMDB。否决：Clash 生态 MMDB/面板/订阅依赖 GEOIP，constraints 禁止删。
- **PROCESS 用 `common/lru.WithAge(1)`** — 最强论据是少一份过期字段。否决：WithAge 是 Unix 秒，200ms TTL 会被量化成 1s，和验收「同五元组 10ms 内不二次查找、200ms 后失效」对不齐。

## Consequences

- **收益**：连续 DOMAIN/SUFFIX 广告列表的 miss 路径从 O(段长) 字符串比较变成一次 `DomainSet.Has`；PROCESS 短连接复用 200ms 内的查找；高 QPS 下 wrapper miss 不再每条规则打一次时间戳 store。
- **代价与已知上限**：用户规则被 PROCESS/GEOIP/KEYWORD 打散则段短、收益小（可接受）。面板 `/rules` 把一段显示成一条 `DomainSpan`；span 本身不套 `RuleWrapper`，`PATCH /rules/disable` 按 index 对这段是 no-op（叶子 wrapper 不在顶层 slice）。DomainSet 是「是否可能命中」的过滤器，真正 adapter 仍扫叶子——set 假阳只会退化成线性，不会选错出站。PROCESS LRU 缓存错误结果 200ms（含 `ErrNotFound`）。重访信号：面板必须逐行 disable/展示 payload，或连续段平均长度实测接近 1。

## Verification

- `GOTMPDIR=/home/dev/tmp go test ./rules/ ./rules/wrapper/ ./rules/common/ ./component/process/ -count=1`
- `TestDomainSpanFirstMatchSameHost`：同 host PROXY 先于 REJECT。
- `TestDomainSpanExactDoesNotMatchSubdomain` / `TestDomainSpanSuffixDotDelimited`：精确不匹配子域、SUFFIX 点分隔。
- `TestDomainSpanMissSkipsLeafScan`：set miss 不调叶子 `Match`。
- `TestProcessCacheTTLAndCap`：同五元组 TTL 内不二次查找；256 淘汰最老。
- `TestMissSamplesTimestamp`：两次 miss 计数 +1，时间戳不变。
