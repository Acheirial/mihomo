# Входящий транспорт

!!! note
    Транспортный уровень можно свободно комбинировать с входами VMess/VLESS/Trojan/AnyTLS/Snell/Shadowsocks: `ws`/`grpc`/`xhttp`/`mkcp`/`mekya`. `mkcp`/`mekya` требуют прослушивания UDP и не могут использоваться одновременно с `shadow-tls`/`res-tls`/`jls-config`. Нулевое значение (не указано) означает отключение, поведение полностью совпадает со старой конфигурацией.

На примере входа trojan:

=== "ws"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # поддерживает формат ports, например 200,302 или 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      ws-path: "/" # если не пусто, то включается транспортный уровень websocket
      # следующие два параметра, если заполнены, включают tls (необходимо заполнить оба)
      certificate: ./server.crt # сертификат в формате PEM или путь к сертификату
      private-key: ./server.key # приватный ключ сертификата в формате PEM или путь к приватному ключу
    ```

=== "grpc"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # поддерживает формат ports, например 200,302 или 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      grpc-service-name: "GunService" # если не пусто, то включается транспортный уровень grpc
      # следующие два параметра, если заполнены, включают tls (необходимо заполнить оба)
      certificate: ./server.crt # сертификат в формате PEM или путь к сертификату
      private-key: ./server.key # приватный ключ сертификата в формате PEM или путь к приватному ключу
    ```

=== "xhttp"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # поддерживает формат ports, например 200,302 или 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      xhttp-config: # если не пусто, то включается транспортный уровень xhttp
        path: "/x"
        host: example.com
        # mode: auto # Возможные значения: "stream-one", "stream-up" или "packet-up"
        # no-sse-header: false
        # x-padding-bytes: "100-1000"
        # x-padding-obfs-mode: false
        # x-padding-key: x_padding
        # x-padding-header: Referer
        # x-padding-placement: queryInHeader # Возможные значения: queryInHeader, cookie, header, query
        # x-padding-method: repeat-x # Возможные значения: repeat-x, tokenish
        # uplink-http-method: POST # Возможные значения: POST, PUT, PATCH, DELETE
        # session-placement: path # Возможные значения: path, query, cookie, header
        # session-key: ""
        # session-table: "" # Возможные значения: "", "uuid", "ALPHABET", "Alphabet", "BASE36", "Base62", "HEX", "alphabet", "base36", "hex", "number"
        # session-length: "16-32" # начальное значение не может быть 0, общее пространство id должно быть больше 2,1 млрд; действует только если session-table не пустой или uuid
        # seq-placement: path # Возможные значения: path, query, cookie, header
        # seq-key: ""
        # uplink-data-placement: body # Возможные значения: body, cookie, header
        # uplink-data-key: ""
        # uplink-chunk-size: 0 # применяется только когда uplink-data-placement не равно body
        # sc-max-buffered-posts: 30
        # sc-stream-up-server-secs: "20-80"
        # sc-max-each-post-bytes: 1000000
      # следующие два параметра, если заполнены, включают tls (необходимо заполнить оба)
      certificate: ./server.crt # сертификат в формате PEM или путь к сертификату
      private-key: ./server.key # приватный ключ сертификата в формате PEM или путь к приватному ключу
    ```

=== "mkcp"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # поддерживает формат ports, например 200,302 или 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      # Если заполнен mkcp-config и установлено enable: true, включается совместимый с v2ray слушатель входа mKCP (прослушивает UDP, не может использоваться одновременно с shadow-tls/res-tls/jls-config)
      mkcp-config:
        enable: true
        mtu: 1350 # максимальная единица передачи
        tti: 50 # интервал передачи, в миллисекундах
        uplink-capacity: 5 # пропускная способность восходящего канала, в МБ/с
        downlink-capacity: 20 # пропускная способность нисходящего канала, в МБ/с
        congestion: false # включать ли управление перегрузкой
        write-buffer: 2097152 # размер буфера записи, в байтах
        read-buffer: 2097152 # размер буфера чтения, в байтах
        seed: "" # начальное значение, используемое при включении аутентификации AES-GCM; оставьте пустым для использования аутентификации по умолчанию
        header: "" # маскировочный заголовок пакета, варианты: none/srtp/utp/wechat-video/dtls/wireguard
      # следующие два параметра, если заполнены, включают tls (необходимо заполнить оба)
      certificate: ./server.crt # сертификат в формате PEM или путь к сертификату
      private-key: ./server.key # приватный ключ сертификата в формате PEM или путь к приватному ключу
    ```

