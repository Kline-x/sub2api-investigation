//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type grokAccountTestRateLimitRepo struct {
	*mockAccountRepoForGemini
	rateLimitedCalls int
	resetAt          time.Time
}

func (r *grokAccountTestRateLimitRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.rateLimitedCalls++
	r.resetAt = resetAt
	return nil
}

func TestAccountTestService_TestAccountConnection_GrokUsesXAIResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := &Account{
		ID:          13,
		Name:        "grok-oauth",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
			"model_mapping": map[string]any{
				"grok": "grok-4.3",
			},
		},
	}
	repo := &mockAccountRepoForGemini{
		accountsByID: map[int64]*Account{account.ID: account},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\"}\n\n",
		)),
	}}
	svc := &AccountTestService{
		accountRepo:       repo,
		grokTokenProvider: NewGrokTokenProvider(repo, nil),
		httpUpstream:      upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/13/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "grok", "", AccountTestModeDefault)
	require.NoError(t, err)

	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer grok-access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, grokCLIVersion, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, "application/json, text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "grok-4.3", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, grokQuotaProbeInput, gjson.GetBytes(upstream.lastBody, "input").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "max_output_tokens").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Exists())
	require.NotContains(t, rec.Body.String(), "claude")
	require.Contains(t, rec.Body.String(), `"model":"grok-4.3"`)
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestAccountTestService_TestAccountConnection_GrokDefaultsEmptyModelTo45(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := &Account{
		ID:          16,
		Name:        "grok-oauth-default-model",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\"}\n\n",
		)),
	}}
	svc := &AccountTestService{
		accountRepo:       repo,
		grokTokenProvider: NewGrokTokenProvider(repo, nil),
		httpUpstream:      upstream,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/16/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.NoError(t, err)
	require.Equal(t, grokDefaultResponsesModel, gjson.GetBytes(upstream.lastBody, "model").String())
	require.Contains(t, recorder.Body.String(), `"model":"grok-4.5"`)
}

func TestAccountTestService_Grok429PersistsRateLimitReset(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := &Account{
		ID:          14,
		Name:        "grok-oauth-limited",
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	baseRepo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	repo := &grokAccountTestRateLimitRepo{mockAccountRepoForGemini: baseRepo}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"45"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := &AccountTestService{
		accountRepo:       repo,
		grokTokenProvider: NewGrokTokenProvider(repo, nil),
		httpUpstream:      upstream,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/14/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "grok", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, time.Now().Add(45*time.Second), repo.resetAt, time.Second)
}

func TestAccountTestService_Grok429WithoutQuotaHeadersUsesFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := &Account{
		ID: 15, Name: "grok-oauth-limited-no-headers", Platform: PlatformGrok,
		Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
	baseRepo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
	repo := &grokAccountTestRateLimitRepo{mockAccountRepoForGemini: baseRepo}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"quota exhausted"}}`)),
	}}
	svc := &AccountTestService{
		accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/15/test", nil)
	before := time.Now()

	err := svc.TestAccountConnection(c, account.ID, "grok", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.rateLimitedCalls)
	require.WithinDuration(t, before.Add(grokRateLimitFallbackCooldown), repo.resetAt, time.Second)
}

type grokAccountTestTempUnschedRepo struct {
	*mockAccountRepoForGemini
	setTempUnschedCalls int
	lastTempReason      string
	lastTempUntil       time.Time
	setErrorCalls       int
	lastErrorMsg        string
}

func (r *grokAccountTestTempUnschedRepo) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.setTempUnschedCalls++
	r.lastTempUntil = until
	r.lastTempReason = reason
	return nil
}

func (r *grokAccountTestTempUnschedRepo) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

// SetRateLimited 计数,供 429 用例断言限流路径仍生效
type grokAccountTestTempUnschedAndRateLimitRepo struct {
	*grokAccountTestTempUnschedRepo
	rateLimitedCalls int
}

func (r *grokAccountTestTempUnschedAndRateLimitRepo) SetRateLimited(_ context.Context, _ int64, _ time.Time) error {
	r.rateLimitedCalls++
	return nil
}

func newGrokTempUnschedTestAccount(id int64) *Account {
	return &Account{
		ID: id, Name: "grok-temp-unsched", Platform: PlatformGrok,
		Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "grok-access-token",
			"refresh_token": "grok-refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}
}

func TestAccountTestService_Grok403SetsTempUnschedulable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokTempUnschedTestAccount(31)
	repo := &grokAccountTestTempUnschedRepo{
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
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Contains(t, repo.lastTempReason, "403")
}

func TestAccountTestService_Grok400SetsTempUnschedulable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokTempUnschedTestAccount(34)
	repo := &grokAccountTestTempUnschedRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(`{"error":"bad request"}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/34/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Contains(t, repo.lastTempReason, "400")
}

func TestAccountTestService_Grok429DoesNotSetTempUnschedulable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokTempUnschedTestAccount(32)
	base := &grokAccountTestTempUnschedRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	repo := &grokAccountTestTempUnschedAndRateLimitRepo{grokAccountTestTempUnschedRepo: base}
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
	require.Zero(t, base.setTempUnschedCalls)
	require.Zero(t, base.setErrorCalls)
	require.Equal(t, 1, repo.rateLimitedCalls)
}

func TestAccountTestService_Grok502SetsTempUnschedulable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokTempUnschedTestAccount(33)
	repo := &grokAccountTestTempUnschedRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader(`{"error":"bad gateway"}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/33/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Contains(t, repo.lastTempReason, "502")
}

func TestAccountTestService_Grok500SetsTempUnschedulable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newGrokTempUnschedTestAccount(35)
	repo := &grokAccountTestTempUnschedRepo{
		mockAccountRepoForGemini: &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(`{"error":"upstream down"}`)),
	}}
	svc := &AccountTestService{accountRepo: repo, grokTokenProvider: NewGrokTokenProvider(repo, nil), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/35/test", nil)

	err := svc.TestAccountConnection(c, account.ID, "", "", AccountTestModeDefault)

	require.Error(t, err)
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Contains(t, repo.lastTempReason, "500")
}
