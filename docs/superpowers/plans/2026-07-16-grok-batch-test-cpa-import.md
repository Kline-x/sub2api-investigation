# 批量测试 / 失败置错 / CPA 导入 / 导入后刷新+测试 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 sub2api 定制分支新增账号批量测试端点、grok 测试/刷新失败自动置错、面板导入支持 CPA(xai-*.json)格式、导入 grok 账号后自动「刷新令牌→grok-4.5 测试」后台流水。

**Architecture:** 后端改动集中在 `handler/admin` 与 `service` 层:置错逻辑放在 service 层测试路径与刷新错误分类函数中,使单测/批测/导入后测试/定时测试统一生效;批测端点复用现成的 `RunTestBackground`;CPA 转换放在导入 handler(前端只做格式检测透传);导入后流水复刻现有 probe 调度器模式并取代它。前端改动:批量操作栏加测试按钮、导入弹窗识别 xai 格式、新增 API 封装与 i18n。

**Tech Stack:** Go 1.26(gin + errgroup + testify + gjson,单测带 `//go:build unit`)、Vue3 + TS(vue-tsc + vitest)。

**设计文档:** `docs/superpowers/specs/2026-07-16-grok-batch-test-cpa-import-design.md`

## Global Constraints

- 仓库是 Wei-Shaw/sub2api 的定制分支;Go module 路径 `github.com/Wei-Shaw/sub2api` 不改
- 后端测试命令(所有 Task 通用):`cd backend && go test -tags unit ./internal/...`;grok 相关测试文件**必须**以 `//go:build unit` 开头
- 本机 Windows,Go 工具链在 WSL:`wsl -u gaore bash -lc 'PATH="/usr/local/go/bin:$HOME/go/bin:$PATH" && cd /mnt/e/code/AI/codex/sub2api-investigation/backend && GOTOOLCHAIN=auto go test -tags unit ./internal/...'`(若 Windows 侧有 D:/go1.25 亦可直接用)
- 前端检查:`cd frontend && npx vue-tsc --noEmit && npx vitest run`
- 提交信息用中文
- 失败界定(测试连接):上游错误响应除 429 外(400/401/403/502 等 4xx/5xx)以及取 token 失败 → `SetTempUnschedulable`(临时不可调度,默认 10 分钟);429 → 现有限流态;网络错误(无响应) → 不改状态。永久 error 由管理员手动/批量 set-error。刷新令牌仍按 4xx 非429 置错、5xx/网络不改状态
- 测试模型:
  - 空 modelID → 各平台硬编码默认(grok=`grokDefaultResponsesModel`/`grok-4.5`,其他=`DefaultTestModel`),不另硬编码新常量
  - **批测按平台选模型**:请求可选 `models_by_platform` map(platform→model_id);每个账号取 `map[acc.Platform]`,缺省则空 model
  - 前端批测确认弹窗:按勾选账号出现的平台各渲染一行模型下拉(同/跨平台均可);每行用该平台某账号的 `getAvailableModels` 填充;grok 默认 `grok-4.5`
  - 导入后流水仍固定空 model(grok-4.5)
