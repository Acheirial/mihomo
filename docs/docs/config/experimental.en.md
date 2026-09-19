# Experimental configuration

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

Disable GSO (Generic Segmentation Offload).

## quic-go-disable-ecn

Disable ECN (Explicit Congestion Notification).

## dialer-ip4p-convert

Enable [IP4P](https://github.com/heiher/natmap/wiki/faq#域名访问是如何实现的) address conversion.

## disable-connection-tracking

Disable connection tracking (default `false`). When `true`, REST `/connections` returns an empty list.

## go-memory-limit

Soft memory limit for the Go runtime, in MiB. `0` unsets the limit (`debug.SetMemoryLimit(MaxInt64)`); a no-op if it was never set. This is not a default `GOMEMLIMIT`.

## go-gc-percent

Go runtime GC trigger percentage (`GOGC`). `0` means unset (do not call `debug.SetGCPercent`), not `GOGC=0`. A negative value is valid `GOGC` (disables GC).
