# 设计：sub2api 原生支持 shadowsocks + simple-obfs 出站代理

- 日期：2026-07-27
- 状态：设计已确认，待编写实施计划
- 背景需求：让 sub2api 能通过机场订阅（GLaDOS）借道访问上游 AI API

## 1. 问题与约束

### 问题

部署 sub2api 的服务器无法直接访问上游 AI API（Anthropic / OpenAI / xAI 等）。用户持有 GLaDOS 机场订阅，希望让 sub2api 的**出站请求**走机场节点。

### 硬约束

| 约束 | 来源 | 影响 |
|---|---|---|
| 不影响宿主机默认出口 IP | 用户明确要求 | 排除 TUN / 全局代理模式 |
| 单一二进制，不新增容器或进程 | 用户明确要求 | 排除 mihomo / sing-box 边车方案；排除 SIP003 外部插件进程 |
| 面板「一键更新」后即可用 | 用户明确要求 | 更新链路只替换二进制，功能必须完全内置 |
| 与现有代理功能保持一致 | 用户明确要求 | 机场节点即普通代理记录，不做平行体系 |

### 订阅实际参数（已确认）

```yaml
type: ss
cipher: chacha20-ietf-poly1305
plugin: obfs
plugin-opts:
  mode: tls
```

标准 AEAD 加密 + simple-obfs 的 TLS 伪装模式。

## 2. 现状基线

sub2api 已有完整的代理子系统，本设计是**扩展**而非新建：

- `proxies` 表：name / protocol / host / port / username / password / status / expires_at / fallback_mode / backup_proxy_id
- 支持协议：`http` / `https` / `socks5` / `socks5h`
- 账号可绑定代理（`Account.proxy` 边）
- 延迟探测、质量评分、到期回退、批量导入导出

三个关键代码位置：

| 位置 | 作用 |
|---|---|
| `internal/pkg/proxyurl/parse.go` | 代理 URL 统一校验，含协议白名单；**fail-fast，无效代理不回退直连** |
| `internal/pkg/proxyutil/dialer.go` | `ConfigureTransportProxy`，全局唯一的代理接入口 |
| `internal/pkg/tlsfingerprint/dialer.go` | TLS 指纹拨号，**独立于 proxyutil** 的第二条代理路径 |

## 3. 目标与非目标

### 目标

sub2api 单一二进制内原生支持 `ss + obfs(tls)` 出站，机场节点以普通代理记录形式纳入现有代理管理。

### 非目标（明确不做）

- 不支持 vmess / vless / trojan / hysteria2（当前订阅不含这些协议）
- 不支持 UDP 转发（上游 API 均为 HTTPS，仅需 TCP）
- 不支持 ss-2022 系列 cipher（当前订阅不用）
- 不支持 `obfs` 之外的插件（v2ray-plugin 等）
- 不做入站监听（不开本地代理端口，纯 dialer 模式）
- 不改宿主机路由表
- **阶段一不做订阅定时自动同步**（理由见 6.2）

## 4. 技术选型

### 决策：加密层用库，伪装层自实现

| 层 | 选择 | 理由 |
|---|---|---|
| ss AEAD | `github.com/shadowsocks/go-shadowsocks2/core` | Apache-2.0；API 正好对口；密码学敏感部分用成熟实现 |
| simple-obfs (tls) | 自实现（新包 `internal/pkg/shadowsocks/`） | 无许可证干净的现成 Go 库；且该层不承担安全职责 |

### 备选方案与否决理由

**mihomo 子包（`transport/simple-obfs` + `transport/shadowsocks/core`）** —— 否决：

- 许可证信号冲突：仓库根 LICENSE 是 MIT（Copyright 2023 KT），但 pkg.go.dev 对 `transport/simple-obfs` 单独标注 GPL-3.0。pkg.go.dev 按包路径解析许可证，这通常意味着该子目录有自己的 LICENSE 文件。对公开分发二进制与 GHCR 镜像的仓库，此风险不可接受
- `transport/shadowsocks/core` 的 `PacketConnCipher` 依赖 mihomo 内部 `common` 包，无法真正做到只引子包
- 本仓库需定期合并上游，`go.mod` 膨胀会持续增加摩擦

**sing-box / sing-shadowsocks** —— 否决：GitHub API 许可证字段为 `NOASSERTION`（非标准许可证），需人工审读方可采用。

**全自实现（含 AEAD）** —— 未采用，但为合理备选：

优点是零依赖、合并上游改动面最小；缺点是自行组装 HKDF-SHA1 + AEAD 时，nonce 递增、salt 处理、分块长度等细节出错会造成**静默的安全问题**（不报错但加密失效）。相比之下 obfs 层写错的症状是"连不上"，立即暴露。风险切分不对等，故加密层选用成熟实现。

