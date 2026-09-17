# Agent Note: fork CI 不再推 Docker Hub

Status: implemented

## Problem

`build.yml` 的 `Docker` job 在每次 Alpha 推送后登录 `docker.io` 并 push 多架构镜像。本 fork 没有 `DOCKER_HUB_USER` / `DOCKER_HUB_TOKEN`,job 固定在 `Username and password required` 失败,把整条 Build 打红。二进制交叉编译(含 Go 1.20–1.26 的 Win7/老 macOS 目标)其实全绿。不删的话每次推送都要为一项用不上的分发渠道付 CI 失败。

## Decision

删除 `build.yml` 的 `Docker` job 和只给它用的 `env.REGISTRY`。`Dockerfile` 保留,本地/手动构建镜像仍可用。GitHub Release / Prerelease 上传路径不动。

这是对 [现代化批次](2026-09-16-modernization-pass.md) 里 CI 形状的收窄:那边把三个 job 的权限收到 `contents`/`packages`,Docker 删掉后 `packages` 写权限不再出现在 workflow 里。

## Alternatives considered

- **给 Docker job 加 `if: secrets.DOCKER_HUB_TOKEN != ''`**(最强论据:上游同步时少冲突,配了 secret 就能重新推):否决,GitHub Actions 对 secrets 的条件判断不可靠(未设置的 secret 在部分上下文里仍是空字符串,但文档明确不建议用 secret 做存在性门控);本 fork 明确不推 Docker Hub,留一个永远跳过的 job 只会继续让人以为 Build 还负责镜像。
- **改推 GHCR(`ghcr.io`),用 `GITHUB_TOKEN`**(最强论据:fork 已经有 `packages: write`,不必另配 Docker Hub 账号):否决,用户要求把推 Docker 删掉,不是换仓库;GHCR 分发是另一个产品决定,需要镜像名、权限和文档一起改,不塞进这次止血。

## Consequences

- 收益:Build 工作流的结论只反映交叉编译和 Release 上传;不再被缺失的 Docker Hub 凭证打红。
- 代价:本 fork 不再自动出多架构容器镜像。需要镜像时必须本地 `docker build` 或另开分发渠道。上游若改 Docker job,这边已经没有对应块,merge 冲突会显示为「整段新增」。
- 重访条件:若决定恢复自动镜像,优先 GHCR 而不是再要一套 Docker Hub secret;那时再写新笔记,不要把本篇 Decision 改写成「推 GHCR」。

## Verification

- `.github/workflows/build.yml` 无 `Docker:` job、无 `docker/login-action`、无 `DOCKER_HUB_*`。
- `jobs.build` / `Upload-Prerelease` / `Upload-Release` 仍在。
- `Dockerfile` 文件仍在仓库根目录。
