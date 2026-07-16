# 自有仓库版本更新链路 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理台「立即更新/回滚」跟踪 `Kline-x/sub2api-investigation` 的 Releases；推送 `vX.Y.Z-custom.N` 标签自动构建二进制资产 + GHCR 镜像；为 v0.1.155 基线补发可回滚版本。

**Architecture:** 三处小改动（更新源常量、4 段版本比较、goreleaser simple 配置补资产）复用上游全部现成机制（release.yml 工作流、二进制自替换更新、回滚面板）。v0.1.155 基线通过维护分支 cherry-pick 同样三处改动后打标签。

**Tech Stack:** Go 1.26.5（本地 D:/go1.25 + `GOTOOLCHAIN=auto`）、goreleaser v2、GitHub Actions、GHCR。

## Global Constraints

- 单元测试必须带 `-tags unit`，用 `GOTOOLCHAIN=auto /d/go1.25/bin/go`（Git Bash 路径写法）
- 版本标签必须是合法 semver：`vX.Y.Z-custom.N`（如 `v0.1.156-custom.1`）
- 标签必须是**附注标签**（`git tag -a`），release.yml 读取标签消息作为发布说明
- 发布顺序硬约束：先 `v0.1.155-custom.1` 后 `v0.1.156-custom.1`（GitHub "latest" 给最近发布的 Release）
- commit message 用中文
- 工作分支：`custom/v0.1.156`；维护分支：`custom/v0.1.155-maint`（从 `2be10837` 拉出）

---

### Task 1: 版本解析/比较扩展为 4 段

**Files:**
- Modify: `backend/internal/service/update_service.go:640-668`（`compareVersions` 与 `parseVersion`）
- Test: `backend/internal/service/update_service_test.go`（文件已存在，追加测试）

**Interfaces:**
- Produces: `parseVersion(v string) [4]int`（第 4 段来自 `-custom.N` 后缀，无后缀或非 custom 后缀为 0）；`compareVersions(current, latest string) int` 语义不变（-1/0/1），比较 4 段
- 依赖方：`CheckUpdate`、`fetchRollbackCandidates`（现有调用点，签名不变无需改动）

- [ ] **Step 1: 写失败测试**

在 `backend/internal/service/update_service_test.go` 末尾追加（文件已有 `//go:build unit` 头和 `require` 导入）：

```go
func TestParseVersionCustomSuffix(t *testing.T) {
	require.Equal(t, [4]int{0, 1, 156, 0}, parseVersion("v0.1.156"))
	require.Equal(t, [4]int{0, 1, 156, 1}, parseVersion("v0.1.156-custom.1"))
	require.Equal(t, [4]int{0, 1, 156, 12}, parseVersion("0.1.156-custom.12"))
	// 旧部署版本格式:custom 后缀为日期序号,也按第 4 段解析
	require.Equal(t, [4]int{0, 1, 155, 20260714}, parseVersion("0.1.155-custom.20260714"))
	// 非 custom 的 semver 预发布后缀不计入第 4 段
	require.Equal(t, [4]int{0, 1, 156, 0}, parseVersion("v0.1.156-rc.1"))
	// 非法输入兜底为 0
	require.Equal(t, [4]int{0, 0, 0, 0}, parseVersion("garbage"))
}

func TestCompareVersionsCustomScheme(t *testing.T) {
	// 上游对上游(原有行为不变)
	require.Equal(t, -1, compareVersions("0.1.155", "0.1.156"))
	require.Equal(t, 0, compareVersions("0.1.156", "v0.1.156"))
	require.Equal(t, 1, compareVersions("0.1.157", "0.1.156"))
	// custom 对 custom
	require.Equal(t, -1, compareVersions("0.1.156-custom.1", "0.1.156-custom.2"))
	require.Equal(t, 0, compareVersions("0.1.156-custom.1", "0.1.156-custom.1"))
	// 上游基线 < 同基线 custom
	require.Equal(t, -1, compareVersions("0.1.156", "0.1.156-custom.1"))
	// 跨基线:上游新版 > 旧基线任意 custom
	require.Equal(t, -1, compareVersions("0.1.156-custom.9", "0.1.157"))
	// 现部署旧格式 < 新方案首版
	require.Equal(t, -1, compareVersions("0.1.155-custom.20260714", "0.1.156-custom.1"))
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto /d/go1.25/bin/go test -tags unit -count=1 -run 'TestParseVersionCustomSuffix|TestCompareVersionsCustomScheme' ./internal/service/
```
预期：编译错误（`[4]int` 与现有 `[3]int` 返回类型不符）——这就是失败信号。

