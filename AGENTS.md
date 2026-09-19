# Repository Guidelines

## 最重要
- 默认用简体中文回复;除非用户明确要求英文。
- 用户是智障人士,回答时先给结论,再用日常语言解释原因、影响和建议;少用术语,必须用时先解释。
- 先确认事实再下结论;涉及最新信息、规则、价格、公告、生产状态等,优先用工具核验。
- 给建议时给出推荐方案和原因,不只列选项;不要用"你只需需要......"弱化问题难度。
- 必要的时候可以考虑 Subagent-Driven 方式执行，减少上下文的污染。

## 工程原则
- KISS:优先最小可行改动,避免不必要的复杂性。
- YAGNI:只实现用户当前明确需要的内容,拒绝过度设计。
- DRY:抽取重复逻辑,但不为"未来复用"而提前抽象。
- SOLID:保持职责单一、接口小而清晰、依赖抽象而非具体实现。

## 重要改动必须留笔记

1. 非平凡改动（改了行为、架构、跨文件契约、流程与工具链、测试策略、落盘/网络/配置格式）前，遵循 `.agents/skills/write-notes-like-deepseek/SKILL.md` 写或更新笔记；机械性小改（样式、格式化、打标、不改行为的补丁）直接提交代码。
2. 写之前先检索 `.agents/notes/` 里的同主题旧笔记：有归属就地更新；新想法先放 `proposed/`，落地随同代码改动转 `implemented/`；新方案彻底取代旧决策时，同批归档旧篇并标明被谁取代。
3. 被放弃的方案先写它最强的理由，再解释为什么不用。
4. 提交前跑校验（见下），红了先修再交。

## 项目概述

mihomo（`github.com/metacubex/mihomo`）——Go 编写的代理内核（Clash.Meta 内核，MetaCubeX 出品）。提供本地 HTTP(S)/SOCKS 监听、TUN，支持多种代理协议（VMess/VLESS/Shadowsocks/Trojan/Snell/TUIC/Hysteria/WireGuard 等）、规则引擎、内置 DNS（DoH/DoT/DoQ、fake-ip）以及 REST 外部控制器。GPL-3.0 许可证附带附加条款：非 MetaCubeX 关联的下游项目不得在名称中使用 "mihomo"。

## 架构与数据流

```
config.yaml ──▶ config.Parse ──▶ hub.Parse ──▶ executor.ApplyConfig ──▶ tunnel/listener/dns/resolver 全局状态
                                                    ▲                        │
REST PUT /configs / SIGHUP ─────────────────────────┘                        ▼
                                              listener accept 循环 ──▶ tunnel.HandleTCPConn/HandleUDPPacket
                                                                              │ match() 规则匹配
                                                                              ▼
                                                              adapter/outbound ProxyAdapter.DialContext
```

- **启动**（`main.go`）：解析 flag/env → `config.Init` → `hub.Parse`。SIGINT/SIGTERM 退出；SIGHUP 重新解析配置（热重载）。子命令内联分发：`convert-ruleset`、`generate`、`age`。默认 net resolver 被埋了雷（触发即崩溃）——**所有 DNS 必须走 `component/resolver`**。
- **`hub/executor/executor.go` 的 `ApplyConfig(cfg, force)`** 是唯一的热重载入口：先挂起 tunnel，按固定顺序更新各子系统（`updateProxies` → `updateRules` → … → `updateTun`），并行加载 provider，最后恢复。listener 通过 `ReCreateX` 函数重建（`listener/listener.go` 中每种 listener 类型一个全局变量 + 一把 mutex）。
- **`tunnel/tunnel.go`** 数据包管线：`handleTCPConn`（修正 metadata → fake-ip/hosts 映射 → 嗅探 → 解析 → 经 `component/slowdown` 退避重试 → 经 `ProxyAdapter` 拨号 → `statistic.NewTCPTracker` → 双向中继）；UDP 走分片 worker 队列 + NAT 表。规则匹配在 `match()`；`C.Pass`/`C.Rematch` 驱动子规则重入。
- **`dns/`**：`Resolver` + `Enhancer`（fake-ip/mapping 模式）+ `Service`；中间件链（`dns/middleware.go`：withHosts → withFakeIP → withMapping → withResolver），处理器签名为 `handler = func(ctx *icontext.DNSContext, *D.Msg) (*D.Msg, error)`。
- **全局状态 + 访问器风格**：无依赖注入框架；子系统状态以包级全局变量存放、在 mutex 保护下整体替换；组装通过 `executor.ApplyConfig` 和显式的 `C.Tunnel` 接口参数完成。

