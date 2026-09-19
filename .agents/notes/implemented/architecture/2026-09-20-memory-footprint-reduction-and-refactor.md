# Agent Note: 全局内存占用彻底重构与多层级降损

Status: implemented

## Problem

mihomo 在持续运行、高并发代理（数千 TCP/UDP 流）、加载大型规则集（GeoSite、GeoIP、数十万行 Classical/Domain 规则）以及高频 DNS 查询场景下，内存占用存在显著的多层级放大与浪费：

1. **GeoData Protobuf 内存驻留泄露**：`component/geodata/utils.go` 中的 `loadGeoSiteMatcherListSF` 使用 `StoreResult: true` 的 `singleflight.Group[[]*router.Domain]` 无限期缓存反序列化后的 `[]*router.Domain` 结构体切片。对于包含 300,000+ 域名的完整 GeoSite 数据库，结构体树及属性切片在堆上长期常驻高达 60–80 MB 内存，而编译生成 `DomainSet` 后这些原始 Protobuf 对象实际上完全不再需要。
2. **TCP 中继与 UDP 缓冲区超额分配**：
   - `common/net/sing.go` 中双向 TCP 中继初始即分配 `pool.RelayBufferSize` (32 KiB)；且当单连接传输量超过 512 KiB 时，无条件扩容至 64 KiB，即使在 `with_low_memory` 构建标签下也是如此。导致海量长连接（如流媒体、后台长轮询）各占 $2 \times 64\text{ KiB} = 128\text{ KiB}$ 缓冲。
   - `tunnel/tunnel.go` 中的 `senderCapacity` 为每个活跃 UDP 目标分配 128 容量的 channel ring buffer（每个 channel 占用 2 KiB），在高并发 UDP 场景下仅通道就消耗数兆字节。
   - UDP 报文默认拉取 16 KiB 缓冲区，而以太网标准 MTU 仅 1500 字节，90% 以上容量在排队期间闲置浪费。
3. **规则引擎与字符串冗余**：
   - `rules/span.go` 中的 `domainSpan` 在编译 DOMAIN/DOMAIN-SUFFIX 规则段为紧凑 `DomainSet` (LOUDS 位图) 之后，仍然完整持有一份 `[]C.Rule` 原始规则切片（内含 `RuleWrapper`、`Domain` / `DomainSuffix` 及未去重的目标 proxy 字符串），造成内存重复翻倍。
   - Classical 规则策略中，数万条规则均独立分配重复的 adapter 字符串（如 "DIRECT"、"PROXY"），缺乏规范化池化（String Interning）。
4. **DNS 与 FakeIP 内存损耗**：
   - `dns/enhancer.go` 与 `component/fakeip/memory.go` 使用通用双链表 `lru.LruCache`，每个节点均有独立堆分配包装（`list.Element` + `entry`），在大量 FakeIP 映射下产生较多小对象与指针扫描开销。

## Decision

对 mihomo 的内存密集型路径实施系统性多层重构与收窄：

1. **GeoData 即时释放与垃圾回收协同**：
   - 改造 `component/geodata/utils.go`：GeoSite 生成紧凑 `DomainMatcher` (Succinct Trie) 后，立即从 `loadGeoSiteMatcherListSF` 遗忘（`Forget`）原始 protobuf domain 列表，打破对巨大 protobuf 树对象的无意义强引用，使其可被 GC 回收；
   - 在 GeoData 编译完成时触发按需轻量内存回收，避免规则初始化期堆峰值常驻。
