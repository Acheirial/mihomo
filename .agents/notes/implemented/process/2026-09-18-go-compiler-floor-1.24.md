# Agent Note: Stage 1 编译器下限上移到 Go 1.24

Status: implemented

## Problem

`go.mod` 长期写 `go 1.20`，而默认发布已经用 MetaCubeX 修补的 Go 1.26 出包。下限与实际编译器分叉：语言/依赖被 1.20 钉死，挡不住用户用新编译器出包，却挡住后续 TUN 追平窗口。Win7 / macOS 10.13 兼容面已经由默认 1.26 fork 构建承担，再为它们保留 `go120`–`go123` 矩阵 job 只增加 CI 面。

本篇只落地编译器下限。不 bump `sing` / `sing-tun`，不改 YAML，不改运行时。Stage 2 已落地，见 [2A 在 metacubex/sing-tun 补 go 栈与 SpliceSocket](../architecture/2026-09-18-go-stack-sing-tun-2a.md)。[现代化批次](./2026-09-16-modernization-pass.md) 当时钉住 1.20 仍是对的；本篇是另一次发布窗口，不改写那条 Decision。

## Decision

编译器下限是 **Go 1.24**，不是 1.25，也不是继续停在 1.20：

- 根 `go.mod` 与 `test/go.mod` 的 `go` 行是 `1.24`。`require` 版本不动，含 `github.com/metacubex/sing-tun v0.4.24` 与 `x/*` 钉住块。钉住块注释改为「last pins that still compile; bump with a dedicated window」。
- `.golangci.yaml` 与 `test/.golangci.yaml` 的 `staticcheck.go` 对齐 `1.24`。
- `test.yml` 矩阵只留 `'1.26'` `'1.25'` `'1.24'`。
- `build.yml` 删掉 `goversion` 为 `1.20`/`1.21`/`1.22`/`1.23` 的 job；保留空 goversion（默认 1.26 MetaCubeX fork）、`go124`、`go125`、custom loong64。Win7 / macOS 10.13 仍走默认 1.26 fork 构建。
- 文档：README「Go 1.24 or newer」；AGENTS 矩阵 `1.24–1.26`，代码可用 1.24 语言；FAQ 二进制标签改为现存的 `go124`/`go125`。`flake.nix` 用 `buildGo124Module`。

本批 NEVER 改 Go 源码行为、YAML、默认 TUN 栈，也 NEVER bump sing / sing-tun / x/*。

## Alternatives considered

- **停在 go 1.20，只删 CI 旧矩阵** — 最强论据是 Win7 / 老 macOS 与一组 pinned 依赖都不用动，CMFA 出包路径零风险。否决：默认构建已经是 1.26 fork；继续把 `go` 行钉在 1.20 只挡住后续 pin / 语言特性，救不了已由 fork 承担的兼容面。Stage 1 的回滚是改回 `go` 行，无运行时残留。
- **一次拉到 Go 1.25 + 解锁 x/* / sing-tun** — 最强论据是少一次发布窗口，马上能升依赖。否决：提案明确 Stage 1 MUST NOT 拉到 1.25，MUST NOT 与 sagernet 换包同提交；CMFA 与 pinned 依赖要单独签字。本批只动编译器下限。
- **只改 go.mod、不砍 go120–go123 产物** — 最强论据是老内核 / Catalina 用户仍能下载对应标签。否决：`go 1.24` 之后那些 job 编的是已声明不支持的工具链；继续出 `go120`/`go123` 会让文档与 module 下限打架。兼容面改由 1.26 fork 默认构建承担。

## Consequences

- **收益**：语言与 CI 下限和实际发布编译器对齐；后续 pin / TUN 窗口不再被 1.20 卡住；构建矩阵少一批必红的旧工具链。
- **代价**：官方不再出 `go120`–`go123` 二进制；Linux 2.6.32–3.1 与只靠旧标签的用户必须改用默认 fork 构建或自编译。`x/*` 等钉住块仍是 1.20 时代最后兼容 tag，升它们要另开窗口。
- **重访**：CMFA gomobile / NDK 在 1.24 上出包失败则把 `go` 行改回 1.20。Stage 2 已落地为 2A 本地 replace + `stack: go`，见 [2A 在 metacubex/sing-tun 补 go 栈与 SpliceSocket](../architecture/2026-09-18-go-stack-sing-tun-2a.md)；默认栈仍 gvisor，不在本篇加码。

## Verification

- `grep -n "go 1.20" go.mod test/go.mod` 为空。
- `test.yml` 无 1.20–1.23 矩阵项；`build.yml` 无 `goversion` 1.20/1.21/1.22/1.23 job。
- README 写 Go 1.24；`go.mod` 里 `sing-tun` 仍为 `v0.4.24`。
- 无 Go 源码行为改动。
