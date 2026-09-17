# ShadowsocksR

!!! note
    遗留协议。现有 `type: ssr` 配置继续可用，与 Shadowsocks（`type: ss`）不是同一协议，不要互相 alias。

```{.yaml linenums="1"}
proxies:
  - name: "ssr"
    type: ssr
    server: server
    port: 443
    cipher: chacha20-ietf
    password: "password"
    obfs: tls1.2_ticket_auth
    protocol: auth_sha1_v4
    # obfs-param: domain.tld
    # protocol-param: "#"
    # udp: true
```

[通用字段](./index.md)
