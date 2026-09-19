# Agent Note: 可维护性批次:上帝文件拆分、死代码清除与核心区补测

Status: implemented

## Problem

三轴审计(架构分层、测试覆盖、复杂度/死代码)显示 mihomo fork 的可维护性债务集中在四处:

1. `config/config.go` 2009 行,是全仓唯一突破 800 行红线的上帝文件,Parse 入口与 DNS/general/rules/proxies/tun/sniffer/hosts/listeners 八个职责混在一个文件里。
2. `common/murmur3`、`common/batch`、`component/power` 三整包零引用,另有 8 个死符号(`contextutils.WithoutCancel`、`once.OnceValues`、`utils.EmptyOr`、`Range.LeftContains/RightContains`、`NewSignedRange(s)`/`FromList`、`IntRanges.Merge`、`picker.WithContext`),其中 `parseSigned` 链是传递性死代码。
3. 6 处文档(AGENTS.md、README.md、mkdocs 的 config/example 页面各三种语言)仍指向迁移 mkdocs 时已删除的 `docs/config.yaml`,照着走的人会撞空路径。
4. `dns` 反向 import `tunnel` 取常量 `DnsRespectRules`,是 component→tunnel/statistic 之外的同类层级倒挂。

同时 `dns/`、`rules/`、`common/singleflight`、`common/deque` 这些核心纯函数区零测试,任何后续重构都没有回归保护。本批治结构、清死代码、补关键测试;与 [现代化批次](../process/2026-09-16-modernization-pass.md)(CI/依赖/构建基建)是同一时期两个互补的治理批次。

## Decision

1. **`config.go` 按职责同包拆分**:拆为 `config.go`(641 行,保留 `Config` 类型与 `Parse` 入口)+ `dns.go`(441)/ `general.go`(260)/ `rules.go`(305)/ `proxies.go`(136)/ `tun.go`(111)/ `sniffer.go`(102)/ `hosts.go`(79)/ `listeners.go`(27)。66 个顶层声明**逐字节移动**,不建子包、不导出新符号、不改任何函数体,行为零变化。选同包拆分而非建子包:`package config` 已被全仓按路径引用,建子包要改导入面;选纯移动而非顺手重构:2009 行文件的可维护性第一步是"让每个职责可见",内部逻辑重构留到有单测覆盖之后。
2. **死代码清除**:删 `common/murmur3`、`common/batch`、`component/power` 三整包及上列 8 个死符号,每项 grep 复核零残留。**保留** `tunnel.TCPIn/UDPIn` 与 `utils.HashType.Len`:前者虽标 Deprecated 且仓内零调用,但 mihomo 被当库引用(CMFA 等外部消费者在用),删导出符号是破坏性 API 变更;后者可能经 msgpack 接口间接使用。这两项是"看起来是死代码但不能删"的边界,删之前 MUST 区分"仓内零引用"与"外部零引用"。
3. **文档死指针清除**:6 个文件改指 mkdocs 实际路径,全仓 grep 零残余;`AGENTS.md` 关键目录表里的 `batch` 条目同步移除。
4. **`dns→tunnel` 常量提升**:`DnsRespectRules` 从 `tunnel/dns_dialer.go` 提到 `constant/dns.go`,`dns/dialer.go` 的 `RespectRules` 别名改指 `constant`。当时 **`dns` 对 `tunnel` 的 import 仍保留**——`dns` 还依赖 `tunnel.DNSDialer`/`NewDNSDialer`。该倒挂已由后续 [atomic resolver + DNSDialer factory](../architecture/2026-09-19-atomic-resolver-dns-dialer-factory.md) 解开。
5. **核心区补测**:`rules/parser` 分发与错误路径、`rules/common` 域名匹配边界、`rules/logic` 的 NOT 与短路语义、`common/singleflight` 去重与 re-panic 语义、`common/deque` 容器不变式,共 17 顶层测试 124 子测试。补测中修正了 3 处按想象语义写错断言的用例(`dns.updateTTL` 是移位语义不是 clamp)。被测试钉死的语义见下节。

## 钉死的匹配语义(测试即契约)

`rules/common` 的几条边界写成测试后成为事实契约,改之前 MUST 意识到测试会红:

- `DOMAIN` 是精确匹配,不匹配子域。
- `DOMAIN-SUFFIX` 要求点分隔,因此 `example.com` 的规则不命中 `notexample.com`。
- 域名规则只小写 pattern、不小写 host;大小写归一化是**调用方**的责任。
- `IP-Suffix` 匹配的是尾字节序列,不是网络前缀语义。

`common/deque` 的空 `Pop` panic 是实现的实际行为,按实现测而非按"应当返回零值"的想象测。`common/singleflight` 的 re-panic 语义:同一个 key 的并发 caller 必须看到首次调用的 panic,不能被吞成 nil。

## Testing

- 全量 `go build ./...` 绿。
- `go test ./config/ ./rules/... ./common/singleflight/ ./common/deque/ ./dns/ ./common/utils/` 全绿(rules: 17 顶层 124 子测试;singleflight 94.1% 覆盖,deque 28.9% 覆盖目标 API 面)。
- 单测质量验证:`TestCommon2` 对 4 处变异(`PopFront` 读尾、空 `Pop` 不 panic、`Do` 丢 `wg.Wait`、吞 panic)做变异测试,每个变异被新测试抓出 4–9 个失败——测试不是凑数。
- `gofmt` 全部新文件干净。
- 已知存量 flake:`common/xsync` 两个并行 map 测试在干净 HEAD 上也偶发失败(时序依赖),与本批无关。`common/singleflight` 补测随后在 CI 上暴露 `arrivedBarrier` 调度竞态,会合点已改为等 `dups`,见 [singleflight dups 栅栏](../testing/2026-09-17-singleflight-dups-barrier.md)。

## Alternatives considered

- **`config.go` 建子包拆分**(最强论据:包边界强制内聚,是比同包文件更强的约束,还能收窄符号可见性):否决,`package config` 被全仓按路径引用,建子包要改导入面,代价远超收益;同包拆分已让每个职责可见,且 upstream merge 时冲突更小。
- **`tunnel.TCPIn/UDPIn` 一并删除**(最强论据:Deprecated 标注 + 仓内零调用,死代码就该删干净):否决,mihomo 被当库引用(CMFA 等),导出符号删除是破坏性变更,不在本批范围;仓内零引用 ≠ 外部零引用。
- **`dns→tunnel` 完全解耦**(最强论据:component→tunnel/statistic 与 dns→tunnel.DNSDialer 是同类倒挂,一起治更彻底):当时否决,`DNSDialer` 的依赖注入改造影响面广且上游同构,收益不抵风险,留白给后续批次。该倒挂现已由 [atomic resolver + DNSDialer factory](../architecture/2026-09-19-atomic-resolver-dns-dialer-factory.md) 落地,本篇不再持有该约束。
- **顺手重构 `config.go` 内部逻辑**(最强论据:既然在动这个文件,`parseDNS` 204 行之类热点一起拆函数):否决,无单测覆盖的大函数重构是赌博;先靠纯移动降低可读性门槛,等补测后再动。
- **interface{} 大头与全局状态治理**(最强论据:审计已定位,一次性收窄):否决,`interface{}` 集中在 protoc 生成代码(不该动),全局单例是仓库公认风格,不在本批。

## Consequences

- 收益:`config` 包职责一目了然(9 文件按域分);死代码减负(3 包 + 8 符号,含传递性死的 `parseSigned` 链);文档不再误导新读者;核心纯函数区(dns 工具、rules 匹配、并发原语)首次有回归保护,且补测过程本身揪出并修正了 3 处断言与实现语义的偏差。
- 代价:`config` 包文件数 1→9,upstream merge 时 `config.go` 的冲突面增大(同包拆分比建子包好合并,但成本不为零);`rules/common` 的匹配语义被测试钉死,若未来上游改语义,测试会红——这是期望行为还是测试脆弱性,需要 reviewer 按改动意图判断。
- 留白:`tunnel` listener 的 `ReCreate` 复制粘贴、`hub/route` 业务下沉、`statistic` 依赖注入。`dns→tunnel.DNSDialer` 倒挂已由 [atomic resolver + DNSDialer factory](../architecture/2026-09-19-atomic-resolver-dns-dialer-factory.md) 接管。
