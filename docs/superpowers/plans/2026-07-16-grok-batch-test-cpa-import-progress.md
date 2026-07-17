# 进度文档:批量测试/置错/CPA导入/导入后流水(供任意 agent 接续开发)

更新时间:2026-07-16(会话因额度中断,Task 2 审查未完成时记录)

## 一句话现状

10 个任务的实施计划已就绪,**Task 1 已完成并通过审查,Task 2 已实现待审查,Task 3-10 未开始**;所有工作在分支 `develop/xuyang/batch-test-cpa-import` 上。

## 三份关键文档(按顺序读)

| 文档 | 作用 |
|---|---|
| `docs/superpowers/plans/2026-07-16-grok-batch-test-cpa-import.md` | **实施计划(核心)**——10 个 Task,每个含完整的失败测试代码、实现代码、验证命令、提交步骤,照抄即可 |
| `docs/superpowers/specs/2026-07-16-grok-batch-test-cpa-import-design.md` | 设计文档——需求背景、已确认决策、错误处理规则表 |
| `.superpowers/sdd/progress.md` | SDD 执行 ledger——每完成一个 Task 追加一行 |

注意:`docs/*` 被 .gitignore 忽略,改动这些文档要 `git add -f`。

## 任务进度表

| Task | 内容 | 状态 | 提交 |
|---|---|---|---|
| 1 | grok 测试上游 4xx(非429)→SetError(service 层) | ✅ 完成+审查通过 | `2f5133d4` |
| 2 | `xai.OAuthUpstreamStatusError` + repository 挂 cause + `service.IsGrokRefreshPermanentFailure` | ⚠️ 已实现,**审查被中断未完成** | `96254a0f` |
| 3 | 手动刷新入口置错(refreshSingleAccount grok 分支 + GrokOAuthHandler.RefreshAccountToken) | ⬜ 未开始 | |
| 4 | 批量测试端点 `POST /accounts/batch-test`(handler 加 `accountTester` 字段) | ⬜ 未开始 | |
| 5 | CPA 后端转换(`DataPayload.XaiAccounts` + `convertXaiAccount`) | ⬜ 未开始 | |
| 6 | 导入后「刷新→测试」流水 `grok_import_pipeline.go`,取代 probe 调用 | ⬜ 未开始 | |
| 7 | 前端 `utils/xaiImport.ts` extractXaiAccounts + vitest | ⬜ 未开始 | |
| 8 | ImportDataModal 识别 xai + types + i18n + `grok_pipeline_scheduled` 提示 | ⬜ 未开始 | |
| 9 | 前端批量测试按钮(accounts.ts batchTest + BulkActionsBar + AccountsView + i18n)+ 验证 grok-4.5 默认 | ⬜ 未开始 | |
| 10 | CUSTOM_CHANGES.md 登记 + 全量回归 | ⬜ 未开始 | |
| 终审 | 全分支 code review + 处理 findings | ⬜ 未开始 | |

**任务间依赖**:Task 3 和 Task 6 消费 Task 2 产出的 `service.IsGrokRefreshPermanentFailure`;Task 6 消费 Task 4 产出的 handler 字段 `accountTester backgroundAccountTester`;Task 8 消费 Task 7 的 `extractXaiAccounts` 和 Task 5/6 的后端字段;Task 9 消费 Task 4 的端点。**按 3→4→5→6→7→8→9→10 顺序执行即可**。

## 接续者的第一件事:补 Task 2 审查

Task 2 实现已提交(`96254a0f`,5 文件:`backend/internal/pkg/xai/errors.go`、`backend/internal/service/grok_refresh_failure.go` + 测试、`backend/internal/repository/grok_oauth_client.go` + 测试),实现者自报测试全绿,但独立审查在派发时因会话额度中断。接续时先对照计划 Task 2 的验收点审查该提交(审查包已生成:`.superpowers/sdd/review-2f5133d4..96254a0f.diff`;简报 `.superpowers/sdd/task-2-brief.md`;实现报告 `.superpowers/sdd/task-2-report.md`),通过后在 ledger 记一行,再开始 Task 3。

## 执行纪律(每个 Task 相同)

1. **TDD**:先抄计划里的失败测试 → 跑一次确认失败/编译错误 → 抄实现 → 跑到全绿 → 按计划的 `git add <明确文件列表> && git commit` 提交(提交信息计划里给了,用中文)
2. **测试命令**(Git Bash / Windows 主机):
   - 后端:`cd /e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto /d/go1.25/bin/go test -tags unit ./internal/... -count=1`(**必须 `-tags unit`**,否则 grok 测试文件被静默跳过;聚焦单测用 `-run '<pattern>'` 缩小范围)
   - 前端:`cd frontend && npx vue-tsc --noEmit && npx vitest run`
3. **每完成一个 Task 在 `.superpowers/sdd/progress.md` 追加一行**:`Task N: complete (commits <base>..<head>, review clean)`

## 环境与坑

- **工作区有不属于本计划的 WIP,严禁混入提交**(提交后用 `git show --stat HEAD` 自查):
  - `backend/internal/service/openai_gateway_grok.go`(已修改:grok CLI 头环境变量覆盖功能)
  - `backend/internal/service/openai_gateway_grok_headers_test.go`(未跟踪,同属该 WIP)
  - `scripts/cap_to_sub2api_accounts.py`(已修改)
  - `.agnes/`、`artifacts/`、`sub2api-v0.1.156-custom.2-20260716.tar`、`sub2api-grok-accounts.json`、`.superpowers/`(均未跟踪)
- Go 工具链:Windows 侧 `/d/go1.25/bin/go`(1.25.6)+ `GOTOOLCHAIN=auto`(go.mod 要 1.26.5,会自动拉);WSL 备选见计划 Global Constraints
- 已知偶发失败:`TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 全量跑 service 包可能超时,与本计划无关,忽略
- 禁改生成文件:`backend/cmd/server/wire_gen.go`、`backend/internal/web/dist/`
- 失败界定(全计划统一,勿走样):上游 4xx 且非 429 → SetError;429 → 限流态;5xx/网络错误 → 不改状态

## 留给终审的 Minor 记录

- Task 1 审查:`account_test_service.go:797` SetError 错误被静默吞掉(`_ =`),沿用同文件既有风格(见 682 行),可在终审时决定是否全文件统一清理

## 完成后的收尾(计划 Task 10 之后)

1. 全分支终审(diff 基点 `93f904cd`,即 main 分叉点)
2. 分支合回 main(本仓库主线是 main,发布 = 在 main 打 `vX.Y.Z-custom.N` 附注标签,详见 RELEASE_PROCESS.md)
3. CUSTOM_CHANGES.md 已在 Task 10 登记;发版前再读一遍 RELEASE_PROCESS.md
