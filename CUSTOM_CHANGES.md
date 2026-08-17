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

## v0.1.177-custom.2（2026-08-17）

Antigravity daily 端点对齐官方客户端：`antigravityDailyBaseURL` 由已废弃的
`https://daily-cloudcode-pa.sandbox.googleapis.com` 改为 `https://daily-cloudcode-pa.googleapis.com`
（`backend/internal/pkg/antigravity/oauth.go`）。

- **动因**：上游 issue #5611——持有 Google AI Pro（g1-pro-tier）的 Antigravity 账号打生产端点
  `cloudcode-pa.googleapis.com` **必定 429**（账号档位级拒绝，与请求内容无关），官方 Antigravity IDE
  实际把推理请求发到 daily 端点。上游 PR #5625 提了同样的一行修改，但**截至本次发布仍是 open 未合并**，
  故先在定制分支落地。
- **影响面**：该常量同时是 `BaseURLs[1]`（`GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL=daily|sandbox` 时
  `resolveAntigravityForwardBaseURL` 取的就是它）和 `client.go` 的 `privacyBaseURL`（隐私设置 API）。
  隐私 API 此前 URL 走 sandbox 主机、`req.Host` 却写死官方 daily 主机，本次改完两者一致。
- **不改变默认行为**：网关转发默认仍走 prod（`BaseURLs[0]`），daily 端点需显式设
  `GATEWAY_ANTIGRAVITY_FORWARD_BASE_URL=daily` 才启用——值必须是关键字 `daily`/`sandbox`，
  填完整 URL 会静默回落到 prod（issue #5611 评论区踩过）。
- **护栏**：`oauth_test.go` 的 `TestForwardBaseURLs_Daily优先` 增加字面量断言，防止合并上游时被带回 sandbox 主机名。
- **已知副作用**（来自 issue 评论，非本次改动引入）：daily 端点下弃用别名 `gemini-3.1-pro-high` 返回 400，
  需在账号 model_mapping 里改映射到 `gemini-pro-agent`。开启 daily 后无 prod 兜底
  （`antigravity_gateway_retry.go` 的 `availableURLs` 只有一个元素），且是全局开关、非按账号。

## v0.1.177-custom.1（2026-08-16，main）

合并上游 tag `v0.1.177` 到 `main`（上一基线 `v0.1.168`，中间跨 169–176，共 447 个非合并提交）。冲突处理要点：

- **README** 保留定制版说明；`README_CN.md` / `README_JA.md` 跟随上游删除（本仓库已删，保持删除）
- VERSION 写 `0.1.177-custom.1`
- **wire 链路**：上游为 `AccountPatrolRecordStore` 引入新依赖注入需求，但**定制分支没有 provider**（`account_patrol_record_repo.go` 存在 repository→service 的 import 环，wire 无法自动生成）。处理：`cmd/server/wire.go`（组合根）加 `wire.Bind(AccountPatrolRecordStore → *repository.AccountPatrolRecordRepository)`，`repository/wire.go` ProviderSet 加 `NewAccountPatrolRecordRepository`，`wire_gen.go` 用 `wire@v0.7.0` 重新生成。**此处带注释说明循环依赖成因，合并上游再动 wire 前先读**
- **`openai_gateway_grok.go` / `account_test_service.go` / `grok_quota_service.go` 及测试**：上游 v0.1.177 引入 `applyGrokUpstreamFailureDecision`（429 冷却、pool mode 区分、免费额度按上游 reset header）；**本仓库继续用定制策略**——429+免费额度耗尽封禁 24h、裸 429 指数递增封禁、**非 429 一律直接 SetError（不区分 pool mode、不用冷却）**。上游函数保留但**不被调用**，删除前确认调用点
- **9 个上游 grok 测试断言改写**为定制行为预期（403 置错而非 temp-unsched、免费额度 24h 而非上游 reset 等），集中在 `service/grok_upstream_failure_test.go`、`service/grok_p2_test.go`、`service/grok_upstream_errors_test.go`。合并上游若看到这些测试被改回上游语义即定制被覆盖
- **`grok_oauth_client_test.go`**：上游把 `NewGrokOAuthClient` 改为 fail-closed 的 `ValidatedTokenURL()`，注入 loopback token endpoint 必须 `EnvAllowUnsafeURLOverrides=true`。自有的 `TestGrokOAuthClientStatusErrorCarriesUpstreamStatusCause` 补上该 env（否则打真实 x.ai 返回 401）
- **前端 `AccountsView.vue` / `AccountBulkActionsBar.vue`**：上游 v0.1.177 重做全选（`handleSelectAllResults` / `total-results` / `all-results-selected` / `select-all-results`，一次拉 1000 条）；**本仓库保留定制契约**（`filtered-total` / `all-filtered-selected` / `@select-filtered` / `selectAllFiltered` + `selectedAccountMeta` 平台元数据缓存），不采用上游全选重构。合并时保留本仓库 props/emits、删除上游 handler
- **上游新增测试 `AccountsView.selectAllResults.spec.ts` 删除**（测的是被废弃的 select-all-results 功能；定制 select-filtered 已有 `AccountsView.selectionRefresh.spec.ts` 覆盖）
- **`handleBulkProbeUpstreamBilling`**：改为与单条探测一致的 `refreshAccountsAfterUpstreamBillingProbe()`（探测成功后无条件重载当前页），满足上游新增测试「批量探测后刷新页面并显示同步倍率」（此前仅在按 `upstream_billing_rate` 排序时才重载）
- **前端 i18n `admin/accounts.ts`**：双方 key 并存；同名 key（`selectingAll`、`selectAllFailed`）取本仓库文案
- 批量编辑测试合并双方用例：本仓库「批量改到期时间」+ 上游「倍率同步冲突提示/专用错误」并存
- `.goreleaser.simple.yaml` 自动合并成功且保留 `prerelease: false`、archives、checksums（合并时勿改）

