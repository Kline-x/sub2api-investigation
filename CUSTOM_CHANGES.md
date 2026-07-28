# 定制改动记录

本仓库相对上游 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的全部定制改动，按版本记录。**每次发布新版本时在此追加对应条目。**

## shadowsocks 出站代理（2026-07-27，`develop/xuyang/ss-obfs-proxy`，待发布）

让 sub2api **单一二进制内**原生支持通过机场订阅节点出站，解决「服务器访问不了上游 AI API」。不新增容器/进程，面板一键更新后即可用；不改宿主机路由，宿主机默认出口 IP 不受影响。

- **feat**：内嵌 `ss + simple-obfs(tls)` 拨号（`pkg/shadowsocks/`）。加密层用 `go-shadowsocks2`（Apache-2.0），obfs 伪装层自实现（未引用 GPL 实现源码——mihomo 的 `transport/simple-obfs` 子包被 pkg.go.dev 标为 GPL-3.0，设计阶段已否决）
- **feat**：Clash 订阅解析（`pkg/clashsub/`）+ 订阅导入接口 `POST /admin/proxies/import-subscription`（支持 `dry_run` 预览；不受支持的节点回传中文跳过原因，不静默丢弃）
- **feat**：`proxies` 表新增 `extra` JSONB 列（定制迁移 `9001_`，避开上游编号段），存插件参数；`Proxy.URL()` 拼成 query（按 key 排序保证字符串稳定，否则击穿 httpclient 的 transport 缓存）
- **feat**：ss 接入两条代理路径——`proxyutil.ConfigureTransportProxy` 与 TLS 指纹路径（`repository/http_upstream.go`）。后者此前 ss 会落入「未知协议回退」，隧道通但**不带指纹**
- **feat**：前端协议下拉加 ss，选中时「用户名」标签变「加密方式」（ss 的 cipher 占 userinfo 的 username 位）；代理列表页新增「从订阅导入」（先 dry_run 预览再确认）
- **验证**：真实机场节点端到端打通，走完整生产链路 `Proxy.URL() → proxyurl.Parse → proxyutil → HTTPS` 请求 api.anthropic.com 返回 HTTP 401（带 request_id）

设计文档 `docs/superpowers/specs/2026-07-27-ss-obfs-proxy-design.md`，实施计划 `docs/superpowers/plans/2026-07-27-ss-obfs-proxy.md`（均被 gitignore，需 `git add -f`）。

**已知范围限制**：仅支持 ss 协议 + obfs 的 tls 模式（当前订阅所用）；不支持 vmess/vless/hysteria2、不支持 ss-2022 密码套件、不支持 obfs 的 http 模式、不做 UDP；订阅同步为手动触发，**不做定时自动同步**（自动删除下线节点会让绑定它的账号悬空，需独立的状态机，YAGNI）。

## v0.1.163 合并（2026-07-23，custom/v0.1.163）

合并上游 `v0.1.163` 到分支 `custom/v0.1.163`（自 `main` 切出）。冲突处理要点：

- README 保留定制版说明与文档索引
- VERSION 对齐上游基线 `0.1.163`（上游 tag 内 VERSION 文件仍为 0.1.162，本仓库按标签基线写入）
- (`openai_gateway_grok.go`：保留上游 `applyGrokForbiddenPolicy`（命中配置规则时模型/时长隔离）；401 与未命中规则的 403/其它非 429 **继续定制 SetError**；**保留** grok 免费额度 24h 与裸 429 指数递增封禁)
- `upstream_models.go`：采用上游统一 Grok OAuth 模型同步（`GetAccessTokenForManualTest`、CLI 头与身份头）；**保留** OAuth `/models` 失败回退 `xai.DefaultModelIDs()` 路径
- `AccountsView.vue`：采用上游工具菜单/移动端适配；**保留**账号巡检设置与巡检记录入口
- 保留自有更新源（`Kline-x/sub2api-investigation`）、`.goreleaser.simple.yaml` prerelease:false、Grok OAuth 429 持续切号、temp 三次置错、批测/CPA 等既有定制