- 禁改生成文件:`backend/cmd/server/wire_gen.go`、`backend/internal/web/dist/`
- 已知偶发失败:`TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 全量跑 service 包可能超时,与本改动无关,可忽略
- `docs/*` 被 .gitignore 忽略,提交本目录文档需 `git add -f`

---

### Task 1: grok 测试失败置错(service 层)

**Files:**
- Modify: `backend/internal/service/account_test_service.go:792-795`(`testGrokAccountConnection` 非 200 分支)
- Test: `backend/internal/service/account_test_service_grok_test.go`(追加)

**Interfaces:**
- Consumes: `s.accountRepo.SetError(ctx, accountID, errMsg)`(已有,`AccountRepository` 接口)
- Produces: 行为变更——grok 测试上游 4xx(非429)后账号被置 error。单测弹窗、批测(Task 4)、导入后流水(Task 6)、定时测试全部经此路径。

- [ ] **Step 1: 写失败测试**

在 `account_test_service_grok_test.go` 末尾追加(文件已有 `//go:build unit` 头,勿重复):

```go
type grokAccountTestSetErrorRepo struct {
	*mockAccountRepoForGemini
	setErrorCalls int
	lastErrorMsg  string
}

func (r *grokAccountTestSetErrorRepo) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

// SetRateLimited 计数,供 429 用例断言限流路径仍生效
type grokAccountTestSetErrorAndRateLimitRepo struct {
	*grokAccountTestSetErrorRepo
	rateLimitedCalls int
}

func (r *grokAccountTestSetErrorAndRateLimitRepo) SetRateLimited(_ context.Context, _ int64, _ time.Time) error {
	r.rateLimitedCalls++
	return nil
}

func newGrokSetErrorTestAccount(id int64) *Account {
	return &Account{
		ID: id, Name: "grok-set-error", Platform: PlatformGrok,
		Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
}

func TestAccountTestService_Grok403SetsAccountError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokSetErrorTestAccount(31)
	repo := &grokAccountTestSetErrorRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader(`{"error":"blocked"}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/31/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Contains(t, repo.lastErrorMsg, "403")
}

func TestAccountTestService_Grok429DoesNotSetError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokSetErrorTestAccount(32)
	base := &grokAccountTestSetErrorRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	repo := &grokAccountTestSetErrorAndRateLimitRepo{grokAccountTestSetErrorRepo: base}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"45"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/32/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Zero(t, base.setErrorCalls)
	require.Equal(t, 1, repo.rateLimitedCalls)
}

func TestAccountTestService_Grok500DoesNotSetError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokSetErrorTestAccount(33)
	repo := &grokAccountTestSetErrorRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(`{"error":"upstream down"}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/33/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Zero(t, repo.setErrorCalls)
}
```

注意:若 `mockAccountRepoForGemini` 的 `SetError`(gemini_multiplatform_test.go:101)与嵌入结构冲突,外层方法已覆写,无需改 mock 本体。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -tags unit ./internal/service/ -run 'TestAccountTestService_Grok403SetsAccountError|TestAccountTestService_Grok429DoesNotSetError|TestAccountTestService_Grok500DoesNotSetError' -v`
Expected: `Grok403SetsAccountError` FAIL(setErrorCalls=0);另两个应 PASS(它们锁定现状防回归)。

- [ ] **Step 3: 实现**

把 `account_test_service.go` 中 `testGrokAccountConnection` 的非 200 分支(当前 792-795 行):

```go
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return s.sendErrorAndEnd(c, fmt.Sprintf("Grok Responses API returned %d: %s", resp.StatusCode, string(body)))
	}
```

改为:

```go
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("Grok Responses API returned %d: %s", resp.StatusCode, string(body))
		// 定制:4xx(非429)代表凭据/权限问题,统一置错停止调度;429 走上方限流持久化,5xx/网络错误不改状态
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests && s.accountRepo != nil {
			_ = s.accountRepo.SetError(ctx, account.ID, errMsg)
		}
		return s.sendErrorAndEnd(c, errMsg)
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -tags unit ./internal/service/ -run 'TestAccountTestService_Grok' -v`
Expected: 全部 PASS(含既有的 4 个 grok 测试用例)。

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/account_test_service.go backend/internal/service/account_test_service_grok_test.go
git commit -m "feat: grok 测试上游4xx(非429)自动置错停调度"
```

---

### Task 2: 刷新失败结构化状态码 + 永久失败分类函数

**Files:**
- Create: `backend/internal/pkg/xai/errors.go`
- Modify: `backend/internal/repository/grok_oauth_client.go:152-166`(`grokOAuthStatusError`)
- Create: `backend/internal/service/grok_refresh_failure.go`
- Test: `backend/internal/service/grok_refresh_failure_test.go`、`backend/internal/repository/grok_oauth_client_test.go`(追加)

**Interfaces:**
- Consumes: `infraerrors.ApplicationError.WithCause(error)`(pkg/errors/errors.go:55,Unwrap 支持 errors.As)、service 包内已有 `isNonRetryableRefreshError(err)`(token_refresh_service.go:1405)
- Produces:
  - `xai.OAuthUpstreamStatusError{Status int}`——OAuth token 端点上游状态码的结构化载体
  - `service.IsGrokRefreshPermanentFailure(err error) bool`——Task 3/6 用来判断是否置错

- [ ] **Step 1: 写失败测试(service 分类函数)**

新建 `backend/internal/service/grok_refresh_failure_test.go`:

```go
//go:build unit

package service

import (
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestIsGrokRefreshPermanentFailure(t *testing.T) {
	t.Parallel()

	upstreamErr := func(status int) error {
		return infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_TOKEN_REFRESH_FAILED",
			"token refresh failed: status %d", status).
			WithCause(&xai.OAuthUpstreamStatusError{Status: status})
	}

	require.False(t, IsGrokRefreshPermanentFailure(nil))
	// 已知不可重试错误清单(与后台刷新 worker 相同)
	require.True(t, IsGrokRefreshPermanentFailure(errors.New("token refresh failed: invalid_grant")))
	require.True(t, IsGrokRefreshPermanentFailure(errors.New("GROK_OAUTH_ENTITLEMENT_DENIED: no active grok subscription")))
	// 上游 4xx(非429)→ 永久失败
	require.True(t, IsGrokRefreshPermanentFailure(upstreamErr(400)))
	require.True(t, IsGrokRefreshPermanentFailure(upstreamErr(401)))
	require.True(t, IsGrokRefreshPermanentFailure(upstreamErr(403)))
	// 429 / 5xx / 无结构化状态码的网络错误 → 非永久
	require.False(t, IsGrokRefreshPermanentFailure(upstreamErr(429)))
	require.False(t, IsGrokRefreshPermanentFailure(upstreamErr(500)))
	require.False(t, IsGrokRefreshPermanentFailure(upstreamErr(502)))
	require.False(t, IsGrokRefreshPermanentFailure(errors.New("request failed: dial tcp: i/o timeout")))
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `go test -tags unit ./internal/service/ -run TestIsGrokRefreshPermanentFailure -v`
Expected: 编译错误——`OAuthUpstreamStatusError`、`IsGrokRefreshPermanentFailure` 未定义。

- [ ] **Step 3: 实现 pkg/xai 错误类型**

新建 `backend/internal/pkg/xai/errors.go`:

```go
package xai

import "fmt"

// OAuthUpstreamStatusError 记录 OAuth token 端点返回的上游 HTTP 状态码。
// 作为 ApplicationError 的 cause 挂载,供调用方用 errors.As 做结构化分类
// (定制:手动/导入刷新失败按上游状态码决定是否置错)。
type OAuthUpstreamStatusError struct {
	Status int
}

func (e *OAuthUpstreamStatusError) Error() string {
	return fmt.Sprintf("oauth upstream status %d", e.Status)
}
```

- [ ] **Step 4: 实现 service 分类函数**

新建 `backend/internal/service/grok_refresh_failure.go`:

```go
package service

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// IsGrokRefreshPermanentFailure 判断一次 grok 手动/导入刷新失败是否应把账号置为 error。
// 规则(定制):已知不可重试错误(invalid_grant 等,与后台刷新 worker 同一清单),
// 或 token 端点上游返回 4xx(429 除外)。网络错误/5xx 返回 false(账号本身未必坏)。
// 注意:后台 TokenRefreshService 的置错策略保持不变(它只认不可重试清单),本函数只服务
// 手动刷新入口与导入后流水。
func IsGrokRefreshPermanentFailure(err error) bool {
	if err == nil {
		return false
	}
	if isNonRetryableRefreshError(err) {
		return true
	}
	var upstream *xai.OAuthUpstreamStatusError
	if errors.As(err, &upstream) {
		return upstream.Status >= 400 && upstream.Status < 500 && upstream.Status != http.StatusTooManyRequests
	}
	return false
}
```

- [ ] **Step 5: repository 挂载 cause**

把 `backend/internal/repository/grok_oauth_client.go` 的 `grokOAuthStatusError` 末行:

```go
	return infraerrors.Newf(statusCode, errorCode, "%s: status %d, body: %s", message, upstreamStatus, body)
```

改为:

```go	
	appErr := infraerrors.Newf(statusCode, errorCode, "%s: status %d, body: %s", message, upstreamStatus, body)
	if upstreamStatus > 0 {
		// 挂结构化上游状态码,供 service.IsGrokRefreshPermanentFailure 分类(定制)
		return appErr.WithCause(&xai.OAuthUpstreamStatusError{Status: upstreamStatus})
	}
	return appErr
```

- [ ] **Step 6: 追加 repository 测试**

在 `backend/internal/repository/grok_oauth_client_test.go` 末尾追加(该文件已有 `//go:build unit` 头;import 需补 `"errors"`):

```go
func TestGrokOAuthClientStatusErrorCarriesUpstreamStatusCause(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()
	t.Setenv(xai.EnvTokenURL, server.URL)

	client := NewGrokOAuthClient()
	_, err := client.RefreshToken(context.Background(), "refresh-token", "", "client-id")
	require.Error(t, err)

	var upstream *xai.OAuthUpstreamStatusError
	require.True(t, errors.As(err, &upstream))
	require.Equal(t, http.StatusBadRequest, upstream.Status)
}
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test -tags unit ./internal/service/ -run TestIsGrokRefreshPermanentFailure -v && go test -tags unit ./internal/repository/ -run TestGrokOAuthClient -v`
Expected: 全部 PASS。

- [ ] **Step 8: 提交**

```bash
git add backend/internal/pkg/xai/errors.go backend/internal/service/grok_refresh_failure.go backend/internal/service/grok_refresh_failure_test.go backend/internal/repository/grok_oauth_client.go backend/internal/repository/grok_oauth_client_test.go
git commit -m "feat: grok OAuth 刷新错误携带上游状态码,新增永久失败分类函数"
```

---

### Task 3: 手动刷新入口置错(单个/批量/grok 专用端点)

**Files:**
- Modify: `backend/internal/handler/admin/account_handler.go:1276-1279`(`refreshSingleAccount` grok 分支)
- Modify: `backend/internal/handler/admin/grok_oauth_handler.go:147-151`(`RefreshAccountToken`)
- Test: `backend/internal/handler/admin/account_handler_grok_refresh_test.go`(追加)

**Interfaces:**
- Consumes: `service.IsGrokRefreshPermanentFailure(err)`(Task 2)、`h.adminService.SetAccountError(ctx, id, msg)`(AdminService 接口,stub 在 admin_service_stub_test.go:458)
- Produces: 行为变更——`POST /accounts/:id/refresh`、`POST /accounts/batch-refresh`(内部走同一 `refreshSingleAccount`)、`POST /grok/accounts/:id/refresh` 刷新遇永久失败时置错。

- [ ] **Step 1: 写失败测试**

在 `account_handler_grok_refresh_test.go` 追加(import 需补 `"net/http"`、`infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"`、`"github.com/Wei-Shaw/sub2api/internal/pkg/xai"`):

```go
type grokRefreshFailStub struct {
	err error
}

func (s *grokRefreshFailStub) RefreshAccountToken(_ context.Context, _ *service.Account) (*service.GrokTokenInfo, error) {
	return nil, s.err
}

func (s *grokRefreshFailStub) BuildAccountCredentials(_ *service.GrokTokenInfo) map[string]any {
	return nil
}

type grokRefreshSetErrorAdminService struct {
	*stubAdminService
	setErrorCalls int
	setErrorID    int64
	setErrorMsg   string
}

func (s *grokRefreshSetErrorAdminService) SetAccountError(_ context.Context, id int64, msg string) error {
	s.setErrorCalls++
	s.setErrorID = id
	s.setErrorMsg = msg
	return nil
}

func newGrokRefreshFailureHandler(adminSvc service.AdminService, refreshErr error) *AccountHandler {
	return NewAccountHandler(
		adminSvc, nil, nil, nil, nil,
		&grokRefreshFailStub{err: refreshErr},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
}

func TestRefreshSingleAccountGrokPermanentFailureSetsError(t *testing.T) {
	t.Parallel()

	adminSvc := &grokRefreshSetErrorAdminService{stubAdminService: newStubAdminService()}
	refreshErr := infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_TOKEN_REFRESH_FAILED",
		"token refresh failed: status 400, body: invalid_grant").
		WithCause(&xai.OAuthUpstreamStatusError{Status: 400})
	handler := newGrokRefreshFailureHandler(adminSvc, refreshErr)
	account := &service.Account{
		ID: 5301, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "a", "refresh_token": "r"},
	}

	_, _, err := handler.refreshSingleAccount(context.Background(), account)

	require.Error(t, err)
	require.Equal(t, 1, adminSvc.setErrorCalls)
	require.Equal(t, int64(5301), adminSvc.setErrorID)
	require.Contains(t, adminSvc.setErrorMsg, "status 400")
}

func TestRefreshSingleAccountGrokTransientFailureDoesNotSetError(t *testing.T) {
	t.Parallel()

	adminSvc := &grokRefreshSetErrorAdminService{stubAdminService: newStubAdminService()}
	refreshErr := infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_REQUEST_FAILED",
		"request failed: dial tcp: i/o timeout")
	handler := newGrokRefreshFailureHandler(adminSvc, refreshErr)
	account := &service.Account{
		ID: 5302, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "a", "refresh_token": "r"},
	}

	_, _, err := handler.refreshSingleAccount(context.Background(), account)

	require.Error(t, err)
	require.Zero(t, adminSvc.setErrorCalls)
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -tags unit ./internal/handler/admin/ -run TestRefreshSingleAccountGrok -v`
Expected: `PermanentFailureSetsError` FAIL(setErrorCalls=0);`TransientFailure` PASS;既有 `TestRefreshSingleAccountRoutesGrokThroughGrokOAuthService` PASS。

- [ ] **Step 3: 实现 refreshSingleAccount grok 分支**

把 `account_handler.go` grok 分支(当前 1276-1279 行):

```go
		tokenInfo, err := h.grokOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			return nil, "", fmt.Errorf("failed to refresh Grok credentials: %w", err)
		}
```

改为:

```go
		tokenInfo, err := h.grokOAuthService.RefreshAccountToken(ctx, account)
		if err != nil {
			// 定制:invalid_grant/上游4xx(非429)等不可恢复失败 → 置错停调度;网络/5xx 只返回错误
			if service.IsGrokRefreshPermanentFailure(err) {
				if setErr := h.adminService.SetAccountError(ctx, account.ID, "Grok token refresh failed: "+err.Error()); setErr != nil {
					log.Printf("[WARN] Failed to set error for grok account %d after refresh failure: %v", account.ID, setErr)
				}
			}
			return nil, "", fmt.Errorf("failed to refresh Grok credentials: %w", err)
		}
```

- [ ] **Step 4: 实现 GrokOAuthHandler.RefreshAccountToken**

把 `grok_oauth_handler.go` 的(当前 147-151 行):

```go
	tokenInfo, err := h.grokOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
```

改为:

```go
	tokenInfo, err := h.grokOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		// 定制:与 refreshSingleAccount 同规则,不可恢复失败置错停调度
		if service.IsGrokRefreshPermanentFailure(err) {
			if setErr := h.adminService.SetAccountError(c.Request.Context(), account.ID, "Grok token refresh failed: "+err.Error()); setErr != nil {
				slog.Warn("grok_refresh_set_error_failed", "account_id", account.ID, "error", setErr)
			}
		}
		response.ErrorFrom(c, err)
		return
	}
```

(`grok_oauth_handler.go` 已 import `log/slog` 与 `service`。)

- [ ] **Step 5: 跑测试确认通过**

Run: `go test -tags unit ./internal/handler/admin/ -run 'TestRefreshSingleAccount|GrokOAuth' -v`
Expected: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/handler/admin/account_handler.go backend/internal/handler/admin/grok_oauth_handler.go backend/internal/handler/admin/account_handler_grok_refresh_test.go
git commit -m "feat: grok 手动/批量刷新令牌永久失败自动置错"
```

---

### Task 4: 批量测试端点 POST /accounts/batch-test

**Files:**
- Modify: `backend/internal/handler/admin/account_handler.go`(BatchRefresh 之后追加 BatchTest;struct 加字段;NewAccountHandler 初始化)
- Modify: `backend/internal/server/routes/admin.go:335` 之后加路由
- Test: `backend/internal/handler/admin/account_handler_batch_test_endpoint_test.go`(新建)

**Interfaces:**
- Consumes: `service.AccountTestService.RunTestBackground(ctx, accountID, modelID) (*service.ScheduledTestResult, error)`(account_test_service.go:1914;`ScheduledTestResult{Status, ResponseText, ErrorMessage, LatencyMs, ...}`,Status 取值 `"success"`/`"failed"`)、`h.rateLimitService.RecoverAccountAfterSuccessfulTest(ctx, accountID)`
- Produces:
  - `AccountHandler.accountTester`(新字段,接口 `backgroundAccountTester`)——Task 6 复用
  - `POST /api/v1/admin/accounts/batch-test`,请求 `{"account_ids":[...],"models_by_platform":{"grok":"grok-4.5","openai":"gpt-5.4"}}`(`models_by_platform` 可选),响应 `{total, success, failed, results:[{id,name,status,error_message,latency_ms}]}`——Task 9 前端消费

- [ ] **Step 1: 写失败测试**

新建 `backend/internal/handler/admin/account_handler_batch_test_endpoint_test.go`:

```go
//go:build unit

package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type batchTestTesterStub struct {
	mu      sync.Mutex
	results map[int64]*service.ScheduledTestResult
	calls   []int64
}

func (s *batchTestTesterStub) RunTestBackground(_ context.Context, accountID int64, _ string) (*service.ScheduledTestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, accountID)
	if r, ok := s.results[accountID]; ok {
		return r, nil
	}
	return &service.ScheduledTestResult{Status: "success", LatencyMs: 5}, nil
}

type batchTestAdminService struct {
	*stubAdminService
	accounts map[int64]*service.Account
}

func (s *batchTestAdminService) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	out := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		if acc, ok := s.accounts[id]; ok {
			out = append(out, acc)
		}
	}
	return out, nil
}

func newBatchTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/batch-test", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, rec
}

func TestBatchTestMixedResults(t *testing.T) {
	adminSvc := &batchTestAdminService{
		stubAdminService: newStubAdminService(),
		accounts: map[int64]*service.Account{
			1: {ID: 1, Name: "ok", Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			2: {ID: 2, Name: "bad", Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
		},
	}
	tester := &batchTestTesterStub{results: map[int64]*service.ScheduledTestResult{
		2: {Status: "failed", ErrorMessage: "Grok Responses API returned 403: blocked"},
	}}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.accountTester = tester

	// id=3 不存在 → 计为 failed
	c, rec := newBatchTestContext(t, `{"account_ids":[1,2,3]}`)
	handler.BatchTest(c)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Equal(t, int64(3), gjson.Get(body, "data.total").Int())
	require.Equal(t, int64(1), gjson.Get(body, "data.success").Int())
	require.Equal(t, int64(2), gjson.Get(body, "data.failed").Int())
	require.Equal(t, int64(3), gjson.Get(body, "data.results.#").Int())

	sort.Slice(tester.calls, func(i, j int) bool { return tester.calls[i] < tester.calls[j] })
	require.Equal(t, []int64{1, 2}, tester.calls)
}

func TestBatchTestEmptyIDsRejected(t *testing.T) {
	handler := NewAccountHandler(newStubAdminService(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.accountTester = &batchTestTesterStub{}

	c, rec := newBatchTestContext(t, `{"account_ids":[]}`)
	handler.BatchTest(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `go test -tags unit ./internal/handler/admin/ -run TestBatchTest -v`
Expected: 编译错误——`accountTester` 字段、`BatchTest` 方法未定义。

- [ ] **Step 3: 实现**

3a. `account_handler.go` struct(49-65 行)加字段,放 `grokImportProber` 之后:

```go
	accountTester           backgroundAccountTester
```

3b. 在 struct 定义前(48 行附近)加接口定义:

```go
// backgroundAccountTester 是批量测试与导入后流水共用的非 SSE 程序化测试端口,
// 生产实现为 *service.AccountTestService(定制)。
type backgroundAccountTester interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*service.ScheduledTestResult, error)
}
```

3c. `NewAccountHandler` 构造完 handler 后、return 前加(注意 typed-nil:必须判空再赋值):

```go
	if accountTestService != nil {
		handler.accountTester = accountTestService
	}
```

(若构造函数直接 `return &AccountHandler{...}`,改成先赋变量 `handler := &AccountHandler{...}`,插入判空,再 `return handler`。)

3d. 在 `BatchRefresh` 方法后追加:

```go
// BatchTestResultItem 单个账号的批量测试结果
type BatchTestResultItem struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
	LatencyMs    int64  `json:"latency_ms"`
}

// BatchTest handles batch testing account connectivity
// POST /api/v1/admin/accounts/batch-test
// 定制:后端并发批测,复用 RunTestBackground;按平台 models_by_platform 选模型,缺省则各平台默认(grok-4.5 等);
// 测试失败置错在 service 层统一生效(见 testGrokAccountConnection)。
func (h *AccountHandler) BatchTest(c *gin.Context) {
	var req struct {
		AccountIDs        []int64           `json:"account_ids"`
		ModelsByPlatform  map[string]string `json:"models_by_platform"` // 可选;platform → model_id
		ModelID           string            `json:"model_id"`           // 可选兼容:无 map 时整批共用
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 {
		response.BadRequest(c, "account_ids is required")
		return
	}
	if h.accountTester == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account test service unavailable")
		return
	}

	ctx := c.Request.Context()
	accounts, err := h.adminService.GetAccountsByIDs(ctx, req.AccountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	foundIDs := make(map[int64]bool, len(accounts))
	for _, acc := range accounts {
		if acc != nil {
			foundIDs[acc.ID] = true
		}
	}

	var mu sync.Mutex
	var successCount, failedCount int
	results := make([]BatchTestResultItem, 0, len(req.AccountIDs))

	for _, id := range req.AccountIDs {
		if !foundIDs[id] {
			failedCount++
			results = append(results, BatchTestResultItem{ID: id, Status: "failed", ErrorMessage: "account not found"})
		}
	}

	const maxConcurrency = 10
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)

	// 注意：所有 goroutine 必须 return nil，避免 errgroup cancel 其他并发任务
	for _, account := range accounts {
		acc := account // 闭包捕获
		if acc == nil {
			continue
		}
		g.Go(func() error {
			item := BatchTestResultItem{ID: acc.ID, Name: acc.Name}
			modelID := req.ModelID
			if req.ModelsByPlatform != nil {
				if m, ok := req.ModelsByPlatform[acc.Platform]; ok {
					modelID = m
				} else {
					modelID = "" // 该平台未选 → 后端默认
				}
			}
			testResult, testErr := h.accountTester.RunTestBackground(gctx, acc.ID, modelID)
			switch {
			case testErr != nil:
				item.Status = "failed"
				item.ErrorMessage = testErr.Error()
			case testResult != nil:
				item.Status = testResult.Status
				item.ErrorMessage = testResult.ErrorMessage
				item.LatencyMs = testResult.LatencyMs
			default:
				item.Status = "failed"
				item.ErrorMessage = "empty test result"
			}
			if item.Status == "success" && h.rateLimitService != nil {
				if _, recoverErr := h.rateLimitService.RecoverAccountAfterSuccessfulTest(gctx, acc.ID); recoverErr != nil {
					log.Printf("[WARN] batch test recover account %d failed: %v", acc.ID, recoverErr)
				}
			}
			mu.Lock()
			if item.Status == "success" {
				successCount++
			} else {
				failedCount++
			}
			results = append(results, item)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{
		"total":   len(req.AccountIDs),
		"success": successCount,
		"failed":  failedCount,
		"results": results,
	})
}
```

3e. `backend/internal/server/routes/admin.go` 335 行 `batch-refresh` 之后加:

```go
			accounts.POST("/batch-test", h.Admin.Account.BatchTest)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -tags unit ./internal/handler/admin/ -run TestBatchTest -v && go build ./...`
Expected: 测试 PASS,全仓编译通过。

- [ ] **Step 5: 提交**

```bash
git add backend/internal/handler/admin/account_handler.go backend/internal/handler/admin/account_handler_batch_test_endpoint_test.go backend/internal/server/routes/admin.go
git commit -m "feat: 新增账号批量测试端点 POST /accounts/batch-test"
```

---

### Task 5: CPA(xai-*.json)导入——后端转换

**Files:**
- Modify: `backend/internal/handler/admin/account_data.go`(DataPayload 加字段;importData 接入转换)
- Create: `backend/internal/handler/admin/account_data_xai.go`
- Test: `backend/internal/handler/admin/account_data_xai_test.go`(新建)

**Interfaces:**
- Consumes: `xai.DecodeJWTClaims(token) map[string]any`、`xai.JWTClaimString(claims, key) string`(pkg/xai/sso_device.go:369,385)、`xai.DefaultCLIBaseURL`(pkg/xai/oauth.go:26)
- Produces:
  - `DataPayload.XaiAccounts []map[string]any`(json 字段 `xai_accounts`)——前端(Task 8)填充
  - `convertXaiAccount(raw map[string]any, index int) (DataAccount, error)`
  - `DataImportResult.GrokPipelineScheduled int`(json `grok_pipeline_scheduled`,Task 6 填值,本 Task 先加字段)

- [ ] **Step 1: 写失败测试**

新建 `backend/internal/handler/admin/account_data_xai_test.go`:

```go
//go:build unit

package admin

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 构造无签名校验的假 JWT(alg none 风格,仅 payload 段被解析)
func makeFakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	seg := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	return seg([]byte(`{"alg":"none"}`)) + "." + seg(payload) + ".sig"
}

func TestConvertXaiAccountFullFields(t *testing.T) {
	t.Parallel()

	accessToken := makeFakeJWT(t, map[string]any{
		"client_id": "cid-123",
		"team_id":   "team-9",
		"scope":     "openid api:access",
		"email":     "claim@example.com",
		"sub":       "sub-1",
	})
	raw := map[string]any{
		"access_token":  accessToken,
		"refresh_token": "rt-1",
		"id_token":      "idt-1",
		"token_type":    "Bearer",
		"email":         "field@example.com",
		"sub":           "sub-field",
		"expired":       "2026-08-01T00:00:00Z",
	}

	item, err := convertXaiAccount(raw, 0)
	require.NoError(t, err)

	require.Equal(t, "field@example.com", item.Name) // 字段 email 优先于 JWT claim
	require.Equal(t, service.PlatformGrok, item.Platform)
	require.Equal(t, service.AccountTypeOAuth, item.Type)
	require.Equal(t, 1, item.Concurrency)
	require.Equal(t, 0, item.Priority)
	require.Equal(t, accessToken, item.Credentials["access_token"])
	require.Equal(t, "rt-1", item.Credentials["refresh_token"])
	require.Equal(t, "idt-1", item.Credentials["id_token"])
	require.Equal(t, "Bearer", item.Credentials["token_type"])
	require.Equal(t, "cid-123", item.Credentials["client_id"])
	require.Equal(t, "team-9", item.Credentials["team_id"])
	require.Equal(t, "openid api:access", item.Credentials["scope"])
	require.Equal(t, "field@example.com", item.Credentials["email"])
	require.Equal(t, "sub-field", item.Credentials["sub"])
	require.Equal(t, "2026-08-01T00:00:00Z", item.Credentials["expires_at"])
	require.Equal(t, xai.DefaultCLIBaseURL, item.Credentials["base_url"])
}

func TestConvertXaiAccountEmailFallsBackToClaimThenIndex(t *testing.T) {
	t.Parallel()

	withClaim := map[string]any{
		"access_token": makeFakeJWT(t, map[string]any{"email": "claim@example.com"}),
	}
	item, err := convertXaiAccount(withClaim, 3)
	require.NoError(t, err)
	require.Equal(t, "claim@example.com", item.Name)

	// access_token 不是合法 JWT → claim 解析失败,名字用序号兜底
	noClaim := map[string]any{"access_token": "not-a-jwt"}
	item2, err := convertXaiAccount(noClaim, 3)
	require.NoError(t, err)
	require.Equal(t, "grok-import-4", item2.Name)
}

func TestConvertXaiAccountMissingAccessTokenFails(t *testing.T) {
	t.Parallel()

	_, err := convertXaiAccount(map[string]any{"refresh_token": "rt"}, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token")
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `go test -tags unit ./internal/handler/admin/ -run TestConvertXaiAccount -v`
Expected: 编译错误——`convertXaiAccount` 未定义。

- [ ] **Step 3: 实现转换**

新建 `backend/internal/handler/admin/account_data_xai.go`:

```go
package admin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// convertXaiAccount 把 GROK CPA 导出的 xai-*.json 单账号对象转成 DataAccount。
// 逻辑与 scripts/cap_to_sub2api_accounts.py 对齐(定制):
//   - client_id/team_id/scope 从 access_token 的 JWT 载荷解出(不验签)
//   - email 取字段,缺省用 JWT claim,再缺省用 grok-import-<序号>
//   - base_url 固定 CLI 代理端点(与 BuildAccountCredentials 一致)
func convertXaiAccount(raw map[string]any, index int) (DataAccount, error) {
	str := func(key string) string {
		v, _ := raw[key].(string)
		return strings.TrimSpace(v)
	}

	accessToken := str("access_token")
	if accessToken == "" {
		return DataAccount{}, errors.New("xai account missing access_token")
	}

	claims := xai.DecodeJWTClaims(accessToken)
	claim := func(key string) string {
		if claims == nil {
			return ""
		}
		return xai.JWTClaimString(claims, key)
	}

	email := str("email")
	if email == "" {
		email = claim("email")
	}
	if email == "" {
		email = fmt.Sprintf("grok-import-%d", index+1)
	}

	sub := str("sub")
	if sub == "" {
		sub = claim("sub")
	}
	tokenType := str("token_type")
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return DataAccount{
		Name:     email,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  accessToken,
			"refresh_token": str("refresh_token"),
			"id_token":      str("id_token"),
			"token_type":    tokenType,
			"client_id":     claim("client_id"),
			"team_id":       claim("team_id"),
			"scope":         claim("scope"),
			"email":         email,
			"sub":           sub,
			"expires_at":    str("expired"),
			"base_url":      xai.DefaultCLIBaseURL,
		},
		Concurrency: 1,
		Priority:    0,
	}, nil
}
```

- [ ] **Step 4: 接入 importData**

4a. `account_data.go` `DataPayload`(27-36 行)加字段:

```go
	// XaiAccounts 是 CPA(xai-*.json)导入的原始账号对象,由前端检测格式后透传,
	// 服务端在 importData 中转换成 DataAccount(定制)
	XaiAccounts []map[string]any `json:"xai_accounts,omitempty"`
```

4b. `DataImportResult`(80-87 行)加字段:

```go
	// GrokPipelineScheduled 本次导入进入「后台刷新+测试」流水的 grok 账号数(定制)
	GrokPipelineScheduled int `json:"grok_pipeline_scheduled,omitempty"`
```

4c. `importData` 中账号循环(`for i := range dataPayload.Accounts`)之前插入:

```go
	// CPA(xai-*.json)账号先转换为 DataAccount 再走统一导入(定制)
	for i := range dataPayload.XaiAccounts {
		item, convErr := convertXaiAccount(dataPayload.XaiAccounts[i], i)
		if convErr != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    fmt.Sprintf("xai-account-%d", i+1),
				Message: convErr.Error(),
			})
			continue
		}
		dataPayload.Accounts = append(dataPayload.Accounts, item)
	}
```

- [ ] **Step 5: 追加 importData 集成用例**

在 `account_data_xai_test.go` 追加(stubAdminService 的 `CreateAccount` 行为以 admin_service_stub_test.go 为准;若 stub 的 CreateAccount 返回的账号缺 Platform/Type,可在子类覆写——参考同包其他 `*_test.go` 对 stubAdminService 的扩展方式):

```go
type xaiImportAdminService struct {
	*stubAdminService
	created []*service.CreateAccountInput
}

func (s *xaiImportAdminService) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	s.created = append(s.created, input)
	return &service.Account{
		ID: int64(len(s.created)), Name: input.Name,
		Platform: input.Platform, Type: input.Type, Credentials: input.Credentials,
	}, nil
}

func (s *xaiImportAdminService) ListProxies(_ context.Context, _, _ int, _, _, _, _, _ string) ([]service.Proxy, int64, error) {
	return nil, 0, nil
}

func TestImportDataConvertsXaiAccounts(t *testing.T) {
	adminSvc := &xaiImportAdminService{stubAdminService: newStubAdminService()}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	req := DataImportRequest{Data: DataPayload{
		Type:     dataType,
		Version:  dataVersion,
		Proxies:  []DataProxy{},
		Accounts: []DataAccount{},
		XaiAccounts: []map[string]any{
			{"access_token": makeFakeJWT(t, map[string]any{"email": "a@x.ai", "client_id": "cid"}), "refresh_token": "rt-a"},
			{"refresh_token": "rt-only"}, // 缺 access_token → 计入失败
		},
	}}

	result, err := handler.importData(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 1, result.AccountCreated)
	require.Equal(t, 1, result.AccountFailed)
	require.Len(t, adminSvc.created, 1)
	require.Equal(t, "a@x.ai", adminSvc.created[0].Name)
	require.Equal(t, service.PlatformGrok, adminSvc.created[0].Platform)
}
```

(import 需补 `"context"`。若 `stubAdminService` 已实现 `ListProxies` 且签名不同,删除上面的覆写、以现有签名为准。)

- [ ] **Step 6: 跑测试确认通过**

Run: `go test -tags unit ./internal/handler/admin/ -run 'TestConvertXaiAccount|TestImportDataConvertsXai' -v`
Expected: 全部 PASS。

- [ ] **Step 7: 提交**

```bash
git add backend/internal/handler/admin/account_data.go backend/internal/handler/admin/account_data_xai.go backend/internal/handler/admin/account_data_xai_test.go
git commit -m "feat: 导入端点支持 CPA(xai-*.json)账号,服务端转换为 grok OAuth"
```

---

### Task 6: 导入后「刷新→测试」后台流水(取代配额探测)

**Files:**
- Create: `backend/internal/handler/admin/grok_import_pipeline.go`
- Modify: `backend/internal/handler/admin/account_data.go:463`(替换 `h.scheduleGrokImportProbe(created)`)
- Test: `backend/internal/handler/admin/grok_import_pipeline_test.go`(新建)

**Interfaces:**
- Consumes: `service.GrokOAuthTokenService`(h.grokOAuthService 字段)、`service.MergeCredentials`、`service.IsGrokRefreshPermanentFailure`(Task 2)、`backgroundAccountTester`(Task 4 的 h.accountTester)、`h.adminService`(GetAccount/UpdateAccount/SetAccountError)、`h.tokenCacheInvalidator`
- Produces: `h.scheduleGrokImportPipeline(account) bool`(返回是否已入队,importData 用来计数)、`runGrokImportPipeline(ctx, deps, accountID)`(可直接单测)

- [ ] **Step 1: 写失败测试**

新建 `backend/internal/handler/admin/grok_import_pipeline_test.go`:

```go
//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type pipelineGrokOAuthStub struct {
	tokenInfo *service.GrokTokenInfo
	err       error
	calls     int
}

func (s *pipelineGrokOAuthStub) RefreshAccountToken(_ context.Context, _ *service.Account) (*service.GrokTokenInfo, error) {
	s.calls++
	return s.tokenInfo, s.err
}

func (s *pipelineGrokOAuthStub) BuildAccountCredentials(info *service.GrokTokenInfo) map[string]any {
	return map[string]any{"access_token": info.AccessToken, "refresh_token": info.RefreshToken}
}

type pipelineAdminService struct {
	*stubAdminService
	account       *service.Account
	updatedCreds  map[string]any
	setErrorCalls int
	setErrorMsg   string
}

func (s *pipelineAdminService) GetAccount(_ context.Context, _ int64) (*service.Account, error) {
	return s.account, nil
}

func (s *pipelineAdminService) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updatedCreds = input.Credentials
	return &service.Account{ID: id, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth, Credentials: input.Credentials}, nil
}

func (s *pipelineAdminService) SetAccountError(_ context.Context, _ int64, msg string) error {
	s.setErrorCalls++
	s.setErrorMsg = msg
	return nil
}

type pipelineTesterStub struct {
	calls  int
	result *service.ScheduledTestResult
}

func (s *pipelineTesterStub) RunTestBackground(_ context.Context, _ int64, modelID string) (*service.ScheduledTestResult, error) {
	s.calls++
	if modelID != "" {
		return nil, errors.New("model must be empty to use grok-4.5 default")
	}
	if s.result != nil {
		return s.result, nil
	}
	return &service.ScheduledTestResult{Status: "success"}, nil
}

func newPipelineAccount() *service.Account {
	return &service.Account{
		ID: 71, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "old", "refresh_token": "rt", "base_url": "https://keep.invalid/v1"},
	}
}

func TestGrokImportPipelineRefreshSuccessThenTest(t *testing.T) {
	adminSvc := &pipelineAdminService{stubAdminService: newStubAdminService(), account: newPipelineAccount()}
	oauth := &pipelineGrokOAuthStub{tokenInfo: &service.GrokTokenInfo{AccessToken: "new-at", RefreshToken: "new-rt"}}
	tester := &pipelineTesterStub{}

	runGrokImportPipeline(context.Background(), grokImportPipelineDeps{
		adminService: adminSvc, grokOAuth: oauth, tester: tester,
	}, 71)

	require.Equal(t, 1, oauth.calls)
	require.Equal(t, "new-at", adminSvc.updatedCreds["access_token"])
	require.Equal(t, "https://keep.invalid/v1", adminSvc.updatedCreds["base_url"]) // base_url 保留
	require.Equal(t, 1, tester.calls)
	require.Zero(t, adminSvc.setErrorCalls)
}

func TestGrokImportPipelinePermanentRefreshFailureSetsErrorAndSkipsTest(t *testing.T) {
	adminSvc := &pipelineAdminService{stubAdminService: newStubAdminService(), account: newPipelineAccount()}
	oauth := &pipelineGrokOAuthStub{err: infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_TOKEN_REFRESH_FAILED",
		"token refresh failed: status 400").WithCause(&xai.OAuthUpstreamStatusError{Status: 400})}
	tester := &pipelineTesterStub{}

	runGrokImportPipeline(context.Background(), grokImportPipelineDeps{
		adminService: adminSvc, grokOAuth: oauth, tester: tester,
	}, 71)

	require.Equal(t, 1, adminSvc.setErrorCalls)
	require.Contains(t, adminSvc.setErrorMsg, "status 400")
	require.Zero(t, tester.calls) // 刷新失败不再测试
}

func TestGrokImportPipelineTransientRefreshFailureNoError(t *testing.T) {
	adminSvc := &pipelineAdminService{stubAdminService: newStubAdminService(), account: newPipelineAccount()}
	oauth := &pipelineGrokOAuthStub{err: errors.New("request failed: dial tcp: i/o timeout")}
	tester := &pipelineTesterStub{}

	runGrokImportPipeline(context.Background(), grokImportPipelineDeps{
		adminService: adminSvc, grokOAuth: oauth, tester: tester,
	}, 71)

	require.Zero(t, adminSvc.setErrorCalls)
	require.Zero(t, tester.calls)
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `go test -tags unit ./internal/handler/admin/ -run TestGrokImportPipeline -v`
Expected: 编译错误——`grokImportPipelineDeps`、`runGrokImportPipeline` 未定义。

- [ ] **Step 3: 实现流水**

新建 `backend/internal/handler/admin/grok_import_pipeline.go`:

```go
package admin

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 定制:导入 grok OAuth 账号后的后台流水——刷新令牌 → grok-4.5 测试。
// 取代原 scheduleGrokImportProbe 配额探测(测试成功路径本身会持久化配额快照)。
const (
	grokImportPipelineConcurrency = 3
	grokImportPipelineTimeout     = 90 * time.Second
)

type grokImportPipelineDeps struct {
	adminService          service.AdminService
	grokOAuth             service.GrokOAuthTokenService
	tester                backgroundAccountTester
	tokenCacheInvalidator service.TokenCacheInvalidator
}

type grokImportPipelineTask struct {
	deps      grokImportPipelineDeps
	accountID int64
}

type grokImportPipelineScheduler struct {
	mu          sync.Mutex
	queue       []grokImportPipelineTask
	concurrency int
	workers     int
	timeout     time.Duration
}

var defaultGrokImportPipelineScheduler = &grokImportPipelineScheduler{
	concurrency: grokImportPipelineConcurrency,
	timeout:     grokImportPipelineTimeout,
}

func (s *grokImportPipelineScheduler) schedule(deps grokImportPipelineDeps, account *service.Account) bool {
	if s == nil || account == nil || account.ID <= 0 {
		return false
	}
	if account.Platform != service.PlatformGrok || account.Type != service.AccountTypeOAuth {
		return false
	}
	if deps.adminService == nil || deps.grokOAuth == nil {
		return false
	}

	s.mu.Lock()
	s.queue = append(s.queue, grokImportPipelineTask{deps: deps, accountID: account.ID})
	if s.workers < s.concurrency {
		s.workers++
		go s.worker()
	}
	s.mu.Unlock()
	return true
}

func (s *grokImportPipelineScheduler) worker() {
	for {
		task, ok := s.nextTask()
		if !ok {
			return
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("grok_import_pipeline_panic", "account_id", task.accountID, "recovery_type", panicType(recovered))
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
			defer cancel()
			runGrokImportPipeline(ctx, task.deps, task.accountID)
		}()
	}
}

func (s *grokImportPipelineScheduler) nextTask() (grokImportPipelineTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		s.workers--
		return grokImportPipelineTask{}, false
	}
	task := s.queue[0]
	s.queue[0] = grokImportPipelineTask{}
	s.queue = s.queue[1:]
	if len(s.queue) == 0 {
		s.queue = nil
	}
	return task, true
}

// runGrokImportPipeline 执行单账号流水:刷新令牌 → 成功则空 model 测试(grok 默认 grok-4.5)。
// 刷新永久失败(invalid_grant/上游4xx非429)→ SetAccountError;测试失败的置错在
// service 层 testGrokAccountConnection 内统一生效。
func runGrokImportPipeline(ctx context.Context, deps grokImportPipelineDeps, accountID int64) {
	account, err := deps.adminService.GetAccount(ctx, accountID)
	if err != nil || account == nil {
		slog.Warn("grok_import_pipeline_load_failed", "account_id", accountID, "error", err)
		return
	}

	tokenInfo, err := deps.grokOAuth.RefreshAccountToken(ctx, account)
	if err != nil {
		if service.IsGrokRefreshPermanentFailure(err) {
			if setErr := deps.adminService.SetAccountError(ctx, accountID, "Grok import refresh failed: "+err.Error()); setErr != nil {
				slog.Warn("grok_import_pipeline_set_error_failed", "account_id", accountID, "error", setErr)
			}
		}
		slog.Warn("grok_import_pipeline_refresh_failed", "account_id", accountID, "error", err)
		return
	}

	newCredentials := service.MergeCredentials(account.Credentials, deps.grokOAuth.BuildAccountCredentials(tokenInfo))
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		newCredentials["base_url"] = baseURL
	}
	updated, err := deps.adminService.UpdateAccount(ctx, accountID, &service.UpdateAccountInput{Credentials: newCredentials})
	if err != nil {
		slog.Warn("grok_import_pipeline_update_failed", "account_id", accountID, "error", err)
		return
	}
	if deps.tokenCacheInvalidator != nil {
		if invalidateErr := deps.tokenCacheInvalidator.InvalidateToken(ctx, updated); invalidateErr != nil {
			slog.Warn("grok_import_pipeline_invalidate_failed", "account_id", accountID, "error", invalidateErr)
		}
	}

	if deps.tester == nil {
		return
	}
	result, testErr := deps.tester.RunTestBackground(ctx, accountID, "")
	if testErr != nil {
		slog.Warn("grok_import_pipeline_test_failed", "account_id", accountID, "error", testErr)
		return
	}
	if result != nil {
		slog.Info("grok_import_pipeline_done", "account_id", accountID, "status", result.Status, "latency_ms", result.LatencyMs)
	}
}

func (h *AccountHandler) scheduleGrokImportPipeline(account *service.Account) bool {
	if h == nil {
		return false
	}
	return defaultGrokImportPipelineScheduler.schedule(grokImportPipelineDeps{
		adminService:          h.adminService,
		grokOAuth:             h.grokOAuthService,
		tester:                h.accountTester,
		tokenCacheInvalidator: h.tokenCacheInvalidator,
	}, account)
}
```

- [ ] **Step 4: importData 换用流水**

把 `account_data.go` importData 中的:

```go
		h.scheduleGrokImportProbe(created)
		result.AccountCreated++
```

改为:

```go
		// 定制:grok OAuth 账号导入后进入「刷新→测试」后台流水,取代原配额探测
		if h.scheduleGrokImportPipeline(created) {
			result.GrokPipelineScheduled++
		}
		result.AccountCreated++
```

`grok_import_probe.go` 保留不删(`AccountHandler.scheduleGrokImportProbe` 若因不再被调用而报 unused,不会——方法不报 unused;GrokOAuthHandler 侧调用不动)。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test -tags unit ./internal/handler/admin/ -run 'TestGrokImportPipeline|TestImportData' -v`
Expected: 全部 PASS。注意 `TestImportDataConvertsXai`(Task 5)在 handler 无 grokOAuthService 时 `scheduleGrokImportPipeline` 返回 false,不影响断言。

- [ ] **Step 6: 全量后端回归**

Run: `go test -tags unit ./internal/... 2>&1 | tail -30`
Expected: 除已知偶发 `TestContentModerationRuntimeSnapshot...` 外全绿。

- [ ] **Step 7: 提交**

```bash
git add backend/internal/handler/admin/grok_import_pipeline.go backend/internal/handler/admin/grok_import_pipeline_test.go backend/internal/handler/admin/account_data.go
git commit -m "feat: 导入 grok 账号后自动后台刷新令牌并测试,取代配额探测"
```

---

### Task 7: 前端 xai 格式检测工具 + vitest

**Files:**
- Create: `frontend/src/utils/xaiImport.ts`
- Test: `frontend/src/utils/__tests__/xaiImport.spec.ts`(若 utils 下已有 `__tests__` 或 `*.spec.ts` 惯例,跟随现有位置;没有就用本路径)

**Interfaces:**
- Produces: `extractXaiAccounts(parsed: unknown): Record<string, unknown>[] | null`——Task 8 导入弹窗消费;识别规则:含非空字符串 `access_token` 的对象,或全部元素满足该条件的非空数组;其余返回 null

- [ ] **Step 1: 写失败测试**

新建 `frontend/src/utils/__tests__/xaiImport.spec.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { extractXaiAccounts } from '../xaiImport'

describe('extractXaiAccounts', () => {
  it('识别单个 xai 账号对象', () => {
    const parsed = { access_token: 'at', refresh_token: 'rt', email: 'a@x.ai' }
    expect(extractXaiAccounts(parsed)).toEqual([parsed])
  })

  it('识别 xai 账号数组', () => {
    const parsed = [{ access_token: 'a1' }, { access_token: 'a2' }]
    expect(extractXaiAccounts(parsed)).toEqual(parsed)
  })

  it('数组含无 access_token 元素时整体拒绝', () => {
    expect(extractXaiAccounts([{ access_token: 'a1' }, { refresh_token: 'r2' }])).toBeNull()
  })

  it('拒绝 sub2api-data payload / 空数组 / 非对象', () => {
    expect(extractXaiAccounts({ type: 'sub2api-data', proxies: [], accounts: [] })).toBeNull()
    expect(extractXaiAccounts([])).toBeNull()
    expect(extractXaiAccounts('text')).toBeNull()
    expect(extractXaiAccounts(null)).toBeNull()
    expect(extractXaiAccounts({ access_token: '' })).toBeNull()
  })
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && npx vitest run src/utils/__tests__/xaiImport.spec.ts`
Expected: FAIL——模块不存在。

- [ ] **Step 3: 实现**

新建 `frontend/src/utils/xaiImport.ts`:

```ts
// CPA(xai-*.json)导入格式检测(定制)。
// 识别 GROK CPA 导出的账号 JSON:单个对象或数组,特征是含非空字符串 access_token。
// 只做检测与透传,字段解析(JWT/base_url 等)在后端 convertXaiAccount 完成。

const isXaiAccountObject = (value: unknown): value is Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const accessToken = (value as Record<string, unknown>).access_token
  return typeof accessToken === 'string' && accessToken.trim() !== ''
}

export const extractXaiAccounts = (parsed: unknown): Record<string, unknown>[] | null => {
  if (isXaiAccountObject(parsed)) return [parsed]
  if (Array.isArray(parsed) && parsed.length > 0 && parsed.every(isXaiAccountObject)) {
    return parsed
  }
  return null
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `npx vitest run src/utils/__tests__/xaiImport.spec.ts`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/utils/xaiImport.ts frontend/src/utils/__tests__/xaiImport.spec.ts
git commit -m "feat: 前端新增 CPA(xai)导入格式检测工具"
```

---

### Task 8: 导入弹窗接入 xai 格式 + 类型 + i18n + 结果提示

**Files:**
- Modify: `frontend/src/components/admin/account/ImportDataModal.vue`
- Modify: `frontend/src/types/index.ts:1234`(AdminDataPayload)、`:1277`(AdminDataImportResult)
- Modify: `frontend/src/i18n/locales/zh/admin/accounts.ts:31-32` 附近、`frontend/src/i18n/locales/en/admin/accounts.ts` 对应位置

**Interfaces:**
- Consumes: `extractXaiAccounts`(Task 7)、后端 `xai_accounts` 字段与 `grok_pipeline_scheduled` 结果字段(Task 5/6)
- Produces: 用户可在现有导入弹窗直接选 xai-*.json 文件(可与 sub2api-data 文件混选)

- [ ] **Step 1: 类型扩展**

`frontend/src/types/index.ts` 中 `AdminDataPayload`(1234 行)增加成员:

```ts
  xai_accounts?: Record<string, unknown>[]
```

`AdminDataImportResult`(1277 行)增加成员:

```ts
  grok_pipeline_scheduled?: number
```

- [ ] **Step 2: ImportDataModal 逐文件识别**

`ImportDataModal.vue` 改动三处:

2a. `<script setup>` 顶部 import 增加:

```ts
import { extractXaiAccounts } from '@/utils/xaiImport'
```

2b. `handleImport` 中逐文件解析段(269-293 行),把:

```ts
      if (!isValidDataPayload(parsed)) {
        appStore.showError(t('admin.accounts.dataImportInvalidFile', { name: sourceFile.name }))
        return
      }
      dataPayloads.push(parsed)
```

改为:

```ts
      if (isValidDataPayload(parsed)) {
        dataPayloads.push(parsed)
        continue
      }
      // CPA(xai-*.json)格式:检测后包装成带 xai_accounts 的 payload,由后端转换(定制)
      const xaiAccounts = extractXaiAccounts(parsed)
      if (xaiAccounts) {
        dataPayloads.push({
          type: 'sub2api-data',
          version: 1,
          exported_at: new Date().toISOString(),
          proxies: [],
          accounts: [],
          xai_accounts: xaiAccounts
        })
        continue
      }
      appStore.showError(t('admin.accounts.dataImportInvalidFile', { name: sourceFile.name }))
      return
```

(`for...of` 循环体内 `continue` 合法;若当前实现是 `for (const sourceFile of files.value)` 保持不变。)

2c. `mergeDataPayloads`(252-267 行)返回对象增加:

```ts
    xai_accounts: payloads.flatMap((item) => item.xai_accounts || []),
```

注意单文件早退分支 `if (payloads.length === 1 && firstPayload) return firstPayload` 不受影响。

2d. 导入成功处理(303-319 行),在 `msgParams` 声明后加:

```ts
    if ((res.grok_pipeline_scheduled || 0) > 0) {
      appStore.showInfo(
        t('admin.accounts.dataImportPipelineScheduled', { count: res.grok_pipeline_scheduled })
      )
    }
```

(若 `appStore` 无 `showInfo`,用 `showSuccess`。)

- [ ] **Step 3: i18n**

`frontend/src/i18n/locales/zh/admin/accounts.ts` 在 `dataImportHint`(31 行)替换及附近追加:

```ts
      dataImportHint: '上传导出的 JSON 文件以批量导入账号与代理;也支持直接选择 GROK CPA 导出的 xai-*.json(自动转换为 grok OAuth 账号)。',
      dataImportPipelineScheduled: '{count} 个 grok 账号已进入后台刷新令牌+测试,失败将自动置为错误状态',
```

`frontend/src/i18n/locales/en/admin/accounts.ts` 对应键(用 grep 定位 `dataImportHint`):

```ts
      dataImportHint: 'Upload exported JSON files to import accounts and proxies. GROK CPA xai-*.json files are also supported (auto-converted to grok OAuth accounts).',
      dataImportPipelineScheduled: '{count} grok account(s) queued for background token refresh + test; failures will be marked as error',
```

- [ ] **Step 4: 类型检查 + 全量前端测试**

Run: `cd frontend && npx vue-tsc --noEmit && npx vitest run`
Expected: 均通过。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/admin/account/ImportDataModal.vue frontend/src/types/index.ts frontend/src/i18n/locales/zh/admin/accounts.ts frontend/src/i18n/locales/en/admin/accounts.ts
git commit -m "feat: 导入弹窗支持 CPA(xai-*.json)格式并提示后台流水进度"
```

---

### Task 9: 前端批量测试按钮 + API + 按平台选模型确认弹窗 + 验证 grok-4.5 默认

> **选模型**:批测确认弹窗按**每个平台一行**模型下拉(同/跨平台均可);组装 models_by_platform 后 atchTest(ids, map)。

**Files:**
- Modify: `frontend/src/api/admin/accounts.ts:766` 附近(batchRefresh 之后加 batchTest)与文件底部导出对象
- Modify: `frontend/src/components/admin/account/AccountBulkActionsBar.vue`
- Modify: `frontend/src/views/admin/AccountsView.vue:183` 附近(事件接线)与 `:1412` 附近(handler)
- Modify: `frontend/src/i18n/locales/zh/admin/accounts.ts:410-427`(bulkActions)、en 对应

**Interfaces:**
- Consumes: `POST /admin/accounts/batch-test`(Task 4)
- Produces: 批量操作栏「批量测试」按钮

- [ ] **Step 1: API 封装**

`frontend/src/api/admin/accounts.ts` 在 `batchRefresh`(766-773 行)之后追加:

```ts
export interface BatchTestResultItem {
  id: number
  name: string
  status: 'success' | 'failed'
  error_message?: string
  latency_ms: number
}

export interface BatchTestResult {
  total: number
  success: number
  failed: number
  results: BatchTestResultItem[]
}

/**
 * 批量测试账号连通性(定制)。grok 账号由后端用默认 grok-4.5 测试,
 * 失败账号会被自动置为错误状态。
 */
