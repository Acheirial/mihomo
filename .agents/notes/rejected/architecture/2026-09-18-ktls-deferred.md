# Agent Note: kTLS deferred (no public KernelTx; no sing-box ktls copy)

Status: rejected — Go 1.24 crypto/tls 没有公开 KernelTx，StreamStack 不能无 linkname 包装

<!--
  【维护提醒】
  1. 冻结不动：不改写为对立面，不随代码演进更新事实；结论就在 Status 行。
  2. 仅在"防止重犯"期间保留：被否方案依赖的旧技术/API 已彻底移除、后人绝无可能
     再提时，把这一篇删掉。
  3. 价值被新笔记吸收时，按 references/archiving.md 的合并删除流程处理。
-->

## Problem

Linux 上 TLS 记录层仍走用户态：握手之后每条记录仍由 `crypto/tls` 加解密，再经 socket 写出。DIRECT 路径上 [splice](../../../implemented/architecture/2026-09-18-go-stack-sing-tun-2a.md) 已落地，但 splice 只能搬运明文或已封装好的字节，不能替用户态 TLS 卸掉记录层。sing-box 的做法是 `go:linkname` 偷 `tls.Conn` 内部，再 `setsockopt(TCP_ULP / TLS_TX)` 把 TX/RX 交给内核 kTLS。

mihomo 已升 [Go 1.24 地板](../../../implemented/process/2026-09-18-go-compiler-floor-1.24.md)。StreamStack 已有 session cache，并与 uTLS / ECH / [early-header](../../../implemented/architecture/2026-09-18-protocol-early-header.md) 共用同一条握手出口。在这条栈上再包一层内核 TLS，没有公开 API 就只能拆内部或抄 sing-box 的 linkname。

本窗口 **没有** 落地 kTLS：仓库里没有 `common/ktls`，也没有对 `KernelTx` 的调用。后人不得把「已升 1.24 + 已 splice」读成「kTLS 已开」。

## Proposal

整包移植 `sing-box/common/ktls` 进 mihomo：Handshake 成功后在 `tls.Conn` 外包一层 kernel TX/RX，Linux 上走 `TCP_ULP` + `TLS_TX`/`TLS_RX`，其余平台 no-op。期望 DIRECT / 出站 TLS 的记录加解密从用户态挪到内核，与已有 splice 叠成「内核搬字节」。

该提案冻结：未合入任何 Go 源。

## Alternatives considered

- **移植 sing-box ktls + `go:linkname`** — 当时唯一已知能真正挂上 kTLS 的路：能拿到记录层密钥并 `setsockopt`。否：Go 1.24 的 `crypto/tls` **没有** 公开 `KernelTx`；linkname 随 stdlib 漂移；包一层会拆开 StreamStack / uTLS / ECH / session cache 的握手出口。sing-box 自己仍 gated `go1.25 && badlinkname`，不是 1.24 地板上的可抄件。
- **只 `setsockopt(TCP_ULP)`，不碰 `tls` 内部** — 无 GPL 拷贝、无 linkname，看起来干净。否：ULP 只把模块挂到 TCP 套接字上；没有记录层密钥，内核无法加解密，不能真正 kTLS。
- **等上游 `crypto/tls` 公开 `KernelTx`** — 干净 API，StreamStack 可在 Handshake 后包装而不偷内部。否：1.24 地板窗口已关，本窗口不能把性能路径挂在未发布符号上。

## Risks

重提条件（同时满足才允许再开提案，否则 never 在本仓库抄 sing-box ktls）：

- 编译地板 **Go ≥ 1.25**，且 `crypto/tls` **公开** `KernelTx`（或等价、稳定、无需 `go:linkname` 的 API）；**或**
- 能在 **不** linkname、**不** 拆 StreamStack / uTLS / ECH / session cache 握手出口 的前提下把密钥交给内核。

本窗口零代码。未满足上述条件时，不得新增 kTLS 包装、不得 `go:linkname` 进 `crypto/tls`。
