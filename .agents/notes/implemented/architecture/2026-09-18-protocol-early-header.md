# Agent Note: 协议头延迟到首写 + 每栈 TLS session cache + H2 连接池

Status: implemented

## Problem

对照 sing-box，mihomo TCP 流协议在握手热路上多 1-RTT：

- Trojan 在 `streamConnContext` 里同步 `WriteHeader`。未发 payload 就断开时服务端已经收到密码头；TFO / TLS 0-RTT / WS early-data 无法把协议头和首包合进同一 record。
- VLESS outbound 走 `Client.StreamConn`，但从不走 early API。现有 `Conn.sendRequest` 已经把头发到首次 Write，Vision / `encryption=` 必须在协议头之前完成。
- VMess / SS 只在底层 `NeedHandshake` 时才 `DialEarlyConn`。纯 TCP + 已完成 TLS 时 `NeedHandshake==false`，协议头仍同步写出。
- `transport/vmess/tls.go` 的 `tls.Config` 没有 `ClientSessionCache`（仅 shadowquic ZeroRTT 挂了 capacity=1）。同 SNI 二次握手每次全量 Handshake。
- `network: h2` 每次 `StreamH2Conn` 新建 `http.Transport` + `NewClientConn`，流结束关掉底层 TCP。H2 节点等价于每条流量一次 TLS。

[2026-09-17-perf-parity](./2026-09-17-perf-parity.md) 本轮不做 splice，也没动协议头。[2026-09-17-transport-protocol-security-stack](./2026-09-17-transport-protocol-security-stack.md) 把 TLS/WS/H2/gRPC 抽进 `StreamStack`，但协议层仍是「Wrap 完立刻写头」，且明确「h2 仍无多路复用」。[2026-09-18-inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) 否决了 `bufio.CopyConn` 包 Relay，本批不能把 lazy conn 再套一层等双 EOF 的 Copy。

## Decision

协议头延迟到首次 Write；TLS session cache 和 H2 池按 outbound / StreamStack 一份，不改 YAML。

### 协议头（lazy / DialEarly）

- Trojan：`streamConnContext` 不再调用 `WriteHeader`。SS overlay 仍先 `ssCipher.StreamConn`，然后返回 `transport/trojan.ClientConn`。`ClientConn.Write` / `WriteBuffer` 把密码头、command、SOCKS 地址和 payload 合写；`NeedHandshake()` 在头未写时为 true。未 Write 就 Close：服务端收不到头。UDP 仍是 `CommandUDP`，头挂在首次包上。
- VLESS：继续用现有 `client.StreamConn` → `Conn.sendRequest`。`sendRequest` 本来就在首次 Write 才发头，outbound 不再另做同步写出。`encryption.Handshake` 与 Vision `newConn` 仍在返回 lazy conn 之前完成。不替换为 sing-vmess 客户端。
- VMess / SS：默认 `DialEarlyConn` / `DialEarlyPacketConn` / `DialEarlyXUDPPacketConn`，不再用 `NeedHandshake(c)` 选 DialConn。obfs / shadowtls / restls / jls 仍先 Wrap 再 early。

### TLS session cache

- 每个 `NewStreamStack` 创建一份 `tlsC.NewSharedClientSessionCache(64)`，挂在该栈的 `vmess.TLSConfig.SessionCache`。禁止全局 cache（会串 SNI）。
- `ToStdConfig` 把 cache 接到 `tls.Config.ClientSessionCache`。`UConfig` 识别 `tlsSessionCache` 后接到 utls 的同构 cache。两端存的是 `SessionState.Bytes()` 序列化票，避免 `metacubex/tls` 与 `utls` 的 `ClientSessionState` 类型不相容。
- `StreamTLSConn` 仍立即 `HandshakeContext`。TLS 1.3 票在握手返回后的 NewSessionTicket 读路径才入 cache。

### H2 池