=== "mekya"
    ```{.yaml linenums="1"}
    listeners:
    - name: trojan-in-1
      type: trojan
      port: 10819 # поддерживает формат ports, например 200,302 или 200,204,401-429,501-503
      listen: 0.0.0.0
      users:
        - username: 1
          password: 9d0cb9d0-964f-4ef6-897d-6c6b3ccf9e68
      # Если заполнен mekya-config и установлено enable: true, включается совместимый с v2ray слушатель входа Mekya (прослушивает UDP, не может использоваться одновременно с mkcp/ws/grpc/xhttp и shadow-tls/res-tls/jls-config)
      mekya-config:
        enable: true
        max-write-size: 10485760 # максимальный размер полезной нагрузки, записываемой в одном ответе, в байтах
        max-write-duration-ms: 5000 # максимальная длительность записи одного ответа, в миллисекундах
        max-simultaneous-write-connection: 128 # количество запросов, которые могут одновременно ожидать записи в рамках одного сеанса
        packet-writing-buffer: 65536 # размер буфера записи пакетов
        kcp:
          mtu: 1350 # максимальная единица передачи
          tti: 15 # интервал передачи, в миллисекундах
          uplink-capacity: 40 # пропускная способность восходящего канала, в МБ/с
          downlink-capacity: 2000 # пропускная способность нисходящего канала, в МБ/с
          congestion: false # включать ли управление перегрузкой
          write-buffer: 67108864 # размер буфера записи, в байтах
          read-buffer: 67108864 # размер буфера чтения, в байтах
          seed: "" # начальное значение, используемое при включении аутентификации AES-GCM; оставьте пустым для использования аутентификации по умолчанию
          header: "" # маскировочный заголовок пакета, варианты: none/srtp/utp/wechat-video/dtls/wireguard
      # следующие два параметра, если заполнены, включают tls (необходимо заполнить оба)
      certificate: ./server.crt # сертификат в формате PEM или путь к сертификату
      private-key: ./server.key # приватный ключ сертификата в формате PEM или путь к приватному ключу
    ```

!!! note
    `mkcp`/`mekya` взаимоисключающи с `ws`/`grpc`/`xhttp`; для одного входа можно включить только один транспортный уровень.

## ws-path

Путь запроса; если не пусто, включает транспортный уровень `ws`

## grpc-service-name

Имя службы gRPC; если не пусто, включает транспортный уровень `grpc`

## xhttp-config

Настройки транспортного уровня `xhttp`; если не пусто, включает транспортный уровень `xhttp`. Поля совпадают с исходящей [Конфигурацией транспортного уровня](../../proxies/transport.ru.md)

## mkcp-config

Настройки транспортного уровня `mkcp`; включается при `enable: true`. Требует прослушивания UDP

### mkcp-config.mtu

Максимальная единица передачи

### mkcp-config.tti

Интервал передачи, в миллисекундах

### mkcp-config.uplink-capacity

Пропускная способность восходящего канала, в МБ/с

### mkcp-config.downlink-capacity

Пропускная способность нисходящего канала, в МБ/с

### mkcp-config.congestion

Включать ли управление перегрузкой

### mkcp-config.write-buffer

Размер буфера записи, в байтах

### mkcp-config.read-buffer

Размер буфера чтения, в байтах

### mkcp-config.seed

Начальное значение, используемое при включении аутентификации AES-GCM; оставьте пустым для использования аутентификации по умолчанию

### mkcp-config.header

Маскировочный заголовок пакета, варианты: `none`/`srtp`/`utp`/`wechat-video`/`dtls`/`wireguard`

## mekya-config

Настройки транспортного уровня `mekya`; включается при `enable: true`. Требует прослушивания UDP. Подобъект `kcp` имеет те же поля, что и [mkcp-config](#mkcp-config)

### mekya-config.max-write-size

Максимальный размер полезной нагрузки, записываемой в одном ответе, в байтах

### mekya-config.max-write-duration-ms

Максимальная длительность записи одного ответа, в миллисекундах

### mekya-config.max-simultaneous-write-connection

Количество запросов, которые могут одновременно ожидать записи в рамках одного сеанса

### mekya-config.packet-writing-buffer

Размер буфера записи пакетов

Поля транспортного уровня совпадают с исходящей [Конфигурацией транспортного уровня](../../proxies/transport.ru.md).
