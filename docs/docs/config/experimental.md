# 实验性配置

```{.yaml linenums="1"}
experimental:
  quic-go-disable-gso: false
  quic-go-disable-ecn: false
  dialer-ip4p-convert: false
  disable-connection-tracking: false
  go-memory-limit: 0
  go-gc-percent: 0
```

## quic-go-disable-gso

禁用`GSO`

## quic-go-disable-ecn

禁用`ECN`

## dialer-ip4p-convert

启用[IP4P](https://github.com/heiher/natmap/wiki/faq#域名访问是如何实现的)地址转换

## disable-connection-tracking

关闭连接跟踪（default `false`）。为 `true` 时 REST `/connections` 返回空列表。

## go-memory-limit

Go runtime 软内存上限，单位 MiB。`0` 表示卸限（`debug.SetMemoryLimit(MaxInt64)`）；从未设置过时与默认相同。并非默认开启 `GOMEMLIMIT`。

## go-gc-percent

Go runtime GC 触发百分比（`GOGC`）。`0` 表示不设置（不调用 `debug.SetGCPercent`），并非 `GOGC=0`。负值是合法的 `GOGC`（关闭 GC）。
