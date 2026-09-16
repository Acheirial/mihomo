# MITM（TLS 中间人解密）

MITM 让 mihomo 作为 TLS 中间人对流量进行解密，使域名嗅探、基于域名/内容的规则匹配能够看到 TLS 加密流量的真实内容。

## 工作原理

- 客户端与 mihomo 建立连接后，mihomo 剥离 ClientHello 读取 SNI，按 SNI 动态签发一张短时叶子证书与客户端完成 TLS 握手；
- 同时 mihomo 以真实 SNI 与目标服务器建立另一条 TLS 连接；
- 解密后的明文在两条 TLS 连接之间单向中继——不会作为第二条连接重新进入 tunnel，不影响统计与规则计数。

## 配置

在 `sniffer` 段下启用：

```{.yaml linenums="1"}
sniffer:
  enable: true
  mitm:
    enable: true
    ca: /path/to/ca.pem            # CA 证书（PEM 文件路径或内联内容），留空自动生成
    ca-key: /path/to/ca-key.pem    # CA 私钥，留空自动生成
    skip-domain:                   # 不拦截的域名（支持通配）
      - '+.apple.com'
    port-whitelist:                # 仅拦截这些端口，留空拦截全部
      - 443
      - 8443
```

自动生成的 CA 为 ECDSA P-256，有效期 10 年；叶子证书按 SNI 动态签发，有效期 1 小时，带内存缓存。不匹配 `port-whitelist` 或命中 `skip-domain` 的连接原样透传，行为与未启用 MITM 完全一致。

## 信任 CA

客户端必须信任 MITM CA 才能正常访问。按平台将 `ca.pem` 导入系统/浏览器信任存储：

- **Windows**：`certmgr.msc` → 受信任的根证书颁发机构 → 导入；
- **macOS**：钥匙串访问 → 系统 → 导入并标记为始终信任；
- **Linux (Debian/Ubuntu)**：`sudo cp ca.pem /usr/local/share/ca-certificates/mitm-ca.crt && sudo update-ca-certificates`；
- **浏览器**：Firefox 需在其自身设置中单独导入。

## 限制与注意事项

- 使用证书固定（pinning）的应用会连接失败，可用 `skip-domain` 排除；
- 不支持 QUIC/UDP 流量的解密（与 Xray 相同限制）；
- TUN 级别的透明拦截不在覆盖范围内；
- 启用 MITM 即可解密经过的 TLS 流量，请仅用于自有设备和合法合规场景，注意隐私风险。

## 重写规则

通过 `sniffer.mitm.rules` 可以对解密后的 HTTP 请求/响应执行重写。规则格式为：

```text
<URL 正则> url <类型> [参数...]
```

支持 17 种类型：

| 类型 | 说明 |
| --- | --- |
| `reject` | 返回 404，无内容 |
| `reject-200` | 返回 200，无内容 |
| `reject-204` | 返回 204，无内容 |
| `reject-img` | 返回 200，内容为 1px PNG |
| `reject-dict` | 返回 200，内容为空 JSON 对象 `{}` |
| `reject-array` | 返回 200，内容为空 JSON 数组 `[]` |
| `302` | 302 重定向到指定地址，支持 `$1` 捕获组引用 |
| `307` | 307 重定向到指定地址，支持 `$1` 捕获组引用 |
| `request-header` | 重写请求头（匹配/替换可含 CRLF，一次可处理多个头） |
| `request-body` | 重写请求体 |
| `response-header` | 重写响应头（同 request-header 规则） |
| `response-body` | 重写响应体 |
| `json-request-header` | 按 JSON 语法重写请求头 |
| `json-request-body` | 按 JSON 语法重写请求体 |
| `json-response-header` | 按 JSON 语法重写响应头 |
| `json-response-body` | 按 JSON 语法重写响应体 |

```{.yaml linenums="1"}
sniffer:
  enable: true
  mitm:
    enable: true
    rules: # 重写规则
      - '^https?://www\.example\.com/1 url reject' # 返回 HTTP 404，无内容
      - '^https?://www\.example\.com/2 url reject-200' # 返回 HTTP 200，无内容
      - '^https?://www\.example\.com/3 url reject-img' # 返回 HTTP 200，内容为 1px PNG
      - '^https?://www\.example\.com/4 url reject-dict' # 返回 HTTP 200，内容为空 JSON 对象
      - '^https?://www\.example\.com/5 url reject-array' # 返回 HTTP 200，内容为空 JSON 数组
      - '^https?://www\.example\.com/(6) url 302 https://www.example.com/new-$1'
      - '^https?://www\.(example)\.com/7 url 307 https://www.$1.com/new-7'
      # request-header/response-header 的匹配与替换可以包含 CRLF，
      # 一个正则即可同时匹配多个头
      - '^https?://www\.example\.com/8 url request-header (\r\n)User-Agent:.+(\r\n) request-header $1User-Agent: mihomo$2'
      - '^https?://www\.example\.com/9 url request-body "pos_2":\[.*\],"pos_3" request-body "pos_2":[{"xx": "xx"}],"pos_3"'
      - '^https?://www\.example\.com/10 url response-header (\r\n)Tracecode:.+(\r\n) response-header $1Tracecode: 88888888888$2'
      - '^https?://www\.example\.com/11 url response-body "errmsg":"ok" response-body "errmsg":"not-ok"'
      # json- 变体按 JSON 结构重写，参数为表达式：
      # Delete("a.b;c.d") 删除字段，DeleteArray("a.b","id","1;;2") 按条件删除数组元素
      - '^https?://www\.example\.com/player\?prettyPrint url json-response-body Delete("playerResponse.adSlots;playerResponse.playerAds")'
```

多个可选值可用 `<and>` 分隔，例如：

```yaml
- '^https?://www\.example\.com/api url response-body "a":"1"<and>"b":"2" "a":"1"<and>"b":"2"'
```

说明：

- 规则按顺序匹配，先命中先执行；
- 单条规则解析失败不会导致 MITM 无法启动，仅记录警告并禁用全部重写；
- 重写只作用于被 MITM 解密的流量，未拦截的连接原样透传。
