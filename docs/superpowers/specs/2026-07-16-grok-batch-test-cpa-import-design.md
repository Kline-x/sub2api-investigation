# 设计:批量测试 / 失败置错 / CPA 导入 / 导入后刷新+测试

日期:2026-07-16
状态:已确认(用户批准);2026-07-17 批测按平台选模型

## 背景与目标

本仓库是 Wei-Shaw/sub2api 的定制分支,主要跑 grok OAuth 账号池。本次新增六项需求:

1. 批量测试账号
2. 测试不成功的账号置为错误状态
3. 面板导入支持 CPA 格式(`xai-*.json`,即 `scripts/cap_to_sub2api_accounts.py` 处理的格式)
4. 导入账号时自动刷新令牌 + 测试账号
5. 测试账号统一用 `grok-4.5` 模型
6. 刷新令牌失败同样置为错误状态

### 已确认的关键决策

| 决策点 | 结论 |
|---|---|
| 批量测试实现方式 | 后端批量端点(非前端循环) |
| 批量测试选模型 | **支持按平台选模型**:请求 `models_by_platform` 映射(如 `{"grok":"grok-4.5","openai":"gpt-5.4"}`);每个出现的平台各选一个。某平台未填则该平台账号走硬编码默认。跨平台勾选也可用 |
| 「失败」界定 | 测试失败(上游 HTTP 错误除 429、以及取 token 失败)→**临时不可调度**;429 走限流;网络无响应不改状态。永久 error 由管理员手动/批量「设为错误」 |
| CPA 导入入口 | 现有「导入数据」弹窗自动识别格式 |
| 导入后刷新+测试范围 | 所有 grok OAuth 账号导入(CPA 与 sub2api-data 均生效) |
| 行为变更 | 导入后「刷新→测试」流水取代原 `scheduleGrokImportProbe` 配额探测(测试路径已含配额快照持久化) |

## 现状摘要(探索结论)

- 单账号测试:`POST /admin/accounts/:id/test`,SSE 输出;`AccountTestService.TestAccountConnection`(`backend/internal/service/account_test_service.go:180`)按平台分派
- **非 SSE 程序化测试已存在**:`RunTestBackground(ctx, accountID, modelID)`(`account_test_service.go:1914`),返回 `ScheduledTestResult{Status, ResponseText, ErrorMessage, LatencyMs, ...}`,定时测试计划在用
- grok 测试默认模型已是 `grok-4.5`(`openai_gateway_grok.go:28` `grokDefaultResponsesModel`),前端 `model_id` 可覆盖
- grok 测试失败目前**不改状态**(对比:Claude 403 / OpenAI 401 会 `SetError`);grok 刷新失败也不改状态
- 批量刷新已存在:`POST /admin/accounts/batch-refresh`(errgroup 并发 10);**无批量测试**
- 导入端点:`POST /admin/accounts/data`(`backend/internal/handler/admin/account_data.go`),`sub2api-data` DataPayload;导入后仅对 grok 做配额探测(`scheduleGrokImportProbe`,并发 3)、对 Antigravity 设隐私
- `SetError` = status=error + 错误信息 + schedulable=false(`account_repo.go:953`)
- 状态常量:`active/disabled/error/unused/used/expired`;限流用 `RateLimitedAt/RateLimitResetAt` 字段,不占 status

## 设计

### 1. 批量测试端点

- 路由:`POST /admin/accounts/batch-test`(挂在 `admin.go` accounts 组,紧邻 batch-refresh)
- 请求:
  ```json
  {
    "account_ids": [1, 2, 3],
    "models_by_platform": {
      "grok": "grok-4.5",
      "openai": "gpt-5.4",
      "claude": "claude-sonnet-4-5-20250929"
    }
  }
  ```
  - `models_by_platform` **可选**;key=账号 `platform` 字符串(与后端 `PlatformGrok` 等一致:`grok`/`openai`/`claude`/`gemini`/`antigravity`…)
  - 某平台 key 缺失或值为空 → 该平台账号传空 model,走 service 硬编码默认
  - **兼容**(可选):若只传顶层 `model_id`(旧字段)且无 `models_by_platform`,则整批共用该 model(仅同平台场景有意义);有 `models_by_platform` 时忽略顶层 `model_id`
- Handler(`account_handler.go` 新增 `BatchTest`):errgroup 并发 10;对每个账号 `modelID = models_by_platform[acc.Platform]`,再 `RunTestBackground(ctx, id, modelID)`
- 响应:`{ total, success, failed, results: [{ id, name, status, error_message, latency_ms }] }`
- 失败置错不在 handler 做,由第 2 条(service 层)统一生效
- **前端(Task 9)**:批量操作栏「批量测试」→ 确认弹窗:
  1. 展示勾选数量
  2. 根据勾选账号 **按 platform 去重**,每个平台渲染一行模型下拉:
     - 该平台任取一个账号调 `getAvailableModels` 填充选项
     - 默认值:grok→`grok-4.5`;其他→列表首项(或空=后端默认,产品可选)
     - 跨平台时同样显示多行,互不影响
  3. 确认后组装 `models_by_platform` 调用 `batchTest(accountIds, modelsByPlatform)`

### 2. 测试失败临时不可调度(service 层,所有入口统一生效)

统一走 `markAccountTempUnschedOnTestHTTPFailure` / `markAccountTempUnschedOnTestFailure`(`account_test_service.go`):