上游 0.1.163 主要能力：分组级 OpenAI 推理策略、Grok `/responses/compact` 与链式中继受保护视频、Redis ACL 用户名、Grok OAuth 模型同步与策略 403 模型级隔离、Codex 客户端工具 Responses 往返、多处移动端布局修复、优雅关停缓冲用量/计费丢失修复等。

## v0.1.162 合并（2026-07-22，main）

合并上游 `v0.1.162` 到 main。冲突处理要点：

- README 保留定制版说明；删除上游恢复的 `README_CN.md` / `README_JA.md`
- VERSION 对齐上游基线 `0.1.162`
- Grok 连接测试：采用上游 `GetAccessTokenForManualTest`（覆盖本地 `withAccountConnectionTestPath` 的同类语义，且更完整地绕过调度门）
- `openai_ws_http_bridge`：采用上游 typed failover / 账号冷却副作用路径（写客户端前切号、Grok/OpenAI 冷却）；不叠加本地 `persistOpenAIWSRateLimitSignal`，避免 OpenAI 限流记两次
- 前端 `admin.system.rollback` 单测：对齐上游 15 分钟超时参数
- **保留** Grok OAuth 429 持续切号、temp 三次置错、批测/CPA、自有更新源、官方上游版本展示等定制

上游 0.1.162 主要能力：客户端 IP 解析可配置（可信代理 + 自定义请求头）、异步生图对象存储后台配置、Grok 客户端工具缓存（Claude Desktop/Codex Lite/Trae）、更新检查支持 GitHub Token、订阅到期精确到分钟、OpenAI 配额标准错误、Codex 模型发现兼容标准列表、API Key 部分更新不再清空 IP 名单、提示词审计仅 blocking 时 fail-closed、S3 临时密钥持久化护栏等。

## v0.1.162-custom.1（2026-07-22，当前线上目标版本）

基于上游 v0.1.162 的首个定制发布（合入 codex/account-patrol-direct-error）：

- **feat**：Grok 请求错误（非 429）与账号连接测试失败（非 429）直接 SetError，不再进入临时不可调度
- **feat**：账号巡检（全局开关 + 间隔/批次/并发；分批连接测试，失败置错、成功恢复）
- **feat**：账号巡检记录页（落库每批结果 + 失败账号 ID；保留 7 天；支持单条删除/清空全部）
- **feat**：Grok OAuth 支持同步上游模型（CLI /models，失败回退默认列表）
- **fix**：批量更新有勾选时仅改选中账号，避免误点「按筛选」更新全表
- **继承**：Grok OAuth 429 持续切号、temp 三次置错、批测/CPA、自有更新源等既有定制

## v0.1.161-custom.1（2026-07-19）

基于上游 v0.1.161。首个定制发布，包含此前 0.1.160-custom 全量定制 + 上游 0.1.161：

- merge：上游 v0.1.161（模型级 temp 冷却隔离、池模式 temp 规则、瞬时耗尽 503、Grok 媒体/权限修复、step-up 2FA 开关默认关、会话绑定默认关等）
- fix：Grok OAuth 429 持续切号（保留，不采用上游 follow-up 一次停切）
- feat：版本徽章展示官方上游最新版本与发布日志入口
- fix：SSE 首 ping 推迟、Anthropic message_stop、批量操作 loading
- 继承：批测/CPA、temp 三次置错、测试成功恢复、Grok 非调度态可测、自有更新源等

## v0.1.161 合并（2026-07-19，main）

合并上游 `v0.1.161` 到 main。冲突处理要点：

- README 保留定制版说明（不恢复上游安装文档）
- VERSION 对齐上游基线 `0.1.161`
- `AccountHandler` 同时保留本地 `accountTester`（批测/导入流水）与上游 `grokImportProber` 类型
- `BulkUpdate` 同时保留本地 `ExpiresAtSet` 与上游 `ProbeEnabled`→Extra 写入
- `AccountsView` 批量探测上游倍率：保留 `runWithBulkBusy` loading，并接入上游 `refreshUpstreamBillingSortedList`
- **保留 Grok OAuth 429 持续切号**（不采用上游 follow-up 一次停切）
- 保留 temp 三次置错、测试成功恢复、自有更新源、官方上游版本展示等定制

