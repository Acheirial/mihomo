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

[Common fields](./index.md)

[TLS fields](./tls.md)

## forward

Whether to use GOST forward mode. Default: `false`.

## mux

Whether to enable multiplexing. Default: `false`.

## sni

TLS SNI. If unset, this usually falls back to `server`.

## username / password

Optional GOST Relay authentication username and password.