上游 0.1.169–0.1.177 主要能力：Grok 密码/SSO 登录、ReAuth 弹窗与 OAuth 凭证形态统一、Grok Voice TTS/STT/Realtime 与分组音频定价、视频按模型族定价、模型目录与可配置映射、P2 模型额度软封与 spending reauth、stream idle 换号与 team+model 冷却、渠道监控 v2（被动聚合、只读 API、Ops 界面）、Grok JWT tier 识别订阅档位、grok-4.6 目录与定价、`/x_search` 独立搜索与计费、Codex OAuth 设备指纹收敛、分组逐模型定价与长上下文阶梯开关、账单探测扩展到全部 API-key 平台并可选同步上游声明倍率、内容审核走可配置代理、按筛选结果全选账号（上游版）、批量删除并发限制、邮件域名注册配额开关、备份大文件分片、安全审计窄范围与 Qwen3Guard 辅助字段。

## v0.1.168-custom.2（2026-07-31）

修复严格客户端（grok-shell / Grok-Desktop，Rust+serde 实现）完全无法使用的一系列问题。
共 7 处，全部带回归测试。**触发场景**：客户端走 `/v1/responses` 或 Anthropic Messages 协议，
每个缺失字段都会让整条流直接失败（`serialization error: missing field xxx`）。

### Responses 协议必填字段（5 处）

`omitempty` 吃掉了规范要求必须存在的零值：

| 字段 | 成因 | 位置 |
|---|---|---|
| `created_at` | `ResponsesResponse` 根本没这个字段 | `apicompat/types.go` + 6 个构造点 |
| `sequence_number` | omitempty + 首个事件 seq=0 恒被丢弃 | `apicompat/types.go` |
| `content` / `arguments` / `summary` | `response.completed` 的 output 项走默认序列化，没走 `responsesItemWire` | `apicompat/types.go`（`ResponsesOutput.MarshalJSON` 统一走 wire） |
| `annotations` | message item 内的 content 数组没跟上 `outputTextPartWire` | `apicompat/responses_stream_event_wire.go`（`messageContentWire`） |
| `input_tokens_details` | 指针 + omitempty，nil 时整个字段消失 | `apicompat/types.go`（新增 `ResponsesUsage.MarshalJSON`） |

护栏：`apicompat/responses_wire_required_fields_test.go` 把整条流真实跑一遍，
按类型逐条核对 19 种事件 / 4 种 item / 2 种 part / usage 两级明细，两条转换链路都覆盖。
**再遇到 `missing field xxx` 先把字段加进那份清单再跑，不要靠线上报错一个个补。**

### 根路径网关别名被前端中间件吞掉（1 处，最隐蔽）

`internal/web/embed_on.go` 的 `shouldBypassEmbeddedFrontend` 是一份**手工维护的放行清单**，
与 `routes/gateway.go` 实际注册的根路径别名**早已脱节**：`/responses`、`/models`、`/images/*`
在清单里，而 **`/messages`、`/chat/completions`、`/embeddings` 不在**。

该中间件是 `r.Use()` 全局注册、位置在所有路由**之前**，漏放行的路径被它直接返回
**200 + index.html**——不是 404、没有任何错误提示，客户端拿到 HTML 只能一直重试转圈。
三层伪装（200 状态码 + 有响应体 + 访问日志记成功）让它极难定位，唯一破绽是
`latency_ms: 0` 和缺失 `client_request_id`。

