# 发布规范（定制版）

本仓库是 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的定制分支。本文档约定定制版的版本号、发布、撤版与回滚规范。

## 版本号方案

标签格式：`vX.Y.Z-custom.N`

- `X.Y.Z`：所基于的上游版本（如 `0.1.156`）
- `N`：该上游基线上的第几次定制发布，从 1 开始递增
- 排序规则（后端 `compareVersions` 按 4 段比较）：
  `0.1.156 < 0.1.156-custom.1 < 0.1.156-custom.2 < 0.1.157`
- 标签必须是合法 semver（goreleaser 校验），必须用**附注标签**（`git tag -a`），标签消息会成为 Release 说明正文

## 发布流程

1. **验证**：发布前必须先通过——后端 `go test -tags unit ./...`、前端 `vue-tsc --noEmit` + vitest；重要改动建议先在本地 docker 环境实测（见下文「本地验证环境」）
2. **打标签并推送**（发布 = 推标签，无其他步骤）：

   ```bash
   git tag -a v0.1.156-custom.3 -m "一句话标题

   详细改动说明（会显示在 Release 页面）"
   git push origin v0.1.156-custom.3
   ```

3. **自动构建**：GitHub Actions（`release.yml`，`SIMPLE_RELEASE=true` 走 `.goreleaser.simple.yaml`）产出：
   - GitHub Release：`sub2api_<版本>_linux_amd64.tar.gz` + `checksums.txt`（面板在线更新/回滚依赖这两个资产）
   - GHCR 镜像：`ghcr.io/kline-x/sub2api:<版本>` 和 `:latest`
   - 构建约 10-20 分钟
4. **发布后验证**：

   ```bash
   # latest 必须指向新版本、prerelease 必须为 false、资产必须齐全
   curl -s https://api.github.com/repos/Kline-x/sub2api-investigation/releases/latest
   ```

## 硬性约束

- **多版本连发时旧版先发、新版后发**：GitHub 的 "latest" 标记给最近发布的 Release，顺序反了面板会把旧版当最新
- **只发可用版本**：未经验证的版本不打标签
- **禁止 prerelease**：`releases/latest` 端点和回滚候选都会跳过 prerelease（`.goreleaser.simple.yaml` 已固定 `prerelease: false`，勿改回 `auto`——`-custom.N` 后缀会被 auto 误判为预发布）
- **禁止移动/复用已发布的标签**：发现问题走撤版 + 新版本号

## 撤版（下架有问题的版本）

```bash
git push origin :refs/tags/<tag>   # 删远程标签
git tag -d <tag>                   # 删本地标签
```

删除标签后对应 Release 自动转为**草稿**，公开 API 与面板立即不可见。可选的手工清理（登录 GitHub）：Releases 页删除草稿、[GHCR 包版本页](https://github.com/users/Kline-x/packages/container/sub2api/versions)删除对应镜像。

## 面板在线更新与回滚

- 更新源：后端常量 `githubRepo`（`backend/internal/service/update_service.go`），指向本仓库 Releases
- **在线更新** = 容器内二进制自替换 + 重启：面板版本徽章 → 「立即更新」→「立即重启」。重启本质是进程退出，**部署容器必须带 `restart: unless-stopped`**（或 systemd `Restart=always`），否则需要手动拉起
- **版本回滚**入口只在「已是最新版本」状态下显示，列出最多 3 个比当前旧的版本
- **持久性边界**：二进制替换发生在容器可写层——容器重启保留，**重建容器（recreate）会回到镜像版本**。大版本建议拉新镜像重建容器，小修补用面板在线更新
- 首次从旧版（更新源还指向上游官方的版本）升级，必须手动拉镜像部署，不能用面板更新

## 上游合并

1. `git fetch upstream "+refs/tags/*:refs/tags/*"`（upstream = Wei-Shaw/sub2api）
2. 在主线分支 `git merge v<新版本>`，解决冲突（注意 `.goreleaser.simple.yaml`、`update_service.go` 是定制热点）
3. 全量测试通过后按本规范发 `v<新版本>-custom.1`
4. 旧基线如需保留回滚能力，从合并前提交拉 `custom/vX.Y.Z-maint` 维护分支并 cherry-pick 链路必需改动（参照 `custom/v0.1.155-maint`）

## 本地验证环境

用与线上一致的 GHCR 镜像 + docker compose（postgres + redis）起本地栈实测，参考 `deploy/docker-compose.yml`。要点：容器带 `restart: unless-stopped`；`JWT_SECRET` ≥32 字节；接有数据的旧库前先 `pg_dump` 备份（新版启动会跑不可逆的 schema 迁移）。
