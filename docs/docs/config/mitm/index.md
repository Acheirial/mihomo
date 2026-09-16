# MITM（TLS 中间人解密）

MITM 让 mihomo 作为 TLS 中间人对流量进行解密，使域名嗅探、基于域名/内容的规则匹配能够看到 TLS 加密流量的真实内容。

## 工作原理

- 客户端与 mihomo 建立连接后，mihomo 剥离 ClientHello 读取 SNI，按 SNI 动态签发一张短时叶子证书与客户端完成 TLS 握手；
- 同时 mihomo 以真实 SNI 与目标服务器建立另一条 TLS 连接；
- 解密后的明文在两条 TLS 连接之间单向中继——不会作为第二条连接重新进入 tunnel，不影响统计与规则计数。

## 配置

在 `sniffer` 段下启用：

```{.yaml linenums="1"}
sniffer:
  enable: true
  mitm:
    enable: true
    ca: /path/to/ca.pem            # CA 证书（PEM 文件路径或内联内容），留空自动生成
    ca-key: /path/to/ca-key.pem    # CA 私钥，留空自动生成
    skip-domain:                   # 不拦截的域名（支持通配）
      - '+.apple.com'
    port-whitelist:                # 仅拦截这些端口，留空拦截全部
      - 443
      - 8443
```

自动生成的 CA 为 ECDSA P-256，有效期 10 年；叶子证书按 SNI 动态签发，有效期 1 小时，带内存缓存。不匹配 `port-whitelist` 或命中 `skip-domain` 的连接原样透传，行为与未启用 MITM 完全一致。

## 信任 CA

客户端必须信任 MITM CA 才能正常访问。按平台将 `ca.pem` 导入系统/浏览器信任存储：

- **Windows**：`certmgr.msc` → 受信任的根证书颁发机构 → 导入；
- **macOS**：钥匙串访问 → 系统 → 导入并标记为始终信任；
- **Linux (Debian/Ubuntu)**：`sudo cp ca.pem /usr/local/share/ca-certificates/mitm-ca.crt && sudo update-ca-certificates`；
- **浏览器**：Firefox 需在其自身设置中单独导入。

## 限制与注意事项

- 使用证书固定（pinning）的应用会连接失败，可用 `skip-domain` 排除；
- 不支持 QUIC/UDP 流量的解密（与 Xray 相同限制）；
- TUN 级别的透明拦截不在覆盖范围内；
- 启用 MITM 即可解密经过的 TLS 流量，请仅用于自有设备和合法合规场景，注意隐私风险。
