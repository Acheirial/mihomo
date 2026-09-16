# Agent Note: MITM HTTP 重写规则引擎移植

Status: implemented

## Problem

mihomo 的 MITM 组件([mitm-port-design](../architecture/2026-09-16-mitm-port-design.md))只做 TLS 解密,解密后的 HTTP 内容对用户不可操作:无法按 URL 正则拒掉请求、改 header、改 body。quirktiva(plus 分支)有一套成熟的 Surge 风格重写规则引擎(`mitm/` 约 1300 行,17 种类型),但它的架构是"劫持整条连接进自建 ReverseProxy",与 mihomo "解密流继续走管线"的设计冲突。本篇记录移植时的适配决策。

## Decision

- **引擎独立成包 `component/mitm/rewrite/`**:类型枚举(Type 16 种)、`ParseRewrite` 单行解析、`Rules{request, response}` 分桶、`HandleRequest`(可短路)/`HandleResponse`(原地改写)。依赖 `dlclark/regexp2`(仓库已有,proxy filter 在用)+ `expr-lang/expr`(唯一新增 direct 依赖)。quirktiva 的 RewriteHandler 接口与 MITM API host(`mitm.clash` 证书下载页)不移植——mihomo 的解密层不需要它们。
- **执行点在 tunnel,不在 listener**:解密成功后,若 `Rewrites() != nil && !Empty()` 且 peek 7 字节判定 HTTP method(`tunnel/mitm.go` 的 `handleMITMRewrite`),则该连接由调解器接管:循环 `http.ReadRequest` → `HandleRequest` 短路或经 per-connection `http.Transport` 转发上游 → `HandleResponse` 原地改写 → 写回,keep-alive 复用。上游拨号复用 tunnel 包内 `resolveMetadata()`(完整规则链 + 懒 DNS + statistic 记账),不是 quirktiva 的 tcpIn channel 注入,也不是 component/dialer 直连(那会绕过用户规则)。
- **规则语义保持 quirktiva 兼容**:type 词自分隔(`url request-header MATCH request-header REPLACE`;单段为纯 payload 如 `url 302 TARGET`)、`<and>` 多值分隔、JSON 类型 payload 编译为 expr 程序、`DeleteArray` 删光元素返回 false。这样 Surge/Loon 生态的既有规则集可平移。
- **配置坏降级不炸**:`mitm.rules` 解析失败只 `log.Warnln` 并置 nil(quirktiva 是整个 config 拒绝)。MITM 解密本体不受影响。
- **零规则零开销**:`rr == nil || rr.Empty()` 时连 peek 探测都不执行,解密分支行为与移植前逐字节一致。
- **防环用 `C.INNER` 双保险**:转发的上游连接 metadata.Type = C.INNER;handleTCPConn 的 mitm 分支守卫 `metadata.Type != C.INNER`,且 resolveMetadata 对 INNER 跳过进程查找。不选 sync.Map 记录连接(错误路径泄漏、有竞态)。
- **quirktiva 的劫持失败无回落缺陷不复现**:它对不信任 CA 的客户端直接断连;mihomo 的解密层本就 SNI 不匹配即透传,重写层接管失败也只是走普通管线。

## Alternatives considered

- **照搬 quirktiva 的 ReverseProxy + channel 劫持架构**(最强论据:代码经过长期使用,`listener/mitm/` 一套拿来即用):否决,因为它要求解密流离开 tunnel 管线、经自建假 net.Listener 回注,与 mihomo 既有 fake-ip/嗅探/tracker/slowdown 全链路重复记账、peeked-byte 重放问题(mitm-port-design 里已论证过同类侵入)。
- **重写执行挂在 listener/http 的 CONNECT 分支**(最强论据:CONNECT 处能拿到完整请求行,分流早):否决,mihomo 的 MITM 在 tunnel 层统一拦截(覆盖 TUN/redir 等所有入站),listener 层只有 http/mixed 走 CONNECT,覆盖面骤减,且解密后的 conn 已在 tunnel 手里,再往 listener 塞是倒流。
- **JSON 改写不用 expr,正则硬吃**(最强论据:少一个依赖):否决,正则改 JSON 结构化数据脆弱(quirktiva 也因此引入 expr),`Delete("playerResponse.adSlots")` 这类删除是去广告场景的核心用法;expr 编译一次复用字节码,运行时开销可接受。
- **去掉 `regexp2` 用标准库 regexp**(最强论据:零依赖):否决,lookbehind/lookahead 是 Surge 重写规则的常用写法,标准库不支持;且仓库已在用 regexp2。

## Consequences

- 收益:解密流量获得完整请求/响应改写能力(拒广告、改 UA、302、JSON 字段删除),规则语法与 Surge 生态兼容;上游转发仍走用户规则链,不改用户预期的分流行为。
- 代价:仅覆盖 HTTP/1.x(`http.ReadRequest` 不支持 HTTP/2 明文 h2c 多路复用,解密后协商出 h2 的连接会在 peek 阶段判非 HTTP method 而走普通管线);QUIC/HTTP3 不在范围(与解密层一致);新增 expr-lang/expr 直接依赖;`tunnel/mitm.go` 约 250 行非平凡转发逻辑,响应 body 全缓冲(大文件下载经重写规则域名时有内存放大——与 quirktiva 行为一致,未优化)。
- 已知上限:客户端证书固定(pinning)场景解密层就失败,重写无从谈起;`skip-domain` 命中的连接不解密也就不改写。

## Testing

- `component/mitm/rewrite/rewrite_test.go`:24 个用例(Type 全枚举、解析错误路径、CRLF 跨 header、gzip 解码/重编码/Content-Length 重算、expr Delete/DeleteArray、未命中不短路)。
- 冒烟(TunnelMediator 子代理,scratch 副本):非 HTTP 解密流拒绝接管且缓冲字节保留给普通管线;HTTP 流经 resolveMetadata/DIRECT/statistic 转发到真实 echo 服务,响应改写重帧,keep-alive 第二请求(带 body POST)同连接应答。
- 仓库级:`go build ./...`、`go vet`、`go test ./component/mitm/... ./config/` 全绿。
