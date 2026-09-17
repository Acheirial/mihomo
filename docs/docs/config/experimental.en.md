# Experimental configuration

```{.yaml linenums="1"}
experimental:
  quic-go-disable-gso: false
  quic-go-disable-ecn: false
  dialer-ip4p-convert: false
  disable-connection-tracking: false
```

## quic-go-disable-gso

Disable GSO (Generic Segmentation Offload).

## quic-go-disable-ecn

Disable ECN (Explicit Congestion Notification).

## dialer-ip4p-convert

Enable [IP4P](https://github.com/heiher/natmap/wiki/faq#域名访问是如何实现的) address conversion.

## disable-connection-tracking

Disable connection tracking (default `false`). When `true`, REST `/connections` returns an empty list.
