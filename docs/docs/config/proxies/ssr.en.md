# ShadowsocksR

!!! note
    Legacy protocol. Existing `type: ssr` configs remain valid. This is not the same protocol as Shadowsocks (`type: ss`); do not alias them to each other.

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

[Common fields](./index.md)