### 核心接口（均在 `constant/`）

- `ProxyAdapter`（`constant/adapters.go`）：`Name/Type/SupportUDP/DialContext(ctx, *Metadata)/ListenPacketContext/Unwrap`。具体包装 `adapter.Proxy` 增加存活状态/延迟历史。基础实现：`adapter/outbound/base.go`（`Base`、`BaseOption`）。
- `InboundListener`（`constant/listener.go`）：`Name/Listen(tunnel)/Close`。
- `Metadata`（`constant/metadata.go`）：贯穿全链路的唯一结构体（源/目的地址、入站名称/用户、嗅探到的 host、进程信息）。
- `Rule`（`constant/rule.go`）：`Match(*Metadata, RuleMatchHelper) (bool, adapterName)`——`RuleMatchHelper` 携带懒加载的 IP 解析/进程查找钩子。
- Provider（`constant/provider/interface.go`）：`Vehicle`（HTTP/File/Inline）+ `ProxyProvider`/`RuleProvider`，自更新后回调通知 tunnel。

## 关键目录

| 路径 | 用途 |
|---|---|
| `adapter/` | 出站/入站适配器，`parser.go`（代理类型分发），`outboundgroup/`（Selector/URLTest/Fallback 等），`provider/` |
| `tunnel/` | 核心数据包管线 + 规则匹配 + 模式 |
| `listener/` | 入站监听器（socks、http、mixed、tproxy、sing_tun、tuic 等），`parse.go`，各类型 `ReCreate*` |
| `dns/` | resolver、enhancer（fake-ip）、DoH/DoT/DoQ/DHCP 上游、中间件 |
| `rules/` | 规则类型（`common/`、`logic/`），`parser.go`，`provider/` |
| `transport/` | 协议引擎（vmess、vless、trojan、tuic、hysteria 等） |
| `config/` | `config.go`——解析/校验 YAML 为强类型 `Config` |
| `hub/` | `hub.go`（Parse/ApplyConfig），`executor/`（应用引擎），`route/`（REST API） |
| `constant/` | 接口、枚举、`Metadata`、路径默认值、build-tag 特性开关 |
| `component/` | resolver、sniffer、trie、fakeip、profile、dialer 等 |
| `common/` | 自研工具库：`atomic`、`xsync.Map`、`singleflight`、`queue`、`pool`、`structure`（解码器） |
| `test/` | 独立 Go module——基于 Docker 的协议集成测试 |

## 开发命令

```bash
go build                                        # 普通构建
go build -tags with_gvisor                      # 包含 gVisor TUN 栈（Makefile/CI 默认）
make linux-amd64-v3                             # 交叉编译目标：darwin-arm64、windows-amd64 等
make all                                        # 常用发布二进制，输出到 bin/
make lint                                       # golangci-lint run ./...
make vet                                        # go vet ./...
go test ./... -v -count=1                       # 单元测试（根 module）
go test ./... -v -count=1 -tags "with_gvisor"   # gvisor 变体
cd test && make test                            # 集成测试（需要 Docker daemon）
```

版本注入通过 ldflags：`-X 'github.com/metacubex/mihomo/constant.Version=…' -X '…constant.BuildTime=…'`。