上游 0.1.161 主要能力：模型级 temp 冷却隔离、池模式 temp 规则、瞬时耗尽 503、Grok 媒体/视频代理与权限修复、step-up 2FA 开关默认关、会话 IP/UA 绑定默认关、Responses 流式 content_part 补全等。

## v0.1.160-custom.3（2026-07-19）

基于上游 v0.1.160。相对 custom.2 的增量：

- fix：恢复 **Grok OAuth 429 持续切号**（合并上游 follow-up 预算后改回合并前语义：429 不停切，直到无可用账号/其它退出条件；OpenAI 仍仅风暴停切）

## v0.1.160-custom.2（2026-07-19）

基于上游 v0.1.160。相对 custom.1 的增量：

- feat：版本徽章展示 **官方上游最新版本**（`Wei-Shaw/sub2api`），落后时天蓝色提醒，并提供官方发布/更新日志入口（仅提示，不驱动本仓库在线更新）
- 后端 `check-updates` 返回 `upstream_latest_version` / `upstream_has_update` / `upstream_release_info`；基线比较只看 X.Y.Z（同基线 custom 不算落后）

## v0.1.160-custom.1（2026-07-19）

基于上游 v0.1.160。相对 v0.1.156-custom.3 + 上游 0.1.160 合并的增量：

- fix：等槽位 SSE **首个 ping 推迟 5s**，短等待不再固化 HTTP 200，降低 Claude Code `empty or malformed response (HTTP 200)`（`gateway_helper.go` / `user_msg_queue_helper.go`）
- fix：Anthropic 流错误 SSE 在 `error` 事件后补 `message_stop` 协议终止帧（`gateway_handler.go`）
- fix：批量操作 busy 状态 `await nextTick()` 后再发请求，确保按钮 disable/处理中文案可见（`AccountsView.vue`）
- 继承：批测/CPA 导入、temp 三次置错、测试成功恢复、Grok 非调度态可测、批量操作 loading、探测上游倍率等

## v0.1.160 合并（2026-07-18，main）

合并上游 `v0.1.160` 到 main。冲突处理要点：

- 保留本地批测/CPA 导入、temp 三次置错、测试成功恢复、Grok 非调度态可测
- 接入上游「探测上游倍率」批量/单账号能力与相关路由
- 保留 `GET /accounts/ids` 与自有更新链路/发布配置
- README 继续使用定制版说明（不恢复上游多语言 README）
- wire 继续注入 `TempUnschedEntryCounterCache`

## v0.1.156-custom.3（2026-07-18）

基于上游 v0.1.156。相对 custom.2 的功能增量：

- feat：账号批量测试 `POST /admin/accounts/batch-test`（支持 `models_by_platform` 按平台选模型，Grok 默认 `grok-4.5`）
- feat：测试失败临时不可调度；管理员可手动/批量置错
- feat：temp 真正 re-entry 累计 3 次自动 `SetError` 并清 temp（窗口延长不计）
- feat：测试成功 / 恢复状态 → 完全正常（ClearError + 强制 `schedulable=true` + 清 temp re-entry 计数）
- feat：Grok 连接测试允许 error/暂停/temp 等非调度态取 token 并刷新（网关调度路径仍要求可调度）
- feat：CPA(`xai-*.json`) 导入 + 导入后后台「刷新→测试」流水（取代配额探测）
- feat：Grok 手动/批量刷新永久失败（invalid_grant / 上游 4xx 非 429）自动置错
- fix：OpenAI compact 探测单测 stub 补 `SetTempUnschedulable`，避免测试失败路径空指针

## v0.1.156-custom.2（2026-07-16）

基于上游 v0.1.156。相对 custom.1 的修订：

- fix：前端回滚面板「手动回退方式」命令常量指向自有仓库（`VersionBadge.vue`：`GITHUB_REPO` → `Kline-x/sub2api-investigation`，`DOCKER_IMAGE` → `ghcr.io/kline-x/sub2api`）
- fix：`deploy/install.sh` 的版本查询与下载仓库指向自有仓库
- test：补 `ListRollbackVersions` 的 custom 版本混排回归用例

