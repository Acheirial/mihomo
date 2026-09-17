# Trojan

```{.yaml linenums="1"}
proxies:
- name: "trojan"
  type: trojan
  server: server
  port: 443
  password: yourpsk
  udp: true

  sni: example.com
  alpn:
  - h2
  - http/1.1
  client-fingerprint: random
  fingerprint: xxxx
  skip-cert-verify: true
  name-cert-verify: example.com
  shadow-tls-opts:
    version: 3
    password: shadow-tls-password
  restls-opts:
    password: restls-password
    version-hint: tls13
  jls-opts:
    username: jls-user
    password: jls-password
  ss-opts:
    enabled: false
    method: aes-128-gcm
    password: "example"
  reality-opts:
    public-key: xxxx
    short-id: xxxx

  network: tcp

  smux:
    enabled: false
```

[Общие поля](./index.md)

[Поля TLS](./tls.md)

## password

Обязательно, пароль сервера trojan

## ss-opts

### ss-opfs.enabled

Включить шифрование AEAD shadowsocks для trojan-go

### ss-opfs.method

Метод шифрования, поддерживает aes-128-gcm/aes-256-gcm/chacha20-ietf-poly1305

### ss-opfs.password

Пароль шифрования AEAD shadowsocks для trojan-go

## network

Транспортный уровень, поддерживает `tcp`/`ws`/`http`/`h2`/`grpc`/`xhttp`/`mkcp`/`mekya`. `httpupgrade` рассматривается как `ws` плюс HTTP Upgrade. Если не настроен или настроен с другим значением, используется tcp. Если `network` не задан, TLS по-прежнему обязателен

См. [Конфигурация транспортного уровня](./transport.md)