> 备注：`go-shadowsocks2` 最后提交为 2024-10，约两年未更新。ss AEAD 协议本身已冻结，影响有限，但属于已知瑕疵。

## 5. 架构

### 5.1 拨号链路

```
http.Transport.DialContext
      │
      ▼
net.Dialer ──TCP──▶ 机场节点 host:port
      │
      ▼
simple-obfs(tls) 包装        ← 自实现；伪装成 TLS 1.2 握手，数据套 TLS record 头
      │
      ▼
ss AEAD 包装                 ← core.Cipher.StreamConn()
      │
      ▼
写入目标地址头（SOCKS5 地址格式）
      │
      ▼
返回 net.Conn ──▶ 交回 http.Transport 跑 HTTPS
```

层序说明：obfs 是**最外层**（网络上看到的就是它伪装的 TLS 流量），ss 加密在其内部。

### 5.2 代码改动点

| 文件 | 改动 |
|---|---|
| `internal/pkg/proxyurl/parse.go` | `allowedSchemes` 加 `ss`；保持 fail-fast 语义 |
| `internal/pkg/proxyutil/dialer.go` | 加 `case "ss"`，构造 dialer 赋给 `transport.DialContext` |
| `internal/pkg/shadowsocks/`（新包） | obfs-tls 实现 + dialer 组装 + 参数解析 |
| `internal/pkg/tlsfingerprint/dialer.go` | ss 场景复用 `NewDialer(profile, baseDialer)` 通用构造 |
| `ent/schema/proxy.go` + 迁移 | 新增可空 `extra` 列 |
| `internal/service/proxy*.go` | `Proxy` 实体加 `Extra`；`URL()` 拼接 query |
| `internal/handler/admin/proxy_data.go` | 导入导出带上 `extra` |
| 前端代理表单 | 按协议切换字段标签 |

**上层调用方零改动**——所有调用方传递的都是「代理 URL 字符串」，`ss://` 同样是合法 URL。

### 5.3 数据模型

ss 节点参数映射到现有五元组，插件参数走 query string：

```
ss://chacha20-ietf-poly1305:密码@节点host:端口?plugin=obfs&mode=tls&obfs-host=xxx.com
   └────── Username ──────┘ └Password┘ └Host┘ └Port┘ └──── extra 字段 ────┘
```

这一映射并非权宜之计：ss 分享链接的标准格式本就是 `ss://method:password@host:port`，cipher 天然占据 URL userinfo 的 username 位。

字段长度校验：

| 字段 | 存放内容 | 上限 | 实际 |
|---|---|---|---|
| `protocol` | `ss` | 20 | 2 ✅ |
| `username` | cipher | 100 | `chacha20-ietf-poly1305` = 22 ✅ |
| `password` | ss 密码 | 100 | 远小于 100 ✅ |

**新增 `extra` 列**存放插件参数：Postgres 类型为 `JSONB NULL`，ent 侧声明为 `field.JSON(...).Optional()`。`Proxy.URL()` 将其拼为 query string 后，下游全链路无感：`proxyurl.Parse` 只校验 scheme 与 host，`*url.URL` 原生支持 query。

query 参数拼接需保证**顺序稳定**（按 key 排序），否则同一节点在不同时刻生成的 URL 字符串不同，会击穿 `httpclient` 的 transport 缓存。

附带正确性：`httpclient` 的 transport 缓存键使用完整 URL 字符串，因此 obfs-host 不同即视为不同 transport，不会错误复用。

### 5.4 迁移

项目迁移机制为「编号 SQL 文件 + SHA256 校验，启动时自动执行」（`internal/repository/migrations_runner.go`）。

**这是本仓库第一个定制 schema 变更。** 为避免与上游迁移编号冲突（上游当前已到 `185_`），定制迁移使用高位编号段 `9001_`，与上游永久隔离。

注意：迁移文件一经应用即不可修改内容（checksum 校验）。

## 6. 订阅接入

### 6.1 手动导入流程

```
管理员粘贴订阅 URL → 后端拉取 → 解析 Clash YAML
   → 筛出 type: ss 的节点 → 转为 DataProxy 结构
   → 复用现有 import 路径（buildProxyKey 去重）
```

依赖情况：`gopkg.in/yaml.v3 v3.0.1` 已是直接依赖，解析订阅无需新增依赖。

解析时需跳过非 `ss` 类型节点，并对 `plugin` 非 `obfs`、或 `plugin-opts.mode` 非 `tls` 的节点给出明确的跳过原因，而非静默丢弃。

