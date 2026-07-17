# SDD 进度 — 2026-07-16 批量测试/置错/CPA导入 计划(docs/superpowers/plans/2026-07-16-grok-batch-test-cpa-import.md)
分支: develop/xuyang/batch-test-cpa-import  基点: 93f904cd

## 本计划任务
- Task 1: complete (commits 93f904cd..2f5133d4, review clean)
- Task 2: complete (commits 2f5133d4..96254a0f, review clean)
- Task 3: complete (commits 96254a0f..92dee164, review clean)
- Task 4: complete (commits 92dee164..7b40532f, review clean)
- Task 5: complete (commits 7b40532f..e928dc31, review clean)
- Task 6: complete (commits e928dc31..fa0b3b1f, review clean)
- Task 7-9: complete (commits fa0b3b1f..8e763bb4, review clean)
- Task 10: complete (commits 8e763bb4..f0728641, CUSTOM_CHANGES 登记)
- Final: HEAD f0728641, 分支 develop/xuyang/batch-test-cpa-import 已推 origin
---
## 历史(2026-07 发版计划,已完结)
Task 1: complete (commits 9a7f9662..e015a533, review clean)
  Minor(留给终审): parseVersion 对多'-'后缀/负数 N 静默兜底为0,理论边界,与上游风格一致
Task 2: complete (commits e015a533..f47c5e50, review clean)
Task 3: complete (commits f47c5e50..dda560a6, review clean)
  Minor(留给终审): format: tar.gz 在 goreleaser v2.6+ 有弃用警告,与上游配置一致保留
Task 4: complete (push dda560a6, 旧标签 custom-v0.1.156 已删本地+远程)
Task 5: complete (custom/v0.1.155-maint = 2be10837 + cherry-pick 41c8e4bf/5181da22/194c0254, 测试 ok, 已推送)
Task 7: complete (v0.1.155-custom.1 发布成功,资产齐全,prerelease=false)
Task 8: complete (v0.1.156-custom.1 发布成功,latest 正确,ghcr 三标签均 200)
Task final-review: READY (修复 406f5fb6/6f636205 + install.sh 86ce7591)
Final: READY(终审复核闭环) + install.sh 修复(86ce7591)
发布: v0.1.155-custom.2 / v0.1.156-custom.2 全部成功,latest(release+docker)均指向 0.1.156-custom.2
遗留: CI golangci-lint 失败(已挂 spawn_task 后续处理);custom.1 两个 release 保留(镜像/资产完整,仅前端常量指旧仓库)
