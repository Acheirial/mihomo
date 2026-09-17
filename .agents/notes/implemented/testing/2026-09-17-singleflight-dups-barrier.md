# Agent Note: singleflight 并发测试改等 dups,不再赌调度

Status: implemented

## Problem

`fd333c29` 给 `common/singleflight` 补的并发测试在 GitHub Actions 上偶发失败,矩阵里 1.20–1.26、Linux/Windows/macOS 都会抽中,看起来像「部分 Go 版本挂」。实际失败数字是 `TestDo_DeduplicatesConcurrentCallers` 期望 fn 跑 1 次实际 8 次、`TestDo_EachKeyRunsOnce` 期望 5 次实际 8 或 16 次。同一 job 第一步过、带 `with_gvisor` 的第二步挂,是典型调度 flake,不是工具链不兼容。

根因是 `arrivedBarrier` 在进 `Do` 之前加计数。计数满只说明大家都「准备去调 `Do`」,不保证已经进了 `Do`。leader 看到计数满立刻干完并把 key 从 map 删掉,还没进 `Do` 的调用者就会各自再跑一遍。开发机 200 次也能绿,CI 核多、调度更乱才会打中。

不修的话 Test 工作流会持续红,而且会把人带去查 Go 版本矩阵。

## Decision

并发会合改等 `Group.m[key].dups`,不在进 `Do` 之前用到达计数。`waitForDups` 由 leader 在 `fn` 里调用:此时 key 一定还在飞行中,`dups` 每多一个真正进入 `Do` 的重复调用者加 1,leader 等到 `dups >= total-1` 才执行真正的 work。`callConcurrent`、`TestDo_EachKeyRunsOnce`、`TestDo_PanicIsRePanickedToCallers` 共用这条路径。被测契约不变:同一 key 飞行中 fn 只跑一次,所有 caller 看到 `shared == true`。

这是对 [可维护性批次](../simplification/2026-09-16-maintainability-pass.md) 补测的修正,不改 `singleflight` 实现。

## Alternatives considered

- **先起 leader、堵住 fn、再起其余 caller**(最强论据:`golang.org/x/sync/singleflight` 的 `TestDoDupSuppress` 就是这个顺序,不读未导出字段):否决,本仓库测试与实现同包,读 `dups` 是合法且更短的会合;先起 leader 还要额外的「leader 已进 fn」信号,两条通道不如一条 `dups`。
- **保留 `arrivedBarrier`,只把 `Add` 挪到 `Do` 返回之后**(最强论据:改动面最小,计数器语义看起来更「调用者已到达」):否决,`Do` 返回时 leader 已经结束,计数永远凑不齐,会死等;问题不在计数器位置,而在会合点必须落在「已经进入飞行中的 call」。
- **放宽断言为 `calls >= 1` 或去掉 `shared == true`**(最强论据:实现在无并发时本来就允许每人各跑一次,测「最终有结果」更稳):否决,那测不到 duplicate suppression,补测的目的就是钉死「飞行中只跑一次」。

## Consequences

- 收益:Test 矩阵不再被这条 flake 随机打红;失败不再被误读成 Go 版本不兼容。
- 代价:测试读未导出的 `mu`/`m`/`dups`,实现若改成不分发 `dups` 的结构,测试会编不过——这是期望的耦合,会合本来就要钉在「重复调用者已进入 Do」这个事实上。
- 重访条件:若 `call.dups` 被改掉或 `Do` 不再同步执行 `fn`(改成一律 `DoChan` 那种另起 goroutine),`waitForDups` 必须一起改。

## Verification

- `go test ./common/singleflight/ -count=200` 与 `-race -count=20` 绿。
- CI 失败现场(run `35225660228`)只红 `common/singleflight` 这两个用例;修后同一断言(`calls == 1` / `calls == 5`,`shared == true`)必须继续成立,不能靠放宽过关。