> 范围说明：simple-obfs 有 `tls` 与 `http` 两种模式，本设计**只实现 `tls`**（当前订阅所用）。`http` 模式节点在导入时按上述规则跳过并提示，不做静默降级。

### 6.2 不做定时自动同步的理由

账号绑定的对象是**代理记录**。订阅刷新后若某节点下线，自动删除该代理记录会使绑定它的账号悬空。正确处理需引入「节点消失 → 标记停用而非删除 → 触发现有 fallback 回退」的协调状态机，这是一块独立的复杂度。

手动导入无此问题：管理员点击一次、查看预览、自行决定。待实际使用后确认节点变更频率确实需要自动化，再作为独立需求实施，届时可复用已有的 `fallback_mode` / `backup_proxy_id` 机制。

## 7. 与现有能力的协同

| 现有能力 | ss 节点是否自动可用 | 说明 |
|---|---|---|
| 延迟探测 / 质量评分 | ✅ 自动 | 探测服务经 `ProxyURL` → httpclient → proxyutil，无需改动 |
| 账号绑定代理 | ✅ 自动 | ss 节点即普通代理记录 |
| 到期回退（`fallback_mode`） | ✅ 自动 | 与协议无关 |
| 导入 / 导出 | ⚠️ 需补 | `DataProxy` 需带上 `extra` 字段 |
| TLS 指纹 | ⚠️ 需改 | 见下 |

**TLS 指纹适配**：`tlsfingerprint` 包现有 `NewSOCKS5ProxyDialer` / `NewHTTPProxyDialer` 两个按协议写死的构造函数，ss 无法复用。适配方式为将 ss 的 `DialContext` 作为 baseDialer 传入同文件已存在的通用构造 `NewDialer(profile, baseDialer)`，**不新增第三个 XXXProxyDialer**。

## 8. 错误处理与安全语义

`proxyurl.Parse` 的既有设计意图为：**无效代理 fail-fast，绝不静默回退直连**，因为回退会产生 IP 关联风险。ss 分支必须遵守同一语义。

| 错误场景 | 处理 |
|---|---|
| 密码 / cipher 缺失 | 创建 dialer 时立即报错 |
| cipher 不受支持 | 创建 dialer 时立即报错，错误信息列出支持的 cipher |
| obfs 参数非法（mode 未知等） | 创建 dialer 时立即报错 |
| obfs 握手失败 | 返回连接错误，交由上层账号 failover 接管 |
| ss 认证失败 | 返回连接错误，交由上层账号 failover 接管 |

**任何情况下都不得返回一个"退化为直连"的 dialer。**

日志方面：`parsed.Redacted()` 会屏蔽密码，但不屏蔽 query 参数。`obfs-host` 非机密，可接受。

## 9. 测试策略

| 层次 | 测什么 | 方式 |
|---|---|---|
| 单元 | obfs-tls 编解码 | 自实现 client 对拼测试用 server，跑 round-trip |
| 单元 | URL ↔ 参数互转 | `Proxy.URL()` → `proxyurl.Parse` → dialer 参数，闭环校验 |
| 单元 | Clash YAML 解析 | 样例含 plugin-opts、含非 ss 节点混杂、含缺字段节点 |
| 单元 | 错误路径 | 每个 fail-fast 场景各一例，断言不产生可用 dialer |
| 集成 | 端到端拨号 | 本地起 ss+obfs 服务端，验证可通 HTTPS |
| 回归 | 既有协议不受影响 | http / socks5 现有测试全绿 |

按项目约定，后端测试须带 `-tags unit`；新增测试文件需确认是否需要该 build tag。

## 10. 上游合并成本

本功能将在 `CUSTOM_CHANGES.md`「持续维护的定制功能清单」新增一行，涉及范围：

- 新包 `internal/pkg/shadowsocks/`
- `proxyurl` 协议白名单
- `proxyutil` ss 分支
- `tlsfingerprint` ss 适配
- 定制迁移 `9001_*`
- `Proxy` 实体 `Extra` 字段与 `URL()` 拼接
- 前端代理表单按协议切换标签

**最脆弱的一处是 `proxyurl.Parse` 的 `allowedSchemes` 白名单**——上游若改动该 map，冲突解决时容易丢失 `ss` 条目。所幸丢失后的症状是「创建 ss 代理直接报错」，属显性失败，可及时发现。

## 11. 已知局限

- 机场节点为**多人共享出口 IP**。本方案解决"借道访问上游"，不解决"多账号 IP 分散降低风控关联"。后者需住宅代理 / 独享 IP，而那类服务通常直接提供 http/socks5，用现有功能即可，无需本改动
- 仅支持当前订阅所用的 cipher 与插件组合，订阅方若变更协议需再行评估
- `go-shadowsocks2` 约两年未更新
