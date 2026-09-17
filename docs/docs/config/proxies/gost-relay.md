# Gost Relay

```{.yaml linenums="1"}
proxies:
- name: "gost-relay"
  type: gost-relay
  server: server
  port: 443
  # forward: false
  # udp: true
  # tls: true
  # mux: true
  # sni: example.com
  # username: username
  # password: password
  # skip-cert-verify: true
  # name-cert-verify: example.com
  # fingerprint: xxxx
  # certificate: ""
  # private-key: ""
  # client-fingerprint: chrome
```

[通用字段](./index.md)

[TLS 字段](./tls.md)

## forward

是否使用 GOST forward 模式，默认 `false`

## mux

是否启用多路复用，默认 `false`

## sni

TLS SNI，未配置时通常回落到 `server`

## username / password

可选，GOST Relay 认证用户名和密码