- [ ] **Step 3: 实现**

将 `update_service.go` 中现有的 `compareVersions` 和 `parseVersion`（约 640-668 行）整体替换为：

```go
// compareVersions compares two versions of the form X.Y.Z[-custom.N].
// The custom suffix forms a 4th segment so self-hosted builds sort after
// their upstream baseline but before the next upstream release.
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 4; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [4]int {
	v = strings.TrimPrefix(v, "v")
	base, suffix, _ := strings.Cut(v, "-")
	result := [4]int{0, 0, 0, 0}
	parts := strings.Split(base, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	if rest, ok := strings.CutPrefix(suffix, "custom."); ok {
		if parsed, err := strconv.Atoi(rest); err == nil {
			result[3] = parsed
		}
	}
	return result
}
```

（`strings`、`strconv` 已在该文件导入，无需改 import。）

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto /d/go1.25/bin/go test -tags unit -count=1 -run 'TestParseVersionCustomSuffix|TestCompareVersionsCustomScheme' ./internal/service/
```
预期：`ok`

- [ ] **Step 5: 跑整个 update_service 相关测试防回归**

```bash
cd /e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto /d/go1.25/bin/go test -tags unit -count=1 -run 'TestUpdate|TestRollback|TestParseVersion|TestCompareVersions' ./internal/service/
```
预期：`ok`

- [ ] **Step 6: 提交**

```bash
cd /e/code/AI/codex/sub2api-investigation && git add backend/internal/service/update_service.go backend/internal/service/update_service_test.go && git commit -m "feat: 版本比较支持 -custom.N 四段排序"
```

---

### Task 2: 更新源指向自有仓库

**Files:**
- Modify: `backend/internal/service/update_service.go:33`

**Interfaces:**
- Produces: 常量 `githubRepo = "Kline-x/sub2api-investigation"`（`fetchRollbackCandidates`、`fetchLatestRelease` 两处使用，无签名变化）

- [ ] **Step 1: 修改常量**

`update_service.go` 第 33 行：

```go
	githubRepo     = "Kline-x/sub2api-investigation"
