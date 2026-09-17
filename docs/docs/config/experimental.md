# 实验性配置

```{.yaml linenums="1"}
experimental:
  quic-go-disable-gso: false
  quic-go-disable-ecn: false
  dialer-ip4p-convert: false
  disable-connection-tracking: false
```

## quic-go-disable-gso

禁用`GSO`

## quic-go-disable-ecn

禁用`ECN`

## dialer-ip4p-convert

启用[IP4P](https://github.com/heiher/natmap/wiki/faq#域名访问是如何实现的)地址转换

## disable-connection-tracking

关闭连接跟踪（default `false`）。为 `true` 时 REST `/connections` 返回空列表。