/**
 * 批量测试账号连通性(定制)。
 * modelsByPlatform 可选: platform → model_id;某平台缺省则后端用该平台默认。
 */
export async function batchTest(
  accountIds: number[],
  modelsByPlatform?: Record<string, string>
): Promise<BatchTestResult> {
  const body: {
    account_ids: number[]
    models_by_platform?: Record<string, string>
  } = {
    account_ids: accountIds,
  }
  if (modelsByPlatform && Object.keys(modelsByPlatform).length > 0) {
    body.models_by_platform = modelsByPlatform
  }
  const { data } = await apiClient.post<BatchTestResult>('/admin/accounts/batch-test', body, {
    timeout: 300000  // 批量逐账号实测,大批量耗时长
  })
  return data
}
```

并在文件底部导出对象(908-915 行附近)加入 `batchTest,`(紧邻 `batchRefresh,`)。

- [ ] **Step 2: 批量操作栏按钮**

`AccountBulkActionsBar.vue`:44-45 行之间(resetStatus 与 refreshToken 之间)插入:

```html
        <button class="btn btn-secondary btn-sm" @click="$emit('test')">{{ t('admin.accounts.bulkActions.test') }}</button>
```

`defineEmits`(67 行)数组追加 `'test'`。

- [ ] **Step 3: AccountsView 接线**

3a. 模板(183 行附近)`@refresh-token="handleBulkRefreshToken"` 之后加一行:

```html
          @test="handleBulkTest"
