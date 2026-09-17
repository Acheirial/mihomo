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

[Общие поля](./index.md)

[Поля TLS](./tls.md)

## forward

Использовать режим GOST forward. По умолчанию `false`.

## mux

Включить мультиплексирование. По умолчанию `false`.

## sni

TLS SNI. Если не задан, обычно используется `server`.

## username / password

Необязательные имя пользователя и пароль для аутентификации GOST Relay.
