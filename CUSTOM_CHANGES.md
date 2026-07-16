# 定制改动记录

本仓库相对上游 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的全部定制改动，按版本记录。**每次发布新版本时在此追加对应条目。**

## v0.1.156-custom.2（2026-07-16，当前线上目标版本）

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
- `26734ffd` fix：grok 429 持续切换账号而非切一次就返回 429（后被上游 followup 预算机制取代）
- `d05ef1bb` fix：Claude Code 调工具报 content block not found（Responses→Anthropic 流转换孤儿 delta；上游 v0.1.156 同修）
- `2be10837` fix：grok 裸 429 连击指数递增封禁，消除 2 分钟兜底抖动（`grokBare429State`，独有，与上游自适应冷却叠加取较晚 reset）

## 持续维护的定制功能清单（合并上游时须保留）

| 功能 | 位置 |
|---|---|
| grok 免费额度耗尽封 24h | `openai_gateway_grok.go`（`grokFreeUsageWindow` 等常量）、`grok_quota_service.go` |
| grok 裸 429 指数递增封禁 | `openai_gateway_grok.go`（`grokBare429State`）、`openai_gateway_service.go`（`grokBare429States`） |
| 账号批量改到期时间 | `admin_service.go` / `BulkEditAccountModal.vue` |
| 筛选账号 ID API + 全选 | `routes/admin.go`（`/accounts/ids`）、`AccountsView.vue` |
| 4 段版本比较 + 自有更新源 | `update_service.go` |
| 发布流水线定制 | `.goreleaser.simple.yaml` |
| 自有仓库引用 | `VersionBadge.vue` 常量、`deploy/install.sh` `GITHUB_REPO` |

## 已知问题

- CI 的 golangci-lint 作业失败（测试全过，待修）
- 上游偶发：`TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 全量跑 service 包时可能超时失败，纯上游同样存在，可忽略
