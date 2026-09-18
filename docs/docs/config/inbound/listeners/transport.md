# 入站传输层配置

!!! note
    传输层可与 VMess/VLESS/Trojan/AnyTLS/Snell/Shadowsocks 入站自由组合：`ws`/`grpc`/`xhttp`/`mkcp`/`mekya`。`mkcp`/`mekya` 需要监听 UDP，且不支持与 `shadow-tls`/`res-tls`/`jls-config` 同时使用。零值（不写）即不启用，行为与旧配置完全一致。

以 trojan 入站为例：

=== "ws"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # 支持使用ports格式，例如200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      ws-path: "/" # 如果不为空则开启 websocket 传输层
      # 下面两项如果填写则开启 tls（需要同时填写）
      certificate: ./server.crt # 证书 PEM 格式，或者 证书的路径
      private-key: ./server.key # 证书对应的私钥 PEM 格式，或者私钥路径
    ```

=== "grpc"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # 支持使用ports格式，例如200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      grpc-service-name: "GunService" # 如果不为空则开启 grpc 传输层
      # 下面两项如果填写则开启 tls（需要同时填写）
      certificate: ./server.crt # 证书 PEM 格式，或者 证书的路径
      private-key: ./server.key # 证书对应的私钥 PEM 格式，或者私钥路径
    ```

=== "xhttp"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # 支持使用ports格式，例如200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      xhttp-config: # 如果不为空则开启 xhttp 传输层
        path: "/x"
        host: example.com
        # mode: auto # 可选："stream-one"、"stream-up" 或 "packet-up"
        # no-sse-header: false
        # x-padding-bytes: "100-1000"
        # x-padding-obfs-mode: false
        # x-padding-key: x_padding
        # x-padding-header: Referer
        # x-padding-placement: queryInHeader # 可选：queryInHeader、cookie、header、query
        # x-padding-method: repeat-x # 可选：repeat-x、tokenish
        # uplink-http-method: POST # 可选：POST、PUT、PATCH、DELETE
        # session-placement: path # 可选：path、query、cookie、header
        # session-key: ""
        # session-table: "" # 可选："", "uuid", "ALPHABET", "Alphabet", "BASE36", "Base62", "HEX", "alphabet", "base36", "hex", "number"
        # session-length: "16-32" # 起始值不可为 0，总的 id 空间必须大于 21 亿，仅当 session-table 不为空或 uuid 时生效
        # seq-placement: path # 可选：path、query、cookie、header
        # seq-key: ""
        # uplink-data-placement: body # 可选：body、cookie、header
        # uplink-data-key: ""
        # uplink-chunk-size: 0 # 仅当 uplink-data-placement 不为 body 时生效
        # sc-max-buffered-posts: 30
        # sc-stream-up-server-secs: "20-80"
        # sc-max-each-post-bytes: 1000000
      # 下面两项如果填写则开启 tls（需要同时填写）
      certificate: ./server.crt # 证书 PEM 格式，或者 证书的路径
      private-key: ./server.key # 证书对应的私钥 PEM 格式，或者私钥路径
    ```

=== "mkcp"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # 支持使用ports格式，例如200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      # 如果填写 mkcp-config 并设置 enable: true 则启用 v2ray 兼容的 mKCP 入站监听（监听 UDP，不可与 shadow-tls/res-tls/jls-config 同时使用）
      mkcp-config:
        enable: true
        mtu: 1350 # 最大传输单元
        tti: 50 # 传输时间间隔，单位毫秒
        uplink-capacity: 5 # 上行容量，单位 MB/s
        downlink-capacity: 20 # 下行容量，单位 MB/s
        congestion: false # 是否启用拥塞控制
        write-buffer: 2097152 # 写缓冲区大小，单位字节
        read-buffer: 2097152 # 读缓冲区大小，单位字节
        seed: "" # 启用 AES-GCM 认证时使用的种子，留空使用默认认证
        header: "" # 伪装包头，可选：none/srtp/utp/wechat-video/dtls/wireguard
      # 下面两项如果填写则开启 tls（需要同时填写）
      certificate: ./server.crt # 证书 PEM 格式，或者 证书的路径
      private-key: ./server.key # 证书对应的私钥 PEM 格式，或者私钥路径
    ```

=== "mekya"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # 支持使用ports格式，例如200,302 or 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      # 如果填写 mekya-config 并设置 enable: true 则启用 v2ray 兼容的 Mekya 入站监听（监听 UDP，不可与 mkcp/ws/grpc/xhttp 及 shadow-tls/res-tls/jls-config 同时使用）
      mekya-config:
        enable: true
        max-write-size: 10485760 # 单个响应写回的最大负载大小，单位字节
        max-write-duration-ms: 5000 # 单个响应写回的最大持续时间，单位毫秒
        max-simultaneous-write-connection: 128 # 同一会话允许同时等待写回的请求数
        packet-writing-buffer: 65536 # 写包缓冲区大小
        kcp:
          mtu: 1350 # 最大传输单元
          tti: 15 # 传输时间间隔，单位毫秒
          uplink-capacity: 40 # 上行容量，单位 MB/s
          downlink-capacity: 2000 # 下行容量，单位 MB/s
          congestion: false # 是否启用拥塞控制
          write-buffer: 67108864 # 写缓冲区大小，单位字节
          read-buffer: 67108864 # 读缓冲区大小，单位字节
          seed: "" # 启用 AES-GCM 认证时使用的种子，留空使用默认认证
          header: "" # 伪装包头，可选：none/srtp/utp/wechat-video/dtls/wireguard
      # 下面两项如果填写则开启 tls（需要同时填写）
      certificate: ./server.crt # 证书 PEM 格式，或者 证书的路径
      private-key: ./server.key # 证书对应的私钥 PEM 格式，或者私钥路径
    ```

!!! note
    `mkcp`/`mekya` 与 `ws`/`grpc`/`xhttp` 互斥，同一入站只能启用一种传输层。

## ws-path

请求路径，不为空时开启 `ws` 传输层

## grpc-service-name

gRPC 服务名称，不为空时开启 `grpc` 传输层

## xhttp-config

`xhttp` 传输层设置，不为空时开启 `xhttp` 传输层，字段与出站 [传输层配置](../../proxies/transport.md) 一致

## mkcp-config

`mkcp` 传输层设置，`enable: true` 时开启，需要监听 UDP

### mkcp-config.mtu

最大传输单元

### mkcp-config.tti

传输时间间隔，单位毫秒

### mkcp-config.uplink-capacity

上行容量，单位 MB/s

### mkcp-config.downlink-capacity

下行容量，单位 MB/s

### mkcp-config.congestion

是否启用拥塞控制

### mkcp-config.write-buffer

写缓冲区大小，单位字节

### mkcp-config.read-buffer

读缓冲区大小，单位字节

### mkcp-config.seed

启用 AES-GCM 认证时使用的种子，留空使用默认认证

### mkcp-config.header

伪装包头，可选：`none`/`srtp`/`utp`/`wechat-video`/`dtls`/`wireguard`

## mekya-config

`mekya` 传输层设置，`enable: true` 时开启，需要监听 UDP，其 `kcp` 子项与 [mkcp-config](#mkcp-config) 字段一致

### mekya-config.max-write-size

单个响应写回的最大负载大小，单位字节

### mekya-config.max-write-duration-ms

单个响应写回的最大持续时间，单位毫秒

### mekya-config.max-simultaneous-write-connection

同一会话允许同时等待写回的请求数

### mekya-config.packet-writing-buffer

写包缓冲区大小

传输层字段与出站 [传输层配置](../../proxies/transport.md) 一致。
