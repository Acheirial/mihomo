# Agent Note: 传输层-协议层-安全层可组合栈

Status: implemented

Supersedes the “transport/ 三份 ws/TLS” leftover item recorded in [2026-09-17-perf-parity](../../implemented/architecture/2026-09-17-perf-parity.md)：那三份握手与四份互斥检查已被 `StreamStack` 与 `vmess.StreamTLSConn` 收敛。

## Problem

出站把传输、安全和协议写进同一个 `StreamConnContext`。`type: vmess|vless|trojan` 各自复制 ws/http/h2/grpc/TLS 接线；安全层入口是 `transport/vmess.StreamTLSConn`，但 WebSocket 内部、HTTP/SOCKS5、SS/Snell plugin 又各走一套握手。用户配置面因此被协议绑死：VLESS 吃不到 mkcp，Trojan 吃不到 http/h2/xhttp，VMess 吃不到 xhttp。

`adapter/parser.go` 有 29 个 YAML `type`，没有别名。hysteria 与 hysteria2、TUIC v4/v5、SSR 与 SS、gost-relay 与 socks5 是不同 wire，不能当重复删。真正重复的是接线代码，不是协议 type。

不拆层：新传输必须在三个 outbound 各贴一份 switch；安全层改动要同时修 websocket.go、http.go、socks5.go、ss plugin。旧 YAML 继续能解析，但不能自由搭配。

## 决策

保持全部 YAML `type` 名、字段、默认值。拆的是层，不是配置面。

拨号顺序 MUST 是：

```
C.Dialer（TFO/MPTCP/iface/dialer-proxy）
  → Security.Handshake（none | tls/uTLS/ECH | REALITY | ShadowTLS | Restls | JLS | TLSMirror）
  → Transport.Wrap 或 SessionTransport.Dial（tcp | ws | http | h2 | grpc | xhttp | mkcp | mekya）
  → Protocol.Stream（vmess/vless/trojan/ss/… 只写协议头）
  → 可选 smux（已有 NewSingMux 后包装）
```

SessionTransport（grpc/xhttp/mekya/mkcp/kcptun）内部用同一套 Security，不另建握手。QUIC 应用层（hysteria/hysteria2/tuic/shadowquic/masque）自带 TLS，不走这条 TCP 栈。L3 overlay（wireguard/openvpn/zerotier）继续用 `newIPStack`，不塞进 stream 栈。

### 配置兼容

- `network`、`ws-opts`/`http-opts`/`h2-opts`/`grpc-opts`/`xhttp-opts`/`mkcp-opts`/`mekya-opts`、`tls`、`sni`/`servername`、`*-opts`、`plugin`/`plugin-opts`/`obfs-opts` 全部保留。
- `network: httpupgrade`（分享链会写出这个值，outbound switch 当前落到裸 TCP）规范化为 `ws` + `ws-opts.v2ray-http-upgrade=true`。这是修兼容坑，不是新配置面。
- 不引入第一期 `transport:` 新块。旧字段就是组合面。
- SS/Snell 继续用 plugin/obfs-opts 方言，内部接到同一 Security，不把字段改成 tls.md 的 `shadow-tls-opts`。
- 入站 YAML（`ws-path`/`grpc-service-name`/`xhttp-config`/`mkcp-config.enable`）本批不改。
- Trojan 无 `network` 仍强制 TLS；h2 仍无多路复用；gun `max-connections` 默认 1。

### 本批删除 / 合并（代码，不是 type）

MUST 删的是复制路径，YAML type 全部保留：

1. vmess/vless/trojan 三份 `StreamConnContext` 的 ws/http/h2/default TLS 分支 → `outbound.StreamStack`。
2. WebSocket 内部 TLS（`transport/vmess/websocket.go` `c.TLS` 分支）→ 调用方先 `StreamTLSConn`，WS 只做 Upgrade。uTLS 的 `BuildWebsocketHandshakeState`（ALPN 锁 `http/1.1`）迁进 `StreamTLSConn` 的 `WebsocketALPN` 开关。
3. HTTP/SOCKS5 的 `tls.Client` 直握手 → `StreamTLSConn`。
4. `checkExclusiveSecurityModes` 四处拷贝合成一个函数。
5. 分享链 `httpupgrade` 写出错误 `network` 值。

MUST NOT 删：ssr、gost-relay、hysteria1、tuic v4、snell、任何 YAML type。ssr/hy1 标遗留。gost-relay 补文档。

### 自由搭配（第一期范围）

把已经共享的 helper 接到所有 V2Ray 协议：

| network | vmess | vless | trojan |
|---|---|---|---|
| tcp/ws/grpc | 已有 | 已有 | 已有 |
| http/h2 | 已有 | 已有 | 本批接上 |
| xhttp | 本批接上 | 已有 | 本批接上 |
| mkcp/mekya | 已有 | 本批接上 | 本批接上 |