```

3b. `handleBulkRefreshToken`(1412-1427 行)之后追加:

```ts
const handleBulkTest = async () => {
  if (!confirm(t('common.confirm'))) return
  try {
    // 批测确认弹窗:按勾选账号出现的平台各渲染一行模型下拉(同/跨平台均可)
    // 伪代码: const modelsByPlatform = await openBatchTestDialog(selectedAccounts)
    const result = await adminAPI.accounts.batchTest(selIds.value, modelsByPlatform)
    if (result.failed > 0) {
      appStore.showError(t('admin.accounts.bulkActions.testCompletedWithFailures', { success: result.success, failed: result.failed }))
    } else {
      appStore.showSuccess(t('admin.accounts.bulkActions.testSuccess', { count: result.success }))
      clearSelection()
    }
    reload()
  } catch (error) {
    console.error('Failed to bulk test accounts:', error)
    appStore.showError(String(error))
  }
}
```

- [ ] **Step 4: i18n**

zh `bulkActions`(410-427 行)追加:

```ts
        test: '批量测试',
        testSuccess: '已完成 {count} 个账号测试,全部通过',
        testCompletedWithFailures: '批量测试完成:{success} 通过,{failed} 失败(失败账号已按规则置错)',
```

en `bulkActions`(约 310-322 行,grep `refreshTokenSuccess` 定位)追加:

```ts
        test: 'Batch Test',
        testSuccess: 'Tested {count} account(s), all passed',
        testCompletedWithFailures: 'Batch test finished: {success} passed, {failed} failed (failed accounts marked as error)',