```

- [ ] **Step 2: 编译 + 跑 update 相关测试**

```bash
cd /e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto /d/go1.25/bin/go build ./internal/service/ && GOTOOLCHAIN=auto /d/go1.25/bin/go test -tags unit -count=1 -run 'TestUpdate|TestRollback' ./internal/service/
```
预期：`ok`（现有测试全部走 stub client，不依赖该常量的具体值）

- [ ] **Step 3: 提交**

```bash
cd /e/code/AI/codex/sub2api-investigation && git add backend/internal/service/update_service.go && git commit -m "feat: 更新检查与回滚改为跟踪 Kline-x/sub2api-investigation"
```

---

### Task 3: goreleaser simple 配置补二进制资产

**Files:**
- Modify: `.goreleaser.simple.yaml`

**Interfaces:**
- Produces: Release 附带 `sub2api_{版本}_linux_amd64.tar.gz`（内含名为 `sub2api` 的二进制）+ `checksums.txt`；Release 不标 prerelease。与后端 `getArchiveName()`（返回 `linux_amd64`，按 `strings.Contains` 匹配资产名）、`extractBinary`（找归档内名为 `sub2api` 的文件）、`verifyChecksum` 兼容

- [ ] **Step 1: 修改配置**

对 `.goreleaser.simple.yaml` 做 4 处修改：

1)（29-30 行）`archives: []` 替换为：

```yaml
# linux/amd64 归档:面板在线更新/回滚需要下载此资产
archives:
  - id: default
    format: tar.gz
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
    files:
      - LICENSE*
      - README*
      - deploy/*
```

2)（32-34 行）`checksum: disable: true` 替换为：

```yaml
checksum:
  name_template: 'checksums.txt'
  algorithm: sha256
```

3) `release.prerelease: auto` 改为 `prerelease: false`（`-custom.N` 后缀会被 auto 判成 prerelease，导致 `releases/latest` 查不到）

4) 删除 `release.skip_upload: true` 这一行（恢复资产上传）

- [ ] **Step 2: YAML 语法自检**

```bash
cd /e/code/AI/codex/sub2api-investigation && python -c "import yaml,sys; yaml.safe_load(open('.goreleaser.simple.yaml', encoding='utf-8')); print('yaml ok')"
```
预期：`yaml ok`（无 python-yaml 则跳过，最终由 Actions 验证）

- [ ] **Step 3: 提交**

```bash
cd /e/code/AI/codex/sub2api-investigation && git add .goreleaser.simple.yaml && git commit -m "ci: simple 发布补回 linux/amd64 归档与 checksums,关闭 prerelease 自动标记"
```

---

### Task 4: 推送主线并清理旧标签

**Files:** 无代码改动（git 操作）

- [ ] **Step 1: 推送 custom/v0.1.156**

```bash
cd /e/code/AI/codex/sub2api-investigation && git push origin custom/v0.1.156
```

- [ ] **Step 2: 删除旧标签（本地 + 远程，用户已在设计中确认）**

```bash
cd /e/code/AI/codex/sub2api-investigation && git tag -d custom-v0.1.156 && git push origin :refs/tags/custom-v0.1.156
```
预期：远程输出 `[deleted]`

---

### Task 5: v0.1.155 维护分支 cherry-pick

**Files:** 无新代码（cherry-pick Task 1-3 的三个提交到维护分支）

**Interfaces:**
- Consumes: Task 1-3 在 `custom/v0.1.156` 上的三个提交哈希（执行时用 `git log --oneline -5` 查取）

- [ ] **Step 1: 建维护分支**

```bash
cd /e/code/AI/codex/sub2api-investigation && git checkout -b custom/v0.1.155-maint 2be10837
```

- [ ] **Step 2: cherry-pick 三个提交（按 Task1→2→3 顺序，哈希以实际为准）**

```bash
cd /e/code/AI/codex/sub2api-investigation && git log custom/v0.1.156 --oneline -8   # 找到三个提交哈希
git cherry-pick <Task1哈希> <Task2哈希> <Task3哈希>
```
预期：干净应用（v0.1.155 基线的 `update_service.go` 版本比较函数与 `.goreleaser.simple.yaml` 相关区域与 v0.1.156 相同，已验证）。若有冲突按 Task 1-3 的最终形态解决。

- [ ] **Step 3: 在维护分支跑测试**

```bash
cd /e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto /d/go1.25/bin/go test -tags unit -count=1 -run 'TestParseVersion|TestCompareVersions|TestUpdate|TestRollback' ./internal/service/
```
预期：`ok`

- [ ] **Step 4: 推送维护分支**

```bash
cd /e/code/AI/codex/sub2api-investigation && git push -u origin custom/v0.1.155-maint
```

- [ ] **Step 5: 切回主线分支**

```bash
cd /e/code/AI/codex/sub2api-investigation && git checkout custom/v0.1.156
```

---

### Task 6: 【用户操作门】仓库设置

**此任务全部由用户在 GitHub 网页完成，完成前不得执行 Task 7：**

- [ ] 仓库 Settings → General → Danger Zone → Change visibility → **Public**
- [ ] 仓库 Settings → Secrets and variables → Actions → Variables → 新建 `SIMPLE_RELEASE` = `true`

验证（Claude 执行）：

```bash
curl -s -o /dev/null -w "%{http_code}" https://api.github.com/repos/Kline-x/sub2api-investigation
```
预期：`200`（公开生效）

---

### Task 7: 发布 v0.1.155-custom.1（先发旧基线）

**Files:** 无代码改动（标签 + 验证）

- [ ] **Step 1: 在维护分支打附注标签并推送**

```bash
cd /e/code/AI/codex/sub2api-investigation && git tag -a v0.1.155-custom.1 -m "v0.1.155 定制基线

基于上游 v0.1.155 的全部本地定制(grok 429 系列修复、content block not found 修复、批量改到期、筛选账号 ID API 等),更新源已指向自有仓库。" custom/v0.1.155-maint && git push origin v0.1.155-custom.1
```

- [ ] **Step 2: 等待并验证 Actions 构建**

```bash
curl -s "https://api.github.com/repos/Kline-x/sub2api-investigation/actions/runs?per_page=1" | python -c "import json,sys; r=json.load(sys.stdin)['workflow_runs'][0]; print(r['name'], r['status'], r['conclusion'])"
```
预期（构建约 10-20 分钟，轮询）：`Release completed success`

- [ ] **Step 3: 验证 Release 资产与镜像**

```bash
curl -s https://api.github.com/repos/Kline-x/sub2api-investigation/releases/latest | python -c "import json,sys; r=json.load(sys.stdin); print(r['tag_name'], r['prerelease'], [a['name'] for a in r['assets']])"
```
预期：`v0.1.155-custom.1 False ['checksums.txt', 'sub2api_0.1.155-custom.1_linux_amd64.tar.gz']`

- [ ] **Step 4: 【用户操作】GHCR 包转公开**

首次发布后包默认私有：GitHub 个人页 → Packages → `sub2api` → Package settings → Change visibility → Public。

验证（Claude 执行，匿名拉取 manifest）：

```bash
TOKEN=$(curl -s "https://ghcr.io/token?scope=repository:kline-x/sub2api:pull" | python -c "import json,sys; print(json.load(sys.stdin)['token'])") && curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json" "https://ghcr.io/v2/kline-x/sub2api/manifests/0.1.155-custom.1"
```
预期：`200`

---

### Task 8: 发布 v0.1.156-custom.1（后发新版，成为 latest）

**Files:** 无代码改动（标签 + 验证）

- [ ] **Step 1: 在主线分支打附注标签并推送**

```bash
cd /e/code/AI/codex/sub2api-investigation && git tag -a v0.1.156-custom.1 -m "合并上游 v0.1.156 的定制版

上游 v0.1.156 全部修复 + 本地定制(免费额度耗尽封 24h、裸 429 指数封禁、批量改到期、筛选账号 ID API 等)。更新源指向自有仓库,面板支持在线更新与回滚。" custom/v0.1.156 && git push origin v0.1.156-custom.1
```

- [ ] **Step 2: 等待并验证 Actions 构建**

同 Task 7 Step 2 命令。预期：`Release completed success`

- [ ] **Step 3: 验证 latest 指向新版**

```bash
curl -s https://api.github.com/repos/Kline-x/sub2api-investigation/releases/latest | python -c "import json,sys; r=json.load(sys.stdin); print(r['tag_name'], r['prerelease'], [a['name'] for a in r['assets']])"
```
预期：`v0.1.156-custom.1 False ['checksums.txt', 'sub2api_0.1.156-custom.1_linux_amd64.tar.gz']`

- [ ] **Step 4: 验证镜像存在**

```bash
TOKEN=$(curl -s "https://ghcr.io/token?scope=repository:kline-x/sub2api:pull" | python -c "import json,sys; print(json.load(sys.stdin)['token'])") && curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" -H "Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json" "https://ghcr.io/v2/kline-x/sub2api/manifests/0.1.156-custom.1"
```
预期：`200`

- [ ] **Step 5: 【用户操作】首次手动部署**

服务器上 `docker pull ghcr.io/kline-x/sub2api:0.1.156-custom.1` 重建容器（现部署二进制的更新源仍指向官方，不可用面板更新做这次升级）。部署后在面板验证：版本徽章显示 `v0.1.156-custom.1` → 「已是最新」→ 底部出现「版本回滚」→ 列表含 `0.1.155-custom.1`。

---

## Self-Review 记录

- 规格覆盖：更新源(Task 2)、版本比较(Task 1)、CI 资产(Task 3)、v0.1.155 补发(Task 5+7)、发布顺序(Task 7→8)、用户操作项(Task 6、7.4、8.5)、旧标签清理(Task 4)——全覆盖
- 占位符：无
- 类型一致：`parseVersion [4]int` 在 Task 1 测试与实现一致；资产名 `sub2api_{版本}_linux_amd64.tar.gz` 与 Task 3 name_template、Task 7/8 验证预期一致
