# Agent Note: fork CI 不再推 Docker Hub

Status: implemented

## Problem

`Acheirial/mihomo` 的 Build workflow 在所有 Go 交叉编译 job 成功之后,无条件跑 `Docker` job,用 `secrets.DOCKER_HUB_USER` / `DOCKER_HUB_TOKEN` 登录 `docker.io` 再 buildx 推多架构镜像。这个 fork 没有这两项 secret,登录步骤固定报 `Username and password required`,整条 Build 变红。二进制其实已经编过了,红灯来自发布侧。继续留着会让「编译是否过」和「有没有 Docker Hub 账号」绑死。

## Decision

`.github/workflows/build.yml` 删除整个 `Docker` job 以及只给它用的 `env.REGISTRY`。交叉编译、Upload-Prerelease、workflow_dispatch 的 Upload-Release 保留。`Dockerfile` 仍在仓库里,本地/手工构建不受影响。本 fork 不维护 `docker.io/acheirial/mihomo`。

与 [现代化批次](2026-09-16-modernization-pass.md) 的关系:那批钉的是 action 版本和权限面,没决定「这个 fork 要不要推镜像」。本篇才是那条发布路径的退出。

## Alternatives considered

- **`if: secrets.DOCKER_HUB_TOKEN != ''` 有凭证才跑**(最强论据:上游 workflow 还能原样留着,哪天配了 secret 自动恢复推送):否决,这个 fork 明确不推 Docker Hub,留一个永远 skip 的 job 还是在假装有这条发布路径;secret 判断在 GitHub Actions 里也容易写成「空字符串仍进 job」。
- **配上 Docker Hub secret 继续推**(最强论据:跟上游 MetaCubeX 一样有容器分发,用户 `docker pull` 就能用):否决,没有要维护的镜像仓库,也没有账号;为了绿灯去申请 Hub 账号会把发布面扩到本 fork 并不想承担的地方。

## Consequences

- **收益**:Build 红绿只反映交叉编译和 GitHub Release 上传;不再被缺失的 Hub 凭证打死。
- **代价与已知上限**:没有自动多架构镜像。有人要容器时必须自己 `docker build`,或从 GitHub Release 拿二进制。若以后要恢复官方镜像分发,把 job 从本篇对照的上游 `build.yml` 加回来,并先有可写的 registry 凭证。

## Verification

`build.yml` 里不再出现 `docker/login-action`、`DOCKER_HUB_`、`jobs.Docker`。下一次 Alpha push 的 Build run 不应再有名为 `Docker` 的 job。