已补齐清单，并加双向回归测试（`web/embed_test.go`）：正向锁住所有根路径别名必须放行，
反向锁住 `/login`、`/dashboard` 等 SPA 路由不能被误放行。**这份清单与根路径路由注册必须同步维护。**

### Anthropic thinking 块缺 `signature`（1 处）

Antigravity 走的是独立的 `internal/pkg/antigravity` 包（用 `map[string]any` 手工拼 JSON），
不经过 `apicompat`。thinking 块的 `content_block_start` 只有 `{"thinking":"","type":"thinking"}`，
缺 `signature`——官方格式是 `{"type":"thinking","thinking":"","signature":""}`。

- `antigravity/stream_transformer.go`：两处手工构造的 thinking 块补 `signature`
- `antigravity/claude_types.go`：`ClaudeContentItem` 新增 `MarshalJSON`，thinking 项恒带 thinking + signature
- `apicompat/types.go`：`AnthropicContentBlock` 的 thinking 分支同样恒带 signature（其它链路用）
- ⚠️ **上游测试 `TestStreamingReasoning` 的断言被改为包含 signature**，合并上游时若被改回即为定制被覆盖

### 测试连接的模型下拉改用账号真实 mapping

`handler/admin/account_handler.go` 的 `GetAvailableModels`，Antigravity 分支此前**无条件返回**
硬编码的 `antigravity.DefaultModels()`，忽略账号的 `model_mapping`（「同步上游支持的模型」拉取
的上游真实列表）。导致「测试账号连接」的下拉与「编辑账号 → 模型限制」对不上，还能选到账号
不支持的模型。已按 OpenAI/Gemini/Grok 三个分支的既有模式补齐：有 mapping 就用 mapping
（不在 DefaultModels 里的上游新模型按名回落），无 mapping 才回落默认；输出按 ID 排序。

### ⚠️ 试过但回滚：不要补发 `response.in_progress`

官方 Responses SSE 序列是 `created → in_progress → output_item.added`，本仓库从不发
`in_progress`（代码里该字符串只出现在**接收**方向）。曾据此补发，结果 grok-shell 从
「只丢开头」恶化为「完全不渲染」，而 `usage_logs` 显示那几次请求全部成功、output_tokens
661/783/811 正常。已回滚并在两处代码留注释。

**教训**：「我们违反了官方协议」是事实，但由此推出「补上就能解决用户的症状」是没有证据的猜测。

## v0.1.168 合并（2026-07-31，main）

合并上游 tag `v0.1.168` 到 `main`（上一基线 `v0.1.163`，中间跨 164/165/166，共 121 个非合并提交）。冲突处理要点：

- README 保留定制版说明；`README_CN.md` / `README_JA.md` 本仓库已删除，保持删除
- VERSION 写 `0.1.168`（上游 tag 内 VERSION 文件仍是 0.1.166，按标签基线写入；发布时 CI 再同步为 `0.1.168-custom.N`）
- **wire 全链路（`cmd/server/wire.go`、`wire_gen.go`、`wire_gen_test.go`、`handler/wire.go`、`service/wire.go`）**：上游新增 `OllamaCloudUsageService`，与定制的 `AccountPatrolService` 落在同一批参数/清理步骤上——**两者都保留**，顺序 `accountPatrol, ollamaCloudUsage`
- **`openai_gateway_grok.go`**：上游把 401 → 10 分钟、402 → 30 分钟临时不可调度，5xx 按 pool mode 做 2 分钟冷却；**本仓库继续用定制策略「非 429 一律直接 SetError」**，未采纳上游这几条分支
- **`account_test_service.go`**：同上，上游给测试路径的 402 加了 30 分钟临时不可调度，定制保持「非 429 的 HTTP 失败直接置错」
- 相应改写了上游随之新增的 3 个测试（它们断言的是上游策略，直接合入必红）：
  - `TestAccountTestServiceGrokOAuthPaymentRequiredTemporarilyUnschedulesAccount` → `...PaymentRequiredSetsAccountError`
  - `TestHandleGrokAccountUpstreamError5xxRespectsPoolMode` → `...5xxSetsErrorRegardlessOfPoolMode`（定制不区分 pool mode）
  - `TestHandleGrokAccountUpstreamError402RecoversAfterCooldownExpiry` 删除（定制下 402 直接置错，无冷却可恢复）