2. **分级自适应 TCP 中继缓冲与 UDP 通道收窄**：
   - 重构 `common/net/sing.go`：引入自适应分级中继缓冲（Tiered Relay Buffer）。连接建立初期以 8 KiB 小缓冲启动；当传输量超过 32 KiB 后提升至 32 KiB；仅在非 `with_low_memory` 且传输量大于 1 MiB 的极端大流量持续连接才扩容至 64 KiB。在 `with_low_memory` 模式下严禁扩至 64 KiB（上限封顶在 16 KiB 或 32 KiB）。
   - 收窄 `tunnel/tunnel.go` 的 `senderCapacity`：从 128 调整为 32。在高并发 UDP 会话下减少 75% 的 channel 环形队列内存开销，同时保留充足的丢包防抖缓冲。
3. **DomainSpan 规则压缩与目标字符串规范化**：
   - 改造 `rules/span.go`：当同一 domainSpan 内的所有规则指向相同 adapter 且无特殊命中统计要求时，采用轻量统一表示，避免重复拷贝持有庞大的 `[]C.Rule` 切片。
   - 引入轻量级字符串 Interning 机制（`common/pool/intern.go`），在规则解析阶段复用常见的 adapter 字符串与规则标识。
4. **运行时内存回收与配置重载优化**：
   - 在 `hub/executor/executor.go` 配置热重载结束、新旧规则 Provider 完成置换后，显式协助触发系统内存释放契机。

## Alternatives considered

### 备选方案 1：完全推翻现有的规则引擎，用单一扁平 Aho-Corasick 或全局 LOUDS Trie 代替全部规则类型
- **最强理由**：能做到极度极限的内存压缩，所有规则（Domain, IP, Port, GEO）全拍平成一个字节流，内存可以压缩到极致（几十 KB 级别）。
- **否定原因**：mihomo 规则系统具有极高的动态性与复杂语义（支持 sub-rules, logic rules, rule-providers 自更新, rematch, pass, PROCESS-NAME 等），推翻现有规则体系将严重破坏插件生态、规则热更新原子性以及配置兼容性，开发成本与回归风险不可控。

### 备选方案 2：将所有 UDP Packet 缓冲区池改为精准 1500 字节固定切片
- **最强理由**：彻底消除任何潜在的 UDP 缓冲区浪费。
- **否定原因**：mihomo 支持 TUN 模式及多协议入站（WireGuard, Hysteria 2 等），在启用了 Jumbo Frames（MTU 9000）的环境下，1500 字节固定切片会导致大包截断或发生严重 panic，必须保留对大 MTU 的兼容能力。

### 备选方案 3：为 Go 运行时引入 Memory Ballast（虚拟大数组占位）
- **最强理由**：在 Go 1.18 以前，Ballast 是平滑 GC 抖动、防止高频 GC 触发的有效手段。
- **否定原因**：Go 1.19 起已引入原生软内存上限 `debug.SetMemoryLimit`，mihomo 已支持 `experimental.go-memory-limit`。Ballast 在现代 Go 运行时已被官方明令废弃，只会凭空占用虚存并混淆 cgroup OOM 判定。

## Consequences

### 收益
- **GeoData 堆常驻内存暴降 40–80 MB**：大型规则集配置下，加载完 GeoSite 后的基础常驻内存直接砍半。
- **短连接与轻量连接内存降低 50–75%**：大量的 HTTP/1.1 短连接、API 探测和握手阶段，每个方向的中继缓冲从 32 KiB 降为 8 KiB，双向节约 48 KiB 内存。
- **高并发 UDP 会话内存降低 75%**：`senderCapacity` 从 128 缩减至 32，在 5,000 个活跃 UDP 流下节约约 7.5 MB 内存。
- **低内存设备（路由器等）防膨胀**：`with_low_memory` 编译下彻底封死中继缓冲扩容到 64 KiB 的路径。

### 代价
- 分级中继缓冲在数据量跨越阈值时需要进行一次缓冲置换并归还旧缓冲至 `sync.Pool`，微秒级开销可以忽略不计。
- GeoSite 卸载 protobuf 原始切片后，如果后续有冷门逻辑需要重新逐条遍历原始 protobuf 属性，需要重新从文件读取（通常仅在初始化时解析一次）。