Build tags：`with_gvisor`（gVisor IP 栈，同时控制 tailscale），`with_low_memory`（更小的缓冲区），`no_easytier`/`no_tailscale`/`no_zerotier`/`no_fake_tcp`（编译期裁掉对应特性），`cmfa`（Android 构建）。特性状态由 `mihomo -v` 报告（`constant/features/`）。

本地运行：`go run . -d <配置目录> -f <配置文件>`；`-t` 测试配置后退出。默认路径见 `constant/path.go`。标准构建为 `CGO_ENABLED=0`（Android CI 是唯一用 cgo 的例外）。

决策笔记（`write-notes-like-deepseek`，skill 位于 `.agents/skills/`）：笔记存放在 `.agents/notes/{proposed,implemented,rejected,archived}/{feature,bug-fix,simplification,architecture,process,testing}/yyyy-mm-dd-topic.md`；路径与格式由脚本强制校验。

```bash
npx tsx .agents/skills/write-notes-like-deepseek/scripts/verify-agent-note-tree.ts    # 目录/类别/文件名/互链
npx tsx .agents/skills/write-notes-like-deepseek/scripts/verify-agent-note-format.ts  # 头块/必备节/备选方案
npx tsx .agents/skills/write-notes-like-deepseek/scripts/verify-archived-agent-notes.ts # 归档封印
npx tsx .agents/skills/write-notes-like-deepseek/scripts/archive-agent-note.ts <笔记> --superseded-by <新笔记>  # 归档被取代决策
```

## Go LSP / DAP

本机已装好并验证可用，**直接用，不要重复安装**：

- **gopls**（LSP）v0.23.0，`~/.local/bin/gopls`。优先用 `lsp` 工具做定义/引用/悬停/重命名/诊断，跨文件改动必须走 `lsp rename`，不要用 sed/手改。
- **dlv**（DAP）v1.27.2，`~/go/bin/dlv`。用 `debug` 工具调试：`launch` 时 `adapter: "dlv"`、`program` 可以直接给包目录（dlv 支持目录目标，会自己 build）。

注意事项：

- 调试真实包时编译耗时可能超过默认超时，`launch` 带上 `timeout: 60` 以上。
- dlv 停在入口的瞬间查 `stack_trace`/求值局部变量会报错——等一两秒或先 `continue` 后再查，这是 Delve 初始化时序，不是故障。
- `/tmp` 是 tmpfs 且配额小（<500M）：编译 Go 报 `disk quota exceeded` 时先清 `/tmp/go-build*`，或设 `GOTMPDIR=/home/dev/tmp`。

## 代码规范与常见模式

- **Go 版本**：go.mod 要求较新 Go（CI 矩阵覆盖 1.24–1.26；代码可使用 1.24 语言特性）。
- **import 别名**：单字母别名是本仓库惯例——`C constant`、`P constant/provider`、`N common/net`、`LC listener/config`、`D miekg/dns`、`M sing metadata`、`icontext context`。
- **import 顺序**（gci 强制）：标准库 → `github.com/metacubex/mihomo/*` → 第三方。mihomo 自身 import 排在外部依赖**之前**。
- **格式化/lint**（`.golangci.yaml`，根目录与 `test/` 相同）：`disable-all` + gofumpt、govet、gci、staticcheck。
- **配置解码**：option 结构体用 `proxy:"name"` 标签，由 `common/structure` 解码（mapstructure 魔改版，`WeaklyTypedInput`）；构造函数 `NewX(option XOption) (*X, error)`。
- **错误处理**：带位置上下文地包装——`fmt.Errorf("proxy %d: ...: %w", i, err)`；哨兵错误（`resolver.ErrIPNotFound`、`C.ErrNotSupport`）；日志用 `log.Infoln/Warnln/Errorln/Debugln/Fatalln`（printf 风格，无结构化日志）。
- **并发**：TCP 每连接一个 goroutine；UDP 用固定分片 worker 池；`common/atomic`、`common/xsync.Map`、`common/singleflight`；`sync.Once` 懒初始化；全局状态由 `configMux` 风格的 mutex 保护。
- **metadata 补充**：入站 `Addition` 函数式选项（`adapter/inbound/addition.go`：`WithInName`、`WithSpecialRules` 等）。
- `docs/docs/config/`（mkdocs 配置参考）的注释以中文为主、中英混排——改配置文档时保持该风格。

