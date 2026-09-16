# MITM (TLS 中间人解密)

## 功能简介

MITM（Man-in-the-Middle，中间人）功能允许 mihomo 对 HTTP / Mixed 入站中的 HTTPS 流量进行解密。开启后，mihomo 会拦截客户端的 CONNECT 请求，以自签证书与客户端完成 TLS 握手，从而以明文方式查看和处理 HTTPS 流量内容，然后再与真实服务器建立连接转发。

典型使用场景：

- 需要基于 HTTPS 域名之外的内容（如具体路径、Host 明文）进行更精细的处理或调试；
- 排查客户端与服务器之间的 TLS 通信问题；
- 对可信环境内的流量做审计与分析。

MITM 依赖客户端信任 mihomo 的 CA 证书，否则客户端会因证书校验失败而拒绝连接。

## 工作原理

- **CONNECT 拦截**：客户端通过 HTTP/Mixed 入站发起 CONNECT 请求时，若目标主机匹配 `hosts` 规则，mihomo 不再直接盲转隧道流量，而是作为 TLS 服务端与客户端握手，解密后再向上游服务器发起新的连接。不匹配 `hosts` 的目标不受影响，仍按原有方式透传。
- **按 SNI 动态签发叶子证书**：握手时 mihomo 根据目标域名（SNI）用 CA 实时签发一张对应该域名的叶子证书，有效期 1 小时。叶子证书在内存中缓存，同一域名后续握手直接复用，不会落盘。
- **单流中继**：解密后 mihomo 与上游服务器的连接仍是普通的 TLS 连接，客户端 → mihomo、mihomo → 上游两条 TLS 流各自独立，互不感知。mihomo 只在本地这一段能看到明文。

CA 证书的来源有两种：

1. **加载已有 CA**：通过 `ca-certificate` 与 `ca-private-key` 指定 PEM 格式的 CA 证书和私钥（既支持文件路径，也支持直接内联 PEM 内容）。
2. **自动生成 CA**：不提供 CA 时，mihomo 自动生成一张 ECDSA P-256 的 CA 证书，有效期 10 年。`store-ca: true` 时会将自动生成的 CA 持久化到本地，重启后继续使用同一张 CA，避免客户端反复重新信任。

## 配置示例

`mitm` 配置节位于 `listeners` 下的 `http` 或 `mixed` 入站中：

```yaml
listeners:
  - name: mixed-in-mitm
    type: mixed
    port: 10810
    listen: 0.0.0.0
    mitm:
      enable: true
      hosts: # 为空或不配置时拦截所有 HTTPS 域名；配置后仅拦截匹配的域名
        - "*.example.com"
        - example.org
      ca-certificate: ./mitm-ca.crt # CA 证书 PEM 格式，或者证书的路径
      ca-private-key: ./mitm-ca.key # CA 对应私钥 PEM 格式，或者私钥路径
      store-ca: false # 为 true 时持久化自动生成的 CA
```

### 方式一：加载已有 CA

自备一张 CA 证书与私钥（PEM 格式），通过 `ca-certificate` / `ca-private-key` 指定：

```yaml
listeners:
  - name: http-in-mitm
    type: http
    port: 10809
    listen: 0.0.0.0
    mitm:
      enable: true
      ca-certificate: ./mitm-ca.crt
      ca-private-key: ./mitm-ca.key
```

也可以直接内联 PEM 内容：

```yaml
    mitm:
      enable: true
      ca-certificate: |
        -----BEGIN CERTIFICATE-----
        ...
        -----END CERTIFICATE-----
      ca-private-key: |
        -----BEGIN PRIVATE KEY-----
        ...
        -----END PRIVATE KEY-----
```

### 方式二：自动生成 CA 并持久化

不提供 CA 时，mihomo 自动生成 ECDSA P-256 的 CA（有效期 10 年）。建议同时开启 `store-ca`，将 CA 落盘，重启后沿用同一张证书：

```yaml
listeners:
  - name: mixed-in-mitm
    type: mixed
    port: 10810
    listen: 0.0.0.0
    mitm:
      enable: true
      store-ca: true
```

## 客户端信任 CA

MITM 解密要求客户端信任 mihomo 使用的 CA 证书。将 CA 证书（自动生成并 `store-ca: true` 时为落盘的那份，加载模式为 `ca-certificate` 指定的那份）安装到系统或浏览器的信任存储中：

- **Windows**：双击证书文件 → 「安装证书」→ 选择「本地计算机」→ 将其放入「受信任的根证书颁发机构」存储区；或使用 `certutil -addstore Root mitm-ca.crt`。
- **macOS**：双击证书导入「钥匙串访问」，找到该证书，在「信任」设置中将 SSL 一项改为「始终信任」；或使用 `security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain mitm-ca.crt`。
- **Linux (Debian/Ubuntu)**：将证书复制到 `/usr/local/share/ca-certificates/`（扩展名需为 `.crt`），然后执行 `update-ca-certificates`。
- **Linux (RHEL/Fedora)**：将证书复制到 `/etc/pki/ca-trust/source/anchors/`，然后执行 `update-ca-trust`。
- **浏览器**：Firefox 使用独立的证书存储，需在「设置 → 隐私与安全 → 证书 → 查看证书 → 证书颁发机构」中手动导入；Chromium 系浏览器默认使用系统存储。移动端（Android/iOS）需在系统设置中安装并信任 CA 配置文件/描述文件。

仅信任当前用户/浏览器而不信任系统时，只有对应的客户端生效；命令行工具（curl、Go 程序等）通常依赖系统存储或自身的 CA 环境变量（如 `SSL_CERT_FILE`）。

## 限制与注意事项

- **证书固定（certificate pinning）的应用会失效**：部分应用（银行、部分即时通讯、Google 移动端应用等）内置了服务器证书校验，不信任系统额外安装的 CA。对这类应用的 MITM 拦截会导致其无法联网，可用 `hosts` 将其域名排除在拦截范围之外。
- **仅覆盖 HTTP / Mixed 入站的 TCP 流量**：QUIC / HTTP3（UDP）流量不在 MITM 覆盖范围内，相关请求不会被解密；TUN 层面的透明拦截同样不受本功能影响。
- **隐私与合规警告**：开启 MITM 意味着 mihomo 可以查看经过入站的 HTTPS 明文内容，包括可能的敏感信息（密码、令牌、消息内容等）。请仅在自有设备、获得明确授权的环境中使用，并妥善保管 CA 私钥——持有 CA 私钥即可签发任意域名的可信证书，泄露后应立即更换并撤销客户端信任。
- **性能开销**：每个新域名的首次握手需要实时签发叶子证书，且加解密本身带来额外 CPU 开销，大流量场景下请评估影响。