## v0.1.156-custom.1（2026-07-16，已撤版）

合并上游 v0.1.156 + 自有更新链路改造。撤版原因：内嵌前端的手动回退命令仍指向上游仓库（custom.2 已修复）。

### 上游 v0.1.156 合并（提交 82223875）

冲突解决要点：

- grok 通用刷新走 OAuth：上游与本地 `ca04e276` 同功能，采用上游接口化实现（`GrokOAuthTokenService`）
- 429 failover 停切判断：上游 followup 预算机制是本地 `26734ffd` 的精化版（继续切号但有界），采用上游
- content block not found 守卫：上游在函数开头加了与本地 `d05ef1bb` 相同判断，删除本地冗余守卫
- 本地独有功能全部保留并与上游新机制组合（见「持续维护的定制功能」）

### 自有更新链路（设计文档：`docs/superpowers/specs/2026-07-16-self-hosted-update-channel-design.md`）

- feat：版本比较支持 `-custom.N` 四段排序（`update_service.go` 的 `parseVersion`/`compareVersions`）
- feat：更新检查与回滚源改为 `Kline-x/sub2api-investigation`（`githubRepo` 常量）
- ci：`.goreleaser.simple.yaml` 补回 linux/amd64 归档 + `checksums.txt`，`prerelease: false`，恢复资产上传

## v0.1.155-custom.2（2026-07-16，回滚基线）

维护分支 `custom/v0.1.155-maint` = 合并 v0.1.156 之前的定制基线 + cherry-pick 上述更新链路改动与 custom.2 修订。作为面板回滚目标保留。

## v0.1.155-custom.1（2026-07-16，已撤版）

同 custom.2 但缺前端常量修订。

## v0.1.155 基线定制（2026-07 上半月，合并进 custom/v0.1.155-maint 与主线）

- `ca04e276` fix：grok 通用刷新路由到 xAI OAuth（后被上游 v0.1.156 等价实现取代）
- `97dfdbbb` feat：账号批量修改到期时间
- `82a1b8ff` fix：grok 免费额度耗尽封禁 24 小时（`grokFreeUsageWindow`，独有）
- `4fdd548e`/`3ae7820f` feat：筛选账号 ID 列表 API（`GET /admin/accounts/ids`，独有）
- `a2bafcb8` feat：按筛选结果全选账号 + token 刷新结果反馈（独有）
- `26734ffd` fix：grok 429 持续切换账号而非切一次就返回 429（上游曾用 followup 预算；**v0.1.160-custom.3 起恢复持续切号**）
- `d05ef1bb` fix：Claude Code 调工具报 content block not found（Responses→Anthropic 流转换孤儿 delta；上游 v0.1.156 同修）
- `2be10837` fix：grok 裸 429 连击指数递增封禁，消除 2 分钟兜底抖动（`grokBare429State`，独有，与上游自适应冷却叠加取较晚 reset）

## 持续维护的定制功能清单（合并上游时须保留）