- `network: h2` 现在 `Session()==true`。`StreamStack.Dial` 走池，不再每次 `Wrap` 新建 Transport。
- `vmess.NewH2Transport` 持有一份 `*http.Transport`（h2c Protocols + 调用方已完成 TLS 的 DialTLSContext）。`StreamH2Conn` 对同一 Transport `RoundTrip` PUT 流。流 `Close` 只关 pipe / response body，不关底层 TCP。节点 `StreamStack.Close` 调 `httputils.CloseTransport`。
- YAML `h2-opts` 字段不变，不加 idle-timeout / max-connections。H2 与 smux 叠加会变差，本批不禁止，只在本笔记警告。

### 不变

- YAML 字段、默认 mux 关闭、ssr / hy1、Box outbound registry、kTLS、`C.Dialer` 签名。gun `Conn.Write` 已走 `WriteBuffer` 的 headroom 前缀（不再 pool.Get+整包 copy）；kTLS 仍否决，见 [kTLS deferred](../../rejected/architecture/2026-09-18-ktls-deferred.md)。

## Alternatives considered

- **用 `bufio.CopyConn` 包 Trojan lazy conn** — 最强论据是省手写 ClientConn，头和 payload 自然合写。否决：CopyConn 等两个方向都 EOF 才返回，而 `Relay` 契约是任一端结束就关双端；已在 [inbound-stack-and-perf-completion](./2026-09-18-inbound-stack-and-perf-completion.md) 回退。
- **删 SSR / Hy1** — 最强论据是少维护热路径。否决：Clash YAML 兼容禁止删 type。
- **默认开 mux** — 最强论据是隔过重手 TLS。否决：破现网行为；面板 smux 统计含义变；与 h2/grpc 叠加更差。
- **把 VLESS 客户端换成 sing-vmess 拿 DialEarlyConn** — 最强论据是与 VMess 统一 EarlyXUDP。否决：Vision + encryption 在树内路径更完整；换库风险大于延迟写头。现有 `Conn.sendRequest` 已经是 early。
- **全局一份 `tls.NewLRUClientSessionCache`** — 最强论据是所有节点共享票、实现最小。否决：不同 SNI / 节点会串 session；必须每 StreamStack 一份。
- **uTLS 直接赋 `config.ClientSessionCache`** — 最强论据是字段同名。否决：`metacubex/tls` 与 `utls` 的 `ClientSessionState` 类型不相容，编译失败。序列化票中转是唯一能两边 resume 的接口。

## Consequences

- **收益**：未发 payload 的 Trojan 连接不再把密码头打到服务端；纯 TCP VMess/SS 协议头可与首 payload 合写；同 SNI 二次握手能吃 session ticket；同一 h2 outbound 的并发流复用一条 TCP+TLS。
- **代价与已知上限**：TLS 1.3 票要等 NewSessionTicket（短连接若握手后立刻关，第二次仍可能全握手）。H2 池在首条流尚未建立 idle conn 时并发 Dial 仍可能开第二条 TCP——Transport 自己的连接缓存只在第一条 RoundTrip 完成后生效。H2 + smux 双重 mux 延迟可能变差，重访信号是用户同时开 `network: h2` 和 `smux`。gun Write 经 WriteBuffer 仍 Flush（h2 流需要）。kTLS 未做，见 [kTLS deferred](../../rejected/architecture/2026-09-18-ktls-deferred.md)。

## Verification

- `GOTMPDIR=/home/dev/tmp go test ./transport/trojan/ ./transport/vmess/ ./adapter/outbound/ ./component/tls/ -count=1`：trojan / vmess / outbound ok；tls 无测试文件。
- `TestClientConnCloseWithoutWriteSendsNoHeader`：net.Pipe 上 Close 未 Write，服务端读到 EOF 且无头字节。
- `TestStreamTLSConnResumesWithPerConfigCache`：同 `SharedClientSessionCache` 第二次握手 `DidResume==true`（第一次 Read 排空 NewSessionTicket）。
- `TestH2PoolReusesOneTCPConn`：同一 `NewH2Transport` 两条流 `dials==1` 且 server `StateNew==1`。