- **429**:保持现有限流逻辑,不置 error/temp
- **其他错误响应**(400/401/403/502/500 等 4xx/5xx):`SetTempUnschedulable`(默认 10 分钟,与 refresh 冷却一致)
- **取 token 失败**(如 oauth refresh state changed):同样临时不可调度
- **网络错误 / 代理故障**(无 HTTP 状态码):不改状态,仅返回错误
- **永久 error**:不由测试自动置;管理员手动 `POST /accounts/:id/set-error` 或批量 `POST /accounts/batch-set-error`

作用范围:单测弹窗、批量测试、导入后测试、定时测试计划,全部经由同一 service 路径。

### 3. 刷新令牌失败置错

两处入口,同一规则:

- `refreshSingleAccount` grok 分支(`account_handler.go:1272-1284`)
- `GrokOAuthHandler.RefreshAccountToken`(`grok_oauth_handler.go:128`)

规则:token 端点返回 **4xx**(典型 `invalid_grant`)→ `SetError`;网络错误/5xx → 不改状态只返回错误。需要从 xai OAuth 刷新错误中区分状态码——若现有错误不带结构化状态码,在 `pkg/xai` 刷新路径补一个带 StatusCode 的错误类型。批量刷新走 `refreshSingleAccount`,自动生效。

### 4. CPA(xai-*.json)导入——格式自动识别

- 后端 `ImportData`(`account_data.go:228`)增加格式识别:请求体若非 `sub2api-data`/`sub2api-bundle`,尝试按 xai 账号解析——单个对象或数组,特征字段 `access_token`(必备)+`refresh_token`
- 服务端转换逻辑移植 `scripts/cap_to_sub2api_accounts.py`:
  - 从 access_token 的 JWT payload 解出 `client_id/team_id/scope`(base64url 解码,无需验签)
  - `email` 取字段,缺省用 JWT claim,再缺省用序号名
  - credentials 含 `access_token/refresh_token/id_token/token_type/client_id/team_id/scope/email/sub/expires_at/base_url`
  - `base_url` 默认 `https://cli-chat-proxy.grok.com/v1`
  - `platform=grok, type=oauth, concurrency=1, priority=0`
- 转换成 `DataAccount` 后走原 `importData` 流程(代理解析、去重、幂等复用)
- 前端 `ImportDataModal.vue`:放宽本地 JSON 校验,识别 xai 格式时显示提示文案(「检测到 N 个 xAI 账号,将转换为 grok OAuth 账号导入」);支持粘贴单对象或数组

### 5. 导入后自动刷新 + 测试

- `importData` 完成后,收集本次新建的 grok OAuth 账号,后台异步执行(goroutine,并发 3,沿用原 probe 的并发与超时风格):
  1. `grokOAuthService.RefreshAccountToken` → 成功则 MergeCredentials + UpdateAccount(同 `refreshSingleAccount` 逻辑)
  2. 刷新成功 → `RunTestBackground(ctx, id, "")`(grok-4.5)
  3. 任一步失败按第 2/3 条规则置 error(刷新 4xx 置错;测试置错在 service 层自动发生)
- **取代**原 `scheduleGrokImportProbe` 调度(probe 代码保留不删,不再从导入路径调用)——测试成功路径已解析并持久化配额快照(`account_test_service.go:773-790`)
- 导入接口立即返回;`DataImportResult` 增加提示字段(如 `background_tasks` 说明「N 个 grok 账号已进入后台刷新+测试」)

### 6. 测试模型规则

- **单测弹窗**:用户选模型;验证 grok 下拉默认选中 `grok-4.5`,若不是则修正
- **批量测试**:确认弹窗 **每个平台各选一个模型**(见 §1)
  - 请求体 `models_by_platform[platform] → model_id`
  - 某平台未选/空 → 该平台账号空 model → 硬编码默认(grok→`grok-4.5`,其他→`DefaultTestModel`)
  - 跨平台勾选:弹窗展示多行下拉,不禁用
- **导入后流水**(仅 grok):仍传空 model → `grok-4.5`,无需用户交互

## 错误处理汇总

| 场景 | 429 | 其他 4xx/5xx | 取 token 失败 | 网络(无响应) |
|---|---|---|---|---|
| 测试(所有入口) | 限流态,不置错 | 临时不可调度 | 临时不可调度 | 不改状态 |
| 刷新令牌(单个/批量/导入后) | 不置错 | SetError(4xx 非429) | - | 不改状态 |

SetError 语义:status=error + schedulable=false + 错误信息,可用现有「批量清错」恢复。

## 测试策略

后端(全部 `-tags unit`,`cd backend && go test -tags unit ./...`):

- BatchTest handler:全部成功 / 部分失败 / 空列表 / 并发正确性
- grok 测试 400/403/502 → 临时不可调度、429 不改(扩展 `account_test_service_grok_test.go`)
- 手动/批量 set-error
- 刷新 4xx 置错、网络错误不置错(扩展 `account_handler_grok_refresh_test.go`)
- xai 格式识别与 JWT 转换:单对象/数组/缺 access_token 跳过/JWT 解析失败容错
- 导入后流水:刷新成功→测试、刷新失败置错且不测试、非 grok 账号不触发

前端:`npx vue-tsc --noEmit && npx vitest run`;批测按钮与结果弹窗、导入弹窗格式识别。

## 影响面与发布

- 改动集中在 `handler/admin` 与 `service` 层,不碰网关转发链路
- 行为变更点(合并上游时需保留,登记 CUSTOM_CHANGES.md):
  1. grok 测试/刷新失败置错
  2. 导入后流水取代配额探测
  3. 导入端点接受 xai 格式
  4. 新端点 batch-test
- 发布按 `vX.Y.Z-custom.N` 流程(RELEASE_PROCESS.md)