| 功能 | 位置 |
|---|---|
| grok 免费额度耗尽封 24h | `openai_gateway_grok.go`（`grokFreeUsageWindow` 等常量）、`grok_quota_service.go` |
| grok 裸 429 指数递增封禁 | `openai_gateway_grok.go`（`grokBare429State`）、`openai_gateway_service.go`（`grokBare429States`） |
| grok OAuth 429 持续切号（合并上游 follow-up 预算后恢复） | `openai_account_runtime_block_fastpath.go`（`ShouldStopOpenAIOAuth429Failover`：Grok 429 不停切） |
| 账号批量改到期时间 | `admin_service.go` / `BulkEditAccountModal.vue` |
| 筛选账号 ID API + 全选 | `routes/admin.go`（`/accounts/ids`）、`AccountsView.vue` |
| 4 段版本比较 + 自有更新源 | `update_service.go` |
| 发布流水线定制 | `.goreleaser.simple.yaml` |
| 自有仓库引用 | `VersionBadge.vue` 常量、`deploy/install.sh` `GITHUB_REPO` |
| 账号批量测试端点（POST `/accounts/batch-test`，`models_by_platform` 按平台选模型） | `handler/admin/account_handler.go`（`BatchTest`）、`routes/admin.go`、前端 `AccountsView.vue` / `AccountBulkActionsBar.vue` / `BatchTestConfirmModal.vue` / `accounts.ts` |
| 测试失败/Grok 非429请求错误直接置错 + 手动置错（HTTP 错误/取 token 失败→SetError；429 仍限流；永久 error 亦可管理员手动/批量 set-error） | `service/account_test_service.go`、`handler/admin/account_handler.go`（`SetError`/`BatchSetError`）、`routes/admin.go`、前端账号操作菜单与批量栏 |
| **账号巡检**（全局开关；定期分批连接测试；失败 SetError；成功 Recover） | `service/account_patrol_service.go`、`handler/admin/account_patrol.go`、`routes/admin.go`、前端 `AccountPatrolSettingsModal.vue` / `AccountsView.vue` |
| **temp 累计 3 次自动置错**（任意入口真正 re-entry 计次；窗口延长不计；达 3 次 → SetError + 清 temp） | `service/temp_unsched_entry_counter.go`、`repository/temp_unsched_entry_counter_cache.go`、`repository/account_repo.go`（`SetTempUnschedulable` / Grok CAS 路径挂钩） |
| **测试/恢复成功 → 完全正常**（ClearError + 强制 `schedulable=true` + 清 temp re-entry 计数） | `service/ratelimit_service.go`（`RecoverAccountState` / `RecoverAccountAfterSuccessfulTest`） |
| **Grok 连接测试允许非调度态取 token**（error/暂停/temp 可测；网关路径仍要求可调度） | `service/grok_token_provider.go`（`GetAccessTokenForManualTest`，v0.1.162 起采用上游接口；`withAccountConnectionTestPath` 仍保留给其它路径）、`oauth_refresh_api.go`、`account_test_service.go` |
| grok 刷新失败置错（4xx 非429→SetError） | `service/grok_refresh_failure.go`、`pkg/xai/errors.go`、`repository/grok_oauth_client.go`、`handler/admin/account_handler.go`、`grok_oauth_handler.go` |
| CPA(xai-*.json)导入 | `handler/admin/account_data_xai.go`、`account_data.go`（`XaiAccounts`）、前端 `ImportDataModal.vue` / `utils/xaiImport.ts` |
| **shadowsocks 出站代理**（ss + simple-obfs/tls 内嵌拨号；Clash 订阅导入）<br>**⚠ 见下方「ss 出站代理：合并上游必查清单」** | `pkg/shadowsocks/`（config/dialer/obfs_tls）、`pkg/clashsub/`、`pkg/proxyurl/parse.go`（`allowedSchemes` 含 `ss`）、`pkg/proxyutil/dialer.go`（`case "ss"`）、`repository/http_upstream.go`（TLS 指纹分派 `case "ss"`）、`repository/req_client_pool.go`（`applyReqClientProxy`）、`service/openai_ws_client.go`（`proxyHTTPClient`）、`migrations/9001_custom_proxy_extra.sql`、`ent/schema/proxy.go`（`extra`）、`service/proxy.go`（`Extra` + `URL()` 拼 query）、`handler/admin/proxy_subscription.go`、`handler/admin/proxy_handler.go`（`oneof` 白名单含 ss）、前端 `ProxiesView.vue` / `types/index.ts` / `api/admin/proxies.ts` |
| 导入后刷新+测试流水（取代 probe；**合并上游须保留 importData 替换点**） | `handler/admin/grok_import_pipeline.go`、`account_data.go` |

### ss 出站代理：合并上游必查清单

上表只列了「改动在哪」。下面这些点**丢失后不会立刻报错**（编译过、测试过、面板点得动），
但节点会静默失效或参数被悄悄清空，所以合并上游解冲突后必须逐条回读确认。