```

- [ ] **Step 5: 验证 grok-4.5 默认选中(只读检查)**

打开 `frontend/src/components/admin/account/AccountTestModal.vue` 的 `loadAvailableModels`(约 345 行),确认对 grok 账号:默认选中值为后端 `GET /accounts/:id/models` 返回列表的哪一项。后端对 grok 返回 `xai.DefaultModels()`,首项即 `grok-4.5`(pkg/xai/models.go:13)。若默认选中逻辑取列表首项或 `default` 标记项,无需改动;若默认为空(让用户手选),把 grok 平台的默认选中改为列表首项。只在不满足时改,改动控制在默认选中逻辑一行。

- [ ] **Step 6: 类型检查 + 全量前端测试**

Run: `cd frontend && npx vue-tsc --noEmit && npx vitest run`
Expected: 均通过。

- [ ] **Step 7: 提交**

```bash
git add frontend/src/api/admin/accounts.ts frontend/src/components/admin/account/AccountBulkActionsBar.vue frontend/src/views/admin/AccountsView.vue frontend/src/i18n/locales/zh/admin/accounts.ts frontend/src/i18n/locales/en/admin/accounts.ts
git commit -m "feat: 账号页新增批量测试按钮,接入 batch-test 端点"
```

(若 Step 5 改了 AccountTestModal.vue,一并 add 并在提交信息中注明。)

---

### Task 10: CUSTOM_CHANGES.md 登记 + 全量回归

**Files:**
- Modify: `CUSTOM_CHANGES.md`(「持续维护的定制功能清单」表格追加行)

- [ ] **Step 1: 登记定制功能**

读 `CUSTOM_CHANGES.md`,在「持续维护的定制功能清单」表格按现有格式追加(列名以现有表格为准,内容如下):

| 功能 | 关键文件 | 说明 |
|---|---|---|
| 账号批量测试端点 | `backend/internal/handler/admin/account_handler.go`(BatchTest)、`backend/internal/server/routes/admin.go`、前端 AccountsView/AccountBulkActionsBar/accounts.ts | POST /accounts/batch-test,并发10,复用 RunTestBackground |
| grok 测试/刷新失败置错 | `backend/internal/service/account_test_service.go`(testGrokAccountConnection)、`service/grok_refresh_failure.go`、`pkg/xai/errors.go`、`repository/grok_oauth_client.go`、`handler/admin/account_handler.go`(refreshSingleAccount)、`grok_oauth_handler.go` | 上游4xx(非429)→SetError;429限流态;5xx/网络不改状态 |
| CPA(xai-*.json)导入 | `backend/internal/handler/admin/account_data_xai.go`、`account_data.go`(XaiAccounts 字段)、前端 ImportDataModal/utils/xaiImport.ts | 导入弹窗自动识别,服务端 JWT 解析转换 |
| 导入后刷新+测试流水 | `backend/internal/handler/admin/grok_import_pipeline.go`、`account_data.go`(替换 scheduleGrokImportProbe 调用) | grok OAuth 导入后异步刷新→grok-4.5 测试;**合并上游时注意保留 importData 中的替换点** |

- [ ] **Step 2: 全量回归**

Run(后端): `cd backend && go test -tags unit ./internal/... 2>&1 | tail -30`
Run(前端): `cd frontend && npx vue-tsc --noEmit && npx vitest run`
Run(编译): `cd backend && go build ./...`
Expected: 全部通过(除已知偶发超时用例)。

- [ ] **Step 3: 提交**

```bash
git add CUSTOM_CHANGES.md
git commit -m "docs: CUSTOM_CHANGES 登记批量测试/失败置错/CPA导入/导入后流水"
```
