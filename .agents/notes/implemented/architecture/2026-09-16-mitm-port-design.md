# Agent Note: mihomo MITM 功能移植设计

Status: implemented

## Problem

mihomo 的 sniffer 只能读取 TLS ClientHello 中的 SNI（不解密），HTTPS 流量对规则系统是不可见的密文。用户要求参照 Xray-core 的 MITM 实现，把 TLS 中间人解密能力加入 mihomo 并编写文档。不做的话，基于域名的精细规则/改写只能依赖 SNI 嗅探，无法触及解密后的内容。

## Decision

参照 Xray `proxy/dokodemo` + `transport/internet/tls/config.go` 的组成方式（透明入站终止客户端 TLS、按 SNI 现场签发叶子证书、出站重新建立 TLS），落地到 mihomo 的 HTTP/Mixed 入站 CONNECT 分支：

- **挂钩点**：`listener/http/proxy.go` 的 CONNECT 分支——在写出 `200 Connection established` 之后、`inbound.NewHTTPS` 之前。Mixed 入站经 `http.HandleConn` 复用同一路径，一个钩子覆盖两种入站。
- **新组件 `component/mitm`**：`CA`（生成/加载 CA 密钥对，模板带 `IsCA`/`KeyUsageCertSign`，可持久化到 `C.Path.GetAssetLocation`）+ `LeafFor(sni)`（按 SNI 缓存签发 1 小时叶子证书，RWMutex 缓存，访问时清过期）+ `Interceptor.Wrap`（用 `component/sniffer.ReadClientHello` 剥 ClientHello、域名匹配、双向 TLS 握手，返回解密连接；不匹配则原样透传）。
- **配置面**：`LC.AuthServer` 增加 `mitm:` 节（`enable`/`hosts`/`ca-certificate`/`ca-private-key`/`store-ca`），经 `HTTPOption`/`MixedOption` 的 `inbound:"mitm,omitempty"` 标签解码，per-listener 作用域与现有 TLS 证书配置惯例一致。
- 解密流不作为第二条连接重新进入 tunnel——与 Xray 单 Link 设计一致，避免 peeked-byte 重放和 tracker 记账的侵入。

## Alternatives considered

- **tunnel 级 splice（candidate B，覆盖 TUN/socks）** — 最强论据是覆盖所有入站类型；否决因为 `handleTCPConn` 的 peeked-byte 重放（tunnel.go:597-604）和 `statistic` 记账都要动，侵入面大；CONNECT 级已覆盖用户要的 HTTP/Mixed 场景，tunnel 级留待复用同一组件扩展。
- **Xray 的 "frommitm" 出站哨兵** — 最强论据是 Xray 已验证该模式；否决因为 mihomo 按连接经规则路由而非按出站 streamSettings 配置，哨兵语义映射不上；服务端 TLS 改在 Interceptor 内构建。
- **复用 `ca.NewTLSKeyPairLoader` 作 CA** — 最强论据是零新增代码；否决因为它生成的是服务端证书（无 IsCA/KeyUsageCertSign），不能签发叶子；只复用其 keygen 脚手架，扩展模板。

## Consequences

- **收益**：HTTP/Mixed 入站获得完整 TLS 解密能力，叶子证书按 SNI 动态签发且有内存缓存；不匹配主机自动透传，零配置时行为与现在完全一致。
- **代价与已知上限**：不支持客户端证书固定（pinning）场景；QUIC/UDP 不在范围内（与 Xray 相同）；tunnel 级（TUN/redir）拦截未覆盖——若需要 TUN 场景的 MITM，必须重访本决定并把 Interceptor 接入 `handleTCPConn`。

## Verification

- `go test ./component/mitm/...`：CA 生成 → 双 SNI 签发 → x509 链验证、过期清理、域名匹配放行/拒绝。
- 集成冒烟：MITM 监听器 + 仅信任 MITM CA 的客户端 CONNECT，断言叶子证书 Subject 与真实上游握手成功。
- `go build ./...`、`go vet ./...` 全绿。