### 新增代理类型（示例参照：`anytls`）

1. `constant/adapters.go`：加 `AdapterType` 枚举 + `String()` 分支。
2. `adapter/outbound/<type>.go`：结构体内嵌 `*Base` + `XOption{BasicOption; …}`（带 `proxy:` 标签）；按需实现 `DialContext`/`ListenPacketContext`（不需要时继承 `Base` 默认实现）；通过 `option.NewDialer(outbound.DialOptions())` 接线 dialer。
3. `adapter/parser.go`：加 `case "<type>":`，用 `structure.Decoder` 解码。
4. 协议引擎放 `transport/<type>/`；入站服务端（可选）放 `listener/<type>/` + `adapter/inbound/<type>.go`，并在 `listener/parse.go` 和 `executor.updateListeners` 注册。

### 新增规则类型

`constant/rule.go` 加枚举 → `rules/common/<type>.go`（内嵌 `common.Base`，断言 `var _ C.Rule = (*X)(nil)`）→ `rules/parser.go` 加 `case`。

## 重要文件

- `main.go`——入口、flag、信号、子命令
- `config/config.go`——配置解析/校验（另见 `docs/docs/config/` = mkdocs 完整带注释配置参考；新增配置项时同步更新它）
- `hub/executor/executor.go`——`ApplyConfig`，热重载引擎
- `tunnel/tunnel.go`——TCP/UDP 管线 + `match()`
- `constant/adapters.go`、`constant/metadata.go`、`constant/rule.go`——核心接口
- `adapter/parser.go`、`listener/parse.go`、`rules/parser.go`——类型注册表
- `hub/route/`——REST 外部控制器
- `Makefile`、`.golangci.yaml`、`.github/workflows/{build,test}.yml`

## 运行时/工具链偏好

- **纯 Go**（无 Bun/Node 运行时依赖）；标准 `go` 工具链 + modules。`test/` 是**独立 module**（`mihomo-test`），通过 `replace` 指向根目录（笔记校验脚本除外，那个需要 npx tsx）。
- `CGO_ENABLED=0` 为默认；CI 为旧系统目标使用 MetaCubeX 魔改 Go 工具链——本地开发无关。
- 分支模型：开发在 `Alpha`，发布在 `Meta`。**严格 Conventional Commits**（`fix:`、`feat:`、`chore:`）——发布说明按前缀分桶（`.github/genReleaseNote.sh`）。
- Docker 镜像经 buildx 多架构构建（`docker/`、Makefile docker 目标）。

## 测试与质量保障

- **单元测试**：根 module，标准 `testing` + `testify`（`assert`/`require`），`t.Run` 表驱动；测试与被测同包；fixture 与测试同目录。`go test ./... -v -count=1`。
- **集成测试**：`test/` module，`package main`，Docker SDK 驱动——没有 Docker daemon 时 `init()` 直接 panic。运行真实协议服务端容器 + 共享 `testSuit`（TCP/UDP pingpong + 大流量传输）。`cd test && make test`（串行，`-p 1`）。**CI 不跑这套**——手动套件。
- 根 module 测试的环境变量开关：`SKIP_INTEROP_TEST=1`（跳过 listener/inbound 互操作测试）、`SKIP_CONCURRENT_TEST=1`。
- CI（`.github/workflows/test.yml`）：6 系统 × Go 1.24–1.26，`CGO_ENABLED=0`，带/不带 `with_gvisor` 各跑一遍；macOS 上删除 `listener/inbound/*_test.go`。无覆盖率工具和阈值。
- Lint：`golangci-lint run ./...`；`test/` module 在 `GOOS=darwin` 和 `GOOS=linux` 下各 lint 一遍。