- `admin_service_bulk_update_test.go`：采用上游扩充后的 repo stub（新增 `Create`/`Update`/`bindGroupsByAccount` 等），**保留**定制字段 `bulkUpdateValue` 与 `TestAdminServiceBulkUpdateAccountsExpirationTriState`
- `go.sum`：取上游版本后 `go mod tidy` 重新补回 `go-shadowsocks2` 及其传递依赖
- 前端 `api/admin/accounts.ts` 与 `i18n/{en,zh}/admin/accounts.ts`：定制的 `AccountPatrolSettings` / `patrol` 文案与上游新增的 `OllamaCloudUsage*` / `ollamaCloud` 文案**并存**
- ss 出站代理必查清单 9 条逐条回读确认未丢（`normalizeProxyURL` 保留 RawQuery、`UpdateProxy` 的 `input.Extra != nil` 守卫、`req_client_pool` 的 ss 分支、`import-subscription` 路由、ent `extra` 生成代码、`9001_` 迁移等）

上游 0.1.164–0.1.168 主要能力：Passkey 登录与设置开关、OpenAI Live（Realtime）网关与 macOS attestation、Ollama Cloud 官方用量抓取与自动刷新、复合模型路由（`composite_model_routes` 表）、模型广场与分组维度定价展示、Kimi K3 与 claude-opus-5 适配、面板 API 限流、客户端 session id 持久化、支付宝移动端 precreate deep link，以及大量网关兼容性与移动端布局修复。

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
| 测试失败/Grok 非429请求错误直接置错 + 手动置错（HTTP 错误/取 token 失败→SetError；429 仍限流；永久 error 亦可管理员手动/批量 set-error）<br>**不区分 pool mode，也不采纳上游对 401/402/5xx 的临时不可调度分支**（上游 v0.1.168 起有这些分支，合并时必删） | `service/account_test_service.go`、`service/openai_gateway_grok.go`（`handleGrokAccountUpstreamError` 的 `default` 分支）、`handler/admin/account_handler.go`（`SetError`/`BatchSetError`）、`routes/admin.go`、前端账号操作菜单与批量栏 |
| **账号巡检**（全局开关；定期分批连接测试；失败 SetError；成功 Recover） | `service/account_patrol_service.go`、`handler/admin/account_patrol.go`、`routes/admin.go`、前端 `AccountPatrolSettingsModal.vue` / `AccountsView.vue` |
| **temp 累计 3 次自动置错**（任意入口真正 re-entry 计次；窗口延长不计；达 3 次 → SetError + 清 temp） | `service/temp_unsched_entry_counter.go`、`repository/temp_unsched_entry_counter_cache.go`、`repository/account_repo.go`（`SetTempUnschedulable` / Grok CAS 路径挂钩） |
| **测试/恢复成功 → 完全正常**（ClearError + 强制 `schedulable=true` + 清 temp re-entry 计数） | `service/ratelimit_service.go`（`RecoverAccountState` / `RecoverAccountAfterSuccessfulTest`） |
| **Grok 连接测试允许非调度态取 token**（error/暂停/temp 可测；网关路径仍要求可调度） | `service/grok_token_provider.go`（`GetAccessTokenForManualTest`，v0.1.162 起采用上游接口；`withAccountConnectionTestPath` 仍保留给其它路径）、`oauth_refresh_api.go`、`account_test_service.go` |
| grok 刷新失败置错（4xx 非429→SetError） | `service/grok_refresh_failure.go`、`pkg/xai/errors.go`、`repository/grok_oauth_client.go`、`handler/admin/account_handler.go`、`grok_oauth_handler.go` |
| CPA(xai-*.json)导入 | `handler/admin/account_data_xai.go`、`account_data.go`（`XaiAccounts`）、前端 `ImportDataModal.vue` / `utils/xaiImport.ts` |
| **shadowsocks 出站代理**（ss + simple-obfs/tls 内嵌拨号；Clash 订阅导入）<br>**⚠ 见下方「ss 出站代理：合并上游必查清单」** | `pkg/shadowsocks/`（config/dialer/obfs_tls）、`pkg/clashsub/`、`pkg/proxyurl/parse.go`（`allowedSchemes` 含 `ss`）、`pkg/proxyutil/dialer.go`（`case "ss"`）、`repository/http_upstream.go`（TLS 指纹分派 `case "ss"`）、`repository/req_client_pool.go`（`applyReqClientProxy`）、`service/openai_ws_client.go`（`proxyHTTPClient`）、`migrations/9001_custom_proxy_extra.sql`、`ent/schema/proxy.go`（`extra`）、`service/proxy.go`（`Extra` + `URL()` 拼 query）、`handler/admin/proxy_subscription.go`、`handler/admin/proxy_handler.go`（`oneof` 白名单含 ss）、前端 `ProxiesView.vue` / `types/index.ts` / `api/admin/proxies.ts` |
| 导入后刷新+测试流水（取代 probe；**合并上游须保留 importData 替换点**） | `handler/admin/grok_import_pipeline.go`、`account_data.go` |
| **Responses SSE 必填字段**（`created_at` / `sequence_number` / `annotations` / `input_tokens_details` / completed 的 output 项字段）<br>严格客户端(grok-shell 等 Rust+serde)缺一个就整条流失败 | `pkg/apicompat/types.go`（`ResponsesResponse.CreatedAt`、`SequenceNumber` 无 omitempty、`ResponsesOutput.MarshalJSON` 统一走 wire、`ResponsesUsage.MarshalJSON`）、`responses_stream_event_wire.go`（`messageContentWire` 带 annotations/logprobs）<br>护栏：`responses_wire_required_fields_test.go` |
| **根路径网关别名的前端放行清单**（`/messages` `/chat/completions` `/embeddings`）<br>⚠ 漏放行→返回 200+HTML 而非 404，客户端静默转圈，极难定位 | `web/embed_on.go`（`shouldBypassEmbeddedFrontend`）+ `routes/gateway.go` 根路径 `/messages` 路由<br>护栏：`web/embed_test.go` 双向断言。**该清单与根路径路由注册必须同步维护** |
| **Anthropic thinking 块恒带 `signature`**（官方格式 `{"type":"thinking","thinking":"","signature":""}`） | `pkg/antigravity/stream_transformer.go`(2 处)、`pkg/antigravity/claude_types.go`(`ClaudeContentItem.MarshalJSON`)、`pkg/apicompat/types.go`(`AnthropicContentBlock` thinking 分支)<br>⚠ 上游 `TestStreamingReasoning` 断言已改为含 signature |
| **测试连接模型下拉用账号 `model_mapping`**（Antigravity 分支原本无条件返回硬编码 DefaultModels） | `handler/admin/account_handler.go`（`GetAvailableModels` 的 Antigravity 分支） |
| **不要补发 `response.in_progress`**（试过更糟，见上文 v0.1.168-custom.2 条目） | `pkg/apicompat/anthropic_to_responses_response.go`、`chatcompletions_responses_bridge.go` 的注释 |
| **Antigravity daily 端点用官方主机名**（`daily-cloudcode-pa.googleapis.com`，非 `.sandbox.`）<br>上游 PR #5625 同款改动但未合并，合并上游时**必查是否被带回 sandbox** | `pkg/antigravity/oauth.go`（`antigravityDailyBaseURL`）<br>护栏：`pkg/antigravity/oauth_test.go` 的 `TestForwardBaseURLs_Daily优先` 字面量断言 |

