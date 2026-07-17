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
	require.Equal(t, "https://keep.invalid/v1", adminSvc.updatedCreds["base_url"])
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
	require.Zero(t, tester.calls)
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