协议层约束仍生效：VLESS vision flow 与 mkcp 的兼容由现有握手决定；失败 MUST 返回明确错误，不得静默降级。SS 不吃 xhttp/mkcp。

### 包拓扑

- `adapter/outbound/streamstack.go`：`StreamStack`、`StreamStackOption`、`NormalizeNetwork`、`checkExclusiveSecurityModes`。
- `transport/vmess` 暂留 `TLSConfig`/`StreamTLSConn`/`StreamWebsocketConn`/`StreamHTTPConn`/`StreamH2Conn`（调用面太宽，本批不搬包）。命名去耦留给后续。
- `IPStackOption`/`newIPStack` 仍在 `wireguard.go`：L3 抽取与 stream 栈正交，本批不做。

## Alternatives considered

- **按 YAML type 删协议（ssr/gost-relay/hysteria1）** — 最强论据是维护面立刻变小，ssr 无 inbound/集成测，gost-relay 无文档。否决：用户硬约束是配置兼容；parser 与订阅仍识别这些 type，删实现等于让旧配置启动失败。本批只标遗留。
- **引入独立 YAML `transport:` / `security:` 块，废弃 `network`/`tls`** — 最强论据是与 sing-box 对齐、组合面更干净。否决：破坏现有配置与面板/订阅生态。旧字段就是组合面。
- **把 Transport 做成 `C.Dialer` 装饰器** — 最强论据是与 dialer-proxy 同一条链。否决：`proxydialer` 已经占用 Dialer 链；grpc/xhttp 自管连接池，塞进 Dialer 会把多路复用和 TFO 搅在一起。
- **本批把 `transport/vmess/{tls,websocket,http,h2}.go` 搬到 `transport/stream/`** — 最强论据是包名不再误导。否决：gun/anytls/trusttunnel/v2ray-plugin/gost 都 import 这些符号，搬家是机械重命名，不增加可组合性，和接线抽取抢同一冲突面。
- **SS plugin-opts 改成 tls.md 的 shadow-tls-opts** — 最强论据是安全层只有一种写法。否决：SIP002/面板生态用 `plugin`/`plugin-opts`；改字段等于破 SS 配置。内部走 `StreamTLSConn` 即可。

## 验证标准

- 旧 YAML（含 `type: ssr`、`type: hysteria`、`type: gost-relay`、`network: ws` + `tls: true`、`plugin: shadow-tls`）解析与拨号语义不变。
- `network: httpupgrade` 与 `type=httpupgrade` 分享链落到 WebSocket HTTP Upgrade，不再走裸 TCP。
- vmess/vless/trojan 对 http/h2/ws/grpc/xhttp/mkcp/mekya 走同一 `StreamStack`；协议头仍在各自 `streamConnContext`。
- 安全模式互斥错误字符串保持 `security modes are mutually exclusive: …`。
- `go test ./adapter/outbound/ ./transport/vmess/ ./common/convert/ ./listener/inbound/ -count=1` 绿。
- 笔记树与格式脚本通过。

## Consequences

- WebSocket 改走 `StreamTLSConn` 后，uTLS ALPN 必须仍锁 `http/1.1`，否则指纹/CDN 握手会偏。用 `WebsocketALPN` 开关覆盖，不用调用方记得改 NextProtos。
- 给 VLESS/Trojan 接 mkcp/xhttp 可能撞上服务端不支持的组合。工厂 MUST 在已知不兼容处返回错误（xhttp h3 仍禁 camouflage；mkcp 仍禁 ShadowTLS/Restls/JLS）。
- 入站 YAML 本批不动，出站自由搭配在入站侧仍可能对不上。这是已知上限：入站对齐另开笔记。
- ssr/hy1 标遗留但不删，维护成本仍在。重访信号：上游删除对应协议或订阅生态归零。

## Verification

- `go build ./adapter/... ./transport/... ./common/... ./listener/... ./config/... ./hub/... ./tunnel/... ./dns/...` 全部退出码 0；`go vet ./adapter/outbound/ ./transport/v2ray-plugin/ ./transport/gost/` 无输出。
- `go test ./adapter/outbound/ -count=1`（含 Vless/Vmess/Trojan/SS/Snell/Socks5/Http/AnyTLS 用例）与 `go test ./common/convert/ ./transport/vmess/ ./transport/v2ray-plugin/ ./transport/gost/ -count=1` 全绿。
- `npx tsx …/verify-agent-note-tree.ts`、`verify-agent-note-format.ts`、`verify-archived-agent-notes.ts` 均 ok。
- 互斥错误字符串实测不变（`checkExclusiveSecurityModes` 产出原文本）。
- 注：`go build ./...` 与 `go test ./...` 在本机两次卡在 futex 死锁（非代码问题，D 状态无 IO 前进），故改按包树分批验证；分包结果即上述。
