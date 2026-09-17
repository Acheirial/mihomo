# ShadowsocksR

!!! note
    Устаревший протокол. Существующие конфигурации `type: ssr` по-прежнему работают. Это не тот же протокол, что Shadowsocks (`type: ss`); не делайте взаимный alias.

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

[Общие поля](./index.md) 