| 必查点 | 丢失后的后果 |
|---|---|
| **`repository/http_upstream.go`：`normalizeProxyURL` 不得清空 `RawQuery`** | **实战踩过的坑，症状最隐蔽。** 该函数为算连接池缓存键做 URL 归一化，原本有一行 `parsed.RawQuery = ""`，而它返回的**同一个** `parsed` 又被用来建 transport。ss 节点的 obfs 插件参数（`plugin`/`mode`/`obfs-host`）正挂在 query 上，被清掉后建出的是**裸 ss 连接**——要求 obfs 的服务端静默丢弃，表现为 `Post ...: EOF`。代理延迟测试、订阅导入、单元测试全部正常，只有真实转发失败，极难定位。保留 query 同时保证了连接池隔离（obfs 参数不同的节点不会共用同一个池）。回归测试见 `http_upstream_proxy_query_test.go` |
| `repository/proxy_repo.go`：`SetExtra` / `ClearExtra` / `proxyEntityToService` 中的 `Extra` | obfs 参数不入库或不出库。代理看起来正常，但拨号时没有 obfs 层 → 节点被墙，症状是「连不上，日志只有超时」 |
| **`service/admin_proxy.go`：`UpdateProxy` 里的 `if input.Extra != nil` 守卫** | **全改动里最脆的一行。** 它防的是「不带 Extra 的普通编辑请求（如面板的 `UpdateProxyRequest`）把 obfs 参数清空」。若合并时被改成无条件 `proxy.Extra = input.Extra`，管理员在面板编辑一次 ss 代理（哪怕只改个备注）→ obfs 参数被清空 → 节点静默失效。回归测试见 `service/admin_proxy_extra_test.go`，该文件必须一起保留 |
| `service/admin_service.go`：`CreateProxyInput.Extra` / `UpdateProxyInput.Extra` | 上面两条的传输载体；字段没了则 handler 传不进 service，obfs 参数永远进不到库里 |
| `handler/dto/types.go` + `mappers.go`：`Proxy.Extra` 及 `ProxyFromService` 中的映射 | `/admin/proxies` 系列接口不再返回 `extra`，前端无法展示/回填 obfs 参数（前端 `types/index.ts` 里 `extra` 字段会永远是 `undefined`） |
| `handler/admin/account_data.go` + `proxy_data.go`：`DataProxy.Extra` 与 `validateDataProxy` 的 ss 白名单 | 导入导出丢 extra；导出再导入一轮，ss 代理退化成裸 ss（无 obfs） |
| `routes/admin.go`：`import-subscription` 路由注册 | 订阅导入接口 404，前端「导入订阅」按钮直接失效 |
| `repository/req_client_pool.go`：`applyReqClientProxy` 的 `ss` 分支（`SetProxy(nil)` + `SetDial`） | req/v3 只认 socks5/socks5h，其余 scheme 一律发明文 CONNECT。丢了这段 → 网关请求能通但 **OAuth token 刷新失败**，叠加本仓库「grok 刷新失败 4xx 非 429 → SetError」的定制逻辑，账号会被自动置为 error |
| `service/openai_ws_client.go`：`proxyHTTPClient` 走 `proxyurl.Parse` + `proxyutil.ConfigureTransportProxy` | 上游原版直接 `url.Parse` + `http.ProxyURL`，既违反 proxyurl 包「禁止直接 url.Parse」的硬约定，ss 下也会退化为 CONNECT 失败 |
| `go.mod`：`github.com/shadowsocks/go-shadowsocks2` 依赖 | 合并上游若重写 go.mod/go.sum，依赖被剔除后 `pkg/shadowsocks` 直接编译不过（这条至少是显性失败） |
| **合并上游若动了 ent schema：必须重跑 ent 代码生成** | `ent/schema/proxy.go` 的 `extra` 字段需要 `go generate ./ent`（或项目既定的 ent 生成命令）重新生成 `ent/proxy.go` / `mutation.go` / `migrate/schema.go` 等。只改 schema 不重跑生成，`Extra` 在生成代码里不存在，编译失败或字段被静默丢弃 |

## 已知问题

- CI 的 golangci-lint 作业失败（测试全过，待修）
- 上游偶发：`TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 全量跑 service 包时可能超时失败，纯上游同样存在，可忽略
