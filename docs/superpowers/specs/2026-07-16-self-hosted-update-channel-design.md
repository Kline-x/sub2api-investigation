# 设计：自有仓库版本更新链路（面板一键更新/回滚 + 标签自动构建）

日期：2026-07-16
状态：已与用户确认

## 背景与目标

当前管理台「立即更新」跟踪上游 `Wei-Shaw/sub2api` 的 GitHub Releases，会把定制版覆盖成官方版。目标：

1. 更新检查与「立即更新」改为跟踪自有仓库 `Kline-x/sub2api-investigation`
2. 每次推送版本标签，CI 自动构建 GitHub Release（二进制资产）+ GHCR 镜像
3. 面板可一键更新到新版本，也可回滚到指定旧版本（复用上游现成回滚功能）
4. 为合并前的 v0.1.155 定制基线补发标签和镜像，作为可回滚目标

前提决策（用户已确认）：仓库转 **Public**；版本号采用 **semver + `-custom.N` 后缀**方案。

## 现有机制（调查结论）

- 「立即更新」= 容器内**二进制自替换**：从 GitHub Releases 下载 `sub2api_<版本>_linux_amd64.tar.gz` + `checksums.txt`，校验后原子替换自身二进制并重启。前端经 `go:embed` 嵌入二进制，换二进制即完整升级
- 更新源是 `backend/internal/service/update_service.go` 中的常量 `githubRepo = "Wei-Shaw/sub2api"`
- 版本比较 `parseVersion`/`compareVersions` 只解析三段 `X.Y.Z`，非数字段按 0 处理（现部署的 `0.1.155-custom.20260714` 被解析为 `0.1.0`）
- 检查更新走 `GET /repos/{repo}/releases/latest`（不含 prerelease/draft）
- 回滚功能上游已完整实现：`ListRollbackVersions`（最多列 3 个比当前旧的版本）+ `RollbackToVersion`，前端 VersionBadge 有「版本回滚」面板入口
- 上游 CI 已参数化：`release.yml` 由 `v*` 标签触发；goreleaser 把 GHCR 镜像推到 `ghcr.io/<仓库所有者小写>/sub2api`；DockerHub 无密钥自动跳过；`SIMPLE_RELEASE=true` 仓库变量启用 `.goreleaser.simple.yaml`（仅 linux/amd64 GHCR 镜像，但 `archives: []` 跳过了二进制资产）

## 版本号方案

标签格式：`vX.Y.Z-custom.N`（合法 semver，goreleaser 可接受，匹配 `v*` 触发模式）

- `X.Y.Z` = 所基于的上游版本；`N` = 该基线上第几次定制发布
- 排序：`0.1.156 < 0.1.156-custom.1 < 0.1.156-custom.2 < 0.1.157`
- 上游无后缀版本第四段视为 0；旧格式 `0.1.155-custom.20260714` 按新逻辑精确解析为 `[0,1,155,20260714]`，仍小于 `0.1.156-custom.1`，必然提示有更新

## 代码改动（3 处）

1. **更新源**：`update_service.go` 常量 `githubRepo` → `Kline-x/sub2api-investigation`
2. **版本比较**：`parseVersion` 返回 4 段（识别 `-custom.N` 后缀），`compareVersions` 比较 4 段；补单元测试（上游对上游、custom 对 custom、跨基线、旧格式兜底、回滚候选过滤）
3. **CI 归档**：`.goreleaser.simple.yaml` 补回 linux/amd64 tar.gz 归档 + `checksums.txt`（资产名模板含 `linux_amd64`，与更新服务的资产匹配逻辑天然兼容；归档内二进制名为 `sub2api`，与 `extractBinary` 预期一致）

不新建 workflow：上游 `release.yml` 本就被 `v*` 标签触发，新建会双触发冲突。

## 发布与回滚流程

```
推送标签 v0.1.156-custom.1
  → Actions（SIMPLE_RELEASE 模式）
  → Release 资产（tar.gz + checksums.txt）+ ghcr.io/kline-x/sub2api:{版本}/:latest
  → 面板检查更新 → 立即更新（下载→校验→原子替换→重启）
  → 回滚：版本回滚面板列出旧版本 → 选择 → 同一套下载替换逻辑
```

## v0.1.155 基线补发布

直接在老提交打标签不可行：该提交的 CI 配置不出二进制资产（回滚无物可下）；且该版二进制更新源仍指向官方仓库（回滚过去后点更新会覆盖成官方版）。方案：

1. 从合并前提交 `2be10837` 拉维护分支 `custom/v0.1.155-maint`
2. cherry-pick 本次三处改动
3. 打 `v0.1.155-custom.1` 触发构建
4. **发布顺序：先发 v0.1.155-custom.1，后发 v0.1.156-custom.1**（GitHub "latest" 默认给最近发布的 Release）

## 用户手动操作项

- 仓库 Settings → 转 Public（Claude 无权限操作访问控制）
- 首次发布后 GHCR 包 `ghcr.io/kline-x/sub2api` 默认私有，需在 Package settings 改一次 Public
- 仓库 Settings → Actions → Variables 添加 `SIMPLE_RELEASE=true`
- 删除旧标签 `custom-v0.1.156`（不符合新方案；本地与远程）

## 边界与已知限制

- **首次升级必须手动部署镜像**：现部署的二进制是旧代码（更新源指向官方），其「立即更新」不可用；部署 `0.1.156-custom.1` 镜像后，后续版本才能面板一键更新
- **在线更新持久性**（上游机制固有）：二进制替换在容器可写层，容器重启保留，**重建容器回退到镜像版本**。大版本建议拉新镜像重建，小修补用面板更新
- 回滚列表上限 3 个版本（上游默认 `maxRollbackVersions`），保持不动
- 未来合并上游时 `.goreleaser.simple.yaml` 可能小冲突，可接受

## 测试与验证

- 单测：版本解析/比较全组合；回滚候选过滤（custom 版本混排）
- 真实链路（需仓库转公开后）：推标签 → Actions 产物齐全 → `curl releases/latest` 可见 → 拉镜像部署 → 面板显示新版可更新/可回滚（部署侧由用户验证）