### ss 出站代理：合并上游必查清单

上表只列了「改动在哪」。下面这些点**丢失后不会立刻报错**（编译过、测试过、面板点得动），
但节点会静默失效或参数被悄悄清空，所以合并上游解冲突后必须逐条回读确认。

| 必查点 | 丢失后的后果 |
|---|---|
| **`repository/http_upstream.go`：`normalizeProxyURL` 不得清空 `RawQuery`** | **实战踩过的坑，症状最隐蔽。** 该函数为算连接池缓存键做 URL 归一化，原本有一行 `parsed.RawQuery = ""`，而它返回的**同一个** `parsed` 又被用来建 transport。ss 节点的 obfs 插件参数（`plugin`/`mode`/`obfs-host`）正挂在 query 上，被清掉后建出的是**裸 ss 连接**——要求 obfs 的服务端静默丢弃，表现为 `Post ...: EOF`。代理延迟测试、订阅导入、单元测试全部正常，只有真实转发失败，极难定位。保留 query 同时保证了连接池隔离（obfs 参数不同的节点不会共用同一个池）。回归测试见 `http_upstream_proxy_query_test.go` |
| **`service/account_test_service_openai_test.go`：401 用例的断言必须是 `API returned 401`** | 定制把测试路径 401/非429 改成**直接永久 SetError**（`markAccountErrorOnTestHTTPFailure`），文案是 `account test failed: API returned 401: ...`；上游那条路走 ratelimit 产生 `Authentication failed (401)`。**v0.1.163 合并时该测试文件取了上游版，断言被换回上游文案，导致测试红了**——代码是对的、测试是错的。合并后若看到该用例失败，先确认是不是又被覆盖回上游断言 |
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
