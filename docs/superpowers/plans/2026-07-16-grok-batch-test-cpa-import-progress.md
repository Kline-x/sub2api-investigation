# 进度文档:批量测试/置错/CPA导入/导入后流水

更新时间:2026-07-17

## 一句话现状

10 个任务**全部完成**；分支 `develop/xuyang/batch-test-cpa-import`。

## 任务进度表

| Task | 内容 | 状态 | 提交 |
|---|---|---|---|
| 1 | grok 测试上游 4xx(非429)→SetError | ✅ | `2f5133d4` |
| 2 | OAuthUpstreamStatusError + IsGrokRefreshPermanentFailure | ✅ | `96254a0f` |
| 3 | 手动刷新入口置错 | ✅ | `92dee164` |
| 4 | POST /accounts/batch-test + models_by_platform | ✅ | `7b40532f` |
| 5 | CPA 后端转换 | ✅ | `e928dc31` |
| 6 | 导入后刷新→测试流水 | ✅ | `fa0b3b1f` |
| 7-9 | 前端 xai 导入 + 批测选模型 UI | ✅ | `8e763bb4` |
| 10 | CUSTOM_CHANGES 登记 | ✅ | 本提交 |

## 产品规则确认(2026-07-17)

- 批量测试按平台选模型:请求 `{ account_ids, models_by_platform? }`
- 空 model → 各平台硬编码默认(grok→`grok-4.5`)
- 前端确认弹窗每个出现平台一行模型下拉
- 导入后流水固定空 model,不弹窗

## 收尾

1. 全分支终审(diff 基点 `93f904cd`)
2. 分支合回 main 后按 RELEASE_PROCESS.md 发版