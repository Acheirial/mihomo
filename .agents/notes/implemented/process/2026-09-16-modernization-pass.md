# Agent Note: 现代化批次:旧式 atomic 收敛、Go 1.20 兼容面钉住与构建流程收敛

Status: implemented

## Problem

mihomo 主体代码停留在 Go 1.19 时代的写法:`ioutil.*` 已废弃、`globalConv`/`KeyUpdateAfterBytes` 等包级可变变量用 `atomic.Value` 或裸 `atomic.AddInt64` 操作,与仓库 `common/atomic` 的泛型包装并存两套写法。构建侧则遗留 cgo 时代产物(Makefile 的 CLANG/CFLAGS 死代码)、错标为 `go test` 的 vet 目标、37 个手写展开的重复平台目标,以及 Dockerfile 漂浮在 `alpine:latest`。CI 侧第三方 action 钉在 `@main` 且三个 job 权限是 write-all。这批清理各自有"顺手就能做错"的坑:拔高 go 下限会拆掉 upstream 刻意维护的兼容面,alpine pin digest 会拆掉多架构分发——本篇记录每处为什么停在"刚好不够激进"的位置。

## Decision

- **Go 代码只做写法收敛,不碰语义**:`transport/hysteria/conns/faketcp/tcp_linux.go` 用 `io.Discard` 替代 `ioutil.Discard`、用 `atomic.Pointer[time.Time]` 替代 `atomic.Value`;`transport/mkcp/conn.go` 的 `globalConv` 收敛为 `atomic.Uint32`;`transport/sudoku/crypto/record_conn.go` 的 `KeyUpdateAfterBytes` 收敛为 `atomic.Int64`,默认 32MiB 经修正保留。审计后划出的不动清单:`xsync/map.go`、`once/once_go120.go` 是刻意低层优化不迁移;`interface{}` 大头在 protoc 生成代码;`time.Now().Unix()` 是 Unix 秒语义。旧式 atomic 经此批次已基本收敛于 `common/atomic` 泛型包装,新代码 MUST 用它,never 再引裸 `sync/atomic.Value`。
- **go 1.20 下限保持不动**:它是 upstream 为 Win7/老 macOS 刻意维护的兼容面。`x/*` 家族、logrus、brotli、miekg/dns 等被精准钉在各自最后 go1.20 兼容 tag,单独拔高任何一个都会牵动整组。本批只跟进 `sing-mux v0.3.11` 与 mipstack/easytier pseudo 版本。
- **Makefile 收敛**:`go vet` 目标从 `go test` 改回真正的 `go vet`;CLANG/CFLAGS 等 cgo 时代死代码删除;37 个平台目标收敛为 `define` + `eval` 参数化模板,目标名与产物名全部不变;AGENTS.md 同步。
- **Dockerfile 基础镜像钉 minor tag**:`alpine:latest` → `alpine:3.22`,不 pin digest;保持 root 运行并加注释说明原因与非 root 覆盖方式。
- **CI 收紧**:docs.yml 的 checkout/setup-python 升 v7,新建 `docs/requirements.txt` 锁版本并配 pip 缓存;build.yml 的第三方 `8Mi-Tech/delete-release-assets-action@main` 替换为 `gh release delete-asset` 步骤,job 权限从 write-all 最小化为 `contents`(Docker Hub 推送已从本 fork 删除,见 [drop docker hub push](2026-09-17-drop-docker-hub-push.md));test.yml 对非默认分支加 concurrency cancel;`.golangci.yaml` 的 `staticcheck.go` 1.19 → 1.20 与工具链下限对齐。

## Alternatives considered

- **整体拔高 go 下限到 ≥1.25,解锁全组钉住的依赖升级**(最强论据:x/* 家族、logrus、brotli、miekg/dns 全部能升到最新,一次解决落后):否决,go 1.20 是 upstream 为 Win7/老 macOS 刻意维护的兼容面,拔下限是破坏性决定,牵动所有 pinned 依赖和上游同步路径,收益(新依赖版本)完全能等;升级它们的前提就是整体提 go 下限,属"需适配"而非"白捡"。
- **alpine pin 到 digest 而非 3.22**(最强论据:digest 才是真正的不可变可复现构建):否决,alpine 官方多架构分发靠同一个 tag 的 manifest list,rust 镜像目标含 armv7 等架构,per-arch digest 只覆盖单一架构,会拆掉多架构构建;安全面上 mihomo 以静态二进制进镜像,alpine minor tag 滚动补丁已够。
- **Dockerfile 加 USER 非 root 运行**(最强论据:最小权限是镜像最佳实践):否决,默认配置目录 `/root/.config/mihomo` 是 700 权限,TUN 模式需要 NET_ADMIN,非 root 用户拿不到;改为在 Dockerfile 注释里给出覆盖方式,把决定留给有真实非 root 需求的用户。
- **logrus 换成 slog 等现代日志库**(最强论据:logrus 已进维护模式,迟早要换):否决,上游有浅封装、替换牵动面大,而 go 1.20 下限还没提,同批做两个破坏性决定只会让回滚粒度变粗;留给提下限批次一并处理。
- **37 平台目标保留手写展开**(最强论据:展开写法一眼可读,模板引入 eval 有学习成本):否决,37 个目标每次加架构要同步改多处,已经漂移出 cgo 死代码;参数化后目标名/产物名不变,对使用者零感知。

## Consequences

- 收益:Go 写法收敛到单一惯例(`io.Discard`/`common/atomic` 泛型包装),新代码有明确规范可循;构建流程删掉 cgo 死遗产,vet 真正生效,加平台从改 37 处变成改模板;镜像与 CI 不再依赖漂浮 tag 与第三方 `@main` action,权限面最小化;所有改动对运行时行为零影响。
- 收益(与 [可维护性批次](../simplification/2026-09-16-maintainability-pass.md) 互补:那批治代码结构与测试,本批治构建与 CI 基建):Go 写法收敛到单一惯例(`io.Discard`/`common/atomic` 泛型包装),新代码有明确规范可循;构建流程删掉 cgo 死遗产,vet 真正生效,加平台从改 37 处变成改模板;镜像与 CI 不再依赖漂浮 tag 与第三方 `@main` action,权限面最小化;所有改动对运行时行为零影响。本 fork 后来不再推 Docker Hub,见 [drop Docker Hub push](2026-09-17-drop-docker-hub-push.md)。
- 本批钉住的 `go 1.20` 下限仍是当时的正确取舍。另开的发布窗口：Stage 1 见 [Stage 1 编译器下限上移到 Go 1.24](./2026-09-18-go-compiler-floor-1.24.md)；Stage 2 见 [2A 在 metacubex/sing-tun 补 go 栈与 SpliceSocket](../architecture/2026-09-18-go-stack-sing-tun-2a.md)。不改写本篇 Decision。
