# 进度文档:批量测试/置错/CPA导入/导入后流水

更新时间:2026-07-17

## 一句话现状

10 个任务**全部完成**；分支 `develop/xuyang/batch-test-cpa-import`（HEAD `f0728641`），已推送到 `origin`。

## 三份关键文档

| 文档 | 作用 |
|---|---|
| `docs/superpowers/plans/2026-07-16-grok-batch-test-cpa-import.md` | 实施计划 |
| `docs/superpowers/specs/2026-07-16-grok-batch-test-cpa-import-design.md` | 设计文档 |
| `.superpowers/sdd/progress.md` | SDD ledger |

## 任务进度表

| Task | 内容 | 状态 | 提交 |
|---|---|---|---|
| 1 | grok 测试上游 4xx(非429)→SetError | ✅ 完成 | `2f5133d4` |
| 2 | OAuthUpstreamStatusError + IsGrokRefreshPermanentFailure | ✅ 完成 | `96254a0f` |
| 3 | 手动刷新入口置错 | ✅ 完成 | `92dee164` |
| 4 | POST /accounts/batch-test + models_by_platform | ✅ 完成 | `7b40532f` |
| 5 | CPA 后端转换 | ✅ 完成 | `e928dc31` |
| 6 | 导入后刷新→测试流水 | ✅ 完成 | `fa0b3b1f` |
| 7-9 | 前端 xai 导入 + 批测选模型 UI | ✅ 完成 | `8e763bb4` |
| 10 | CUSTOM_CHANGES 登记 | ✅ 完成 | `f0728641` |

基点: `93f904cd` → HEAD: `f0728641`

## 产品规则确认

- 批量测试按平台选模型:请求 `{ account_ids, models_by_platform? }`
- 空 model → 各平台硬编码默认(grok→`grok-4.5`)
- 前端确认弹窗每个出现平台一行模型下拉
- 导入后流水固定空 model,不弹窗

## 验证摘要

- 后端相关单测(Task 3/4/5/6): PASS(`go test -tags unit`)
- 前端: vitest 159 files / 1095 tests PASS; vue-tsc 无报错
- 未合入本计划的 WIP(勿混提交): `openai_gateway_grok.go`、headers 测试、`scripts/cap_to_sub2api_accounts.py` 等

## 下一步

1. 全分支终审(`git diff 93f904cd..HEAD`)
2. 合回 main 后按 RELEASE_PROCESS.md 发版