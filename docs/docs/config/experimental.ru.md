# Экспериментальная конфигурация

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

Отключить `GSO`

## quic-go-disable-ecn

Отключить `ECN`

## dialer-ip4p-convert

Включить преобразование адреса [IP4P](https://github.com/heiher/natmap/wiki/faq#域名访问是如何实现的) 

## disable-connection-tracking

Отключить отслеживание соединений (default `false`). При `true` REST `/connections` возвращает пустой список.

## go-memory-limit

Мягкий лимит памяти runtime Go, в MiB. `0` снимает лимит (`debug.SetMemoryLimit(MaxInt64)`); no-op, если лимит никогда не задавался. Это не значение `GOMEMLIMIT` по умолчанию.

## go-gc-percent

Процент срабатывания GC runtime Go (`GOGC`). `0` — не задавать (не вызывать `debug.SetGCPercent`), а не `GOGC=0`. Отрицательное значение — допустимый `GOGC` (отключает GC).