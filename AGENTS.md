# 项目 Agent 指南

## 仓库性质（先读这个）

这是 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的**定制分支**，不是原仓库：

- origin = `Kline-x/sub2api-investigation`（本仓库，发布/部署来源）
- upstream = `Wei-Shaw/sub2api`（只用来合并新版本，**永远不要向它推送**）
- 主线分支：`main`（合并上游、发标签都在这里）；`custom/vX.Y.Z-maint` 是旧基线回滚维护分支；`custom/v0.1.155`、`custom/v0.1.156` 是历史分支，不再演进
- Go module 路径仍是 `github.com/Wei-Shaw/sub2api`，这是刻意保留的，**不要改**

## 两份必读文档

| 文档 | 什么时候读 |
|---|---|
| [RELEASE_PROCESS.md](RELEASE_PROCESS.md) | 发版本、撤版本、合并上游、部署之前 |
| [CUSTOM_CHANGES.md](CUSTOM_CHANGES.md) | 改代码之前（尤其「持续维护的定制功能清单」表格——合并上游或重构时**必须保留**表中功能）；每次发布后在其中追加条目 |

## 硬性规则

1. **版本号**：`vX.Y.Z-custom.N`（X.Y.Z=上游基线）。发布 = 推附注标签，CI 自动出 Release + GHCR 镜像。只发验证过的版本；发现问题删标签撤版，禁止移动已发布标签
2. **多版本连发顺序**：旧版先发、新版后发（GitHub latest 给最近发布的）
3. **面板在线更新机制**：更新源是 `backend/internal/service/update_service.go` 的 `githubRepo` 常量（指向本仓库）；版本比较是 4 段（识别 `-custom.N`）。改这两处前先读 RELEASE_PROCESS.md
4. **`.goreleaser.simple.yaml`**：`prerelease: false`、archives、checksums 是定制关键配置，合并上游冲突时保住

## 构建与测试

- 后端测试**必须带 `-tags unit`**（grok 相关测试文件有 `//go:build unit` 标签，不带就静默跳过）：
  `cd backend && go test -tags unit ./...`
- go.mod 要求 go 1.26.5；本机工具链较旧时加 `GOTOOLCHAIN=auto`
- 前端：`cd frontend && npx vue-tsc --noEmit && npx vitest run`
- 提交信息用中文
- 已知的偶发失败：`TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 全量跑 service 包时可能超时，上游同样存在，可忽略；CI 的 golangci-lint 作业当前是红的（待修），测试作业必须是绿的

## 生成文件（禁止手改）

- `backend/cmd/server/wire_gen.go`（wire 生成）
- `backend/internal/web/dist/`（前端构建产物，经 go:embed 嵌入）
