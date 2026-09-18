# Inbound Transport

!!! note
    The transport layer can be freely combined with VMess/VLESS/Trojan/AnyTLS/Snell/Shadowsocks inbounds: `ws`/`grpc`/`xhttp`/`mkcp`/`mekya`. `mkcp`/`mekya` require listening on UDP, and cannot be used together with `shadow-tls`/`res-tls`/`jls-config`. A zero value (left unset) means disabled, and the behavior is identical to the old configuration.

Using a trojan inbound as the carrier type:

=== "ws"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # supports ports format, e.g., 200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      ws-path: "/" # if not empty, enables websocket transport layer
      # if the following two options are filled, enables tls (must be filled together)
      certificate: ./server.crt # certificate in PEM format, or the path to the certificate
      private-key: ./server.key # corresponding private key in PEM format, or the path to the private key
    ```

=== "grpc"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # supports ports format, e.g., 200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      grpc-service-name: "GunService" # if not empty, enables grpc transport layer
      # if the following two options are filled, enables tls (must be filled together)
      certificate: ./server.crt # certificate in PEM format, or the path to the certificate
      private-key: ./server.key # corresponding private key in PEM format, or the path to the private key
    ```

=== "xhttp"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # supports ports format, e.g., 200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      xhttp-config: # if not empty, enables xhttp transport layer
        path: "/x"
        host: example.com
        # mode: auto # Available: "stream-one", "stream-up" or "packet-up"
        # no-sse-header: false
        # x-padding-bytes: "100-1000"
        # x-padding-obfs-mode: false
        # x-padding-key: x_padding
        # x-padding-header: Referer
        # x-padding-placement: queryInHeader # Available: queryInHeader, cookie, header, query
        # x-padding-method: repeat-x # Available: repeat-x, tokenish
        # uplink-http-method: POST # Available: POST, PUT, PATCH, DELETE
        # session-placement: path # Available: path, query, cookie, header
        # session-key: ""
        # session-table: "" # Available: "", "uuid", "ALPHABET", "Alphabet", "BASE36", "Base62", "HEX", "alphabet", "base36", "hex", "number"
        # session-length: "16-32" # the initial value cannot be 0, and the total ID space must be greater than 2.1 billion; takes effect only if session-table is not empty or uuid
        # seq-placement: path # Available: path, query, cookie, header
        # seq-key: ""
        # uplink-data-placement: body # Available: body, cookie, header
        # uplink-data-key: ""
        # uplink-chunk-size: 0 # only applicable when uplink-data-placement is not body
        # sc-max-buffered-posts: 30
        # sc-stream-up-server-secs: "20-80"
        # sc-max-each-post-bytes: 1000000
      # if the following two options are filled, enables tls (must be filled together)
      certificate: ./server.crt # certificate in PEM format, or the path to the certificate
      private-key: ./server.key # corresponding private key in PEM format, or the path to the private key
    ```

=== "mkcp"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # supports ports format, e.g., 200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      # If mkcp-config is filled in and enable: true is set, the v2ray-compatible mKCP inbound listener is enabled (listens on UDP, cannot be used together with shadow-tls/res-tls/jls-config)
      mkcp-config:
        enable: true
        mtu: 1350 # maximum transmission unit
        tti: 50 # transmission time interval, in milliseconds
        uplink-capacity: 5 # uplink capacity, in MB/s
        downlink-capacity: 20 # downlink capacity, in MB/s
        congestion: false # whether to enable congestion control
        write-buffer: 2097152 # write buffer size, in bytes
        read-buffer: 2097152 # read buffer size, in bytes
        seed: "" # seed used when AES-GCM authentication is enabled; leave empty to use the default authentication
        header: "" # camouflage packet header, available: none/srtp/utp/wechat-video/dtls/wireguard
      # if the following two options are filled, enables tls (must be filled together)
      certificate: ./server.crt # certificate in PEM format, or the path to the certificate
      private-key: ./server.key # corresponding private key in PEM format, or the path to the private key
    ```

=== "mekya"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # supports ports format, e.g., 200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      # If mekya-config is filled in and enable: true is set, the v2ray-compatible Mekya inbound listener is enabled (listens on UDP, cannot be used together with mkcp/ws/grpc/xhttp or shadow-tls/res-tls/jls-config)
      mekya-config:
        enable: true
        max-write-size: 10485760 # maximum payload size written back in a single response, in bytes
        max-write-duration-ms: 5000 # maximum duration written back in a single response, in milliseconds
        max-simultaneous-write-connection: 128 # number of requests allowed to wait for write-back simultaneously in the same session
        packet-writing-buffer: 65536 # packet write buffer size
        kcp:
          mtu: 1350 # maximum transmission unit
          tti: 15 # transmission time interval, in milliseconds
          uplink-capacity: 40 # uplink capacity, in MB/s
          downlink-capacity: 2000 # downlink capacity, in MB/s
          congestion: false # whether to enable congestion control
          write-buffer: 67108864 # write buffer size, in bytes
          read-buffer: 67108864 # read buffer size, in bytes
          seed: "" # seed used when AES-GCM authentication is enabled; leave empty to use the default authentication
          header: "" # camouflage packet header, available: none/srtp/utp/wechat-video/dtls/wireguard
      # if the following two options are filled, enables tls (must be filled together)
      certificate: ./server.crt # certificate in PEM format, or the path to the certificate
      private-key: ./server.key # corresponding private key in PEM format, or the path to the private key
    ```

!!! note
    `mkcp`/`mekya` are mutually exclusive with `ws`/`grpc`/`xhttp`; only one transport layer can be enabled per inbound.

## ws-path

Request path; when not empty, enables the `ws` transport layer

## grpc-service-name

gRPC service name; when not empty, enables the `grpc` transport layer

## xhttp-config

`xhttp` transport layer settings; when not empty, enables the `xhttp` transport layer. The fields are the same as the outbound [Transport Configuration](../../proxies/transport.en.md)

## mkcp-config

`mkcp` transport layer settings; enabled when `enable: true`. Requires listening on UDP

### mkcp-config.mtu

Maximum transmission unit

### mkcp-config.tti

Transmission time interval, in milliseconds

### mkcp-config.uplink-capacity

Uplink capacity, in MB/s

### mkcp-config.downlink-capacity

Downlink capacity, in MB/s

### mkcp-config.congestion

Whether to enable congestion control

### mkcp-config.write-buffer

Write buffer size, in bytes

### mkcp-config.read-buffer

Read buffer size, in bytes

### mkcp-config.seed

Seed used when AES-GCM authentication is enabled; leave empty to use the default authentication

### mkcp-config.header

Camouflage packet header, available: `none`/`srtp`/`utp`/`wechat-video`/`dtls`/`wireguard`

## mekya-config

`mekya` transport layer settings; enabled when `enable: true`. Requires listening on UDP. Its `kcp` sub-object shares the same fields as [mkcp-config](#mkcp-config)

### mekya-config.max-write-size

Maximum payload size written back in a single response, in bytes

### mekya-config.max-write-duration-ms

Maximum duration written back in a single response, in milliseconds

### mekya-config.max-simultaneous-write-connection

Number of requests allowed to wait for write-back simultaneously in the same session

### mekya-config.packet-writing-buffer

Packet write buffer size

The transport layer fields are the same as the outbound [Transport Configuration](../../proxies/transport.en.md).
