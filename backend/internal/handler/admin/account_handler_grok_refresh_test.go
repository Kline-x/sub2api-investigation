//go:build unit

package admin

import (
	"context"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type grokRefreshOAuthStub struct {
	account *service.Account
	info    *service.GrokTokenInfo
	calls   int
}

func (s *grokRefreshOAuthStub) RefreshAccountToken(_ context.Context, account *service.Account) (*service.GrokTokenInfo, error) {
	s.calls++
	s.account = account
	return s.info, nil
}

func (s *grokRefreshOAuthStub) BuildAccountCredentials(info *service.GrokTokenInfo) map[string]any {
	return map[string]any{
		"access_token":  info.AccessToken,
		"refresh_token": info.RefreshToken,
		"expires_at":    info.ExpiresAt,
		"base_url":      "https://api.x.ai/v1",
	}
}

type grokRefreshAdminService struct {
	*stubAdminService
	updatedCredentials map[string]any
}

func (s *grokRefreshAdminService) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updatedCredentials = input.Credentials
	return &service.Account{
		ID:          id,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Credentials: input.Credentials,
	}, nil
}

func TestRefreshSingleAccountRoutesGrokThroughGrokOAuthService(t *testing.T) {
	t.Parallel()

	adminSvc := &grokRefreshAdminService{stubAdminService: newStubAdminService()}
	grokOAuth := &grokRefreshOAuthStub{info: &service.GrokTokenInfo{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresAt:    1_800_000_000,
	}}
	handler := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		grokOAuth,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	account := &service.Account{
		ID:       4227,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "old-access",
			"refresh_token":      "old-refresh",
			"base_url":           "https://example.invalid/v1",
			"subscription_tier":  "SUPER_GROK",
			"entitlement_status": "ACTIVE",
		},
	}

	updated, warning, err := handler.refreshSingleAccount(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, warning)
	require.Equal(t, 1, grokOAuth.calls)
	require.Same(t, account, grokOAuth.account)
	require.Equal(t, "new-access", adminSvc.updatedCredentials["access_token"])
	require.Equal(t, "new-refresh", adminSvc.updatedCredentials["refresh_token"])
	require.Equal(t, "https://example.invalid/v1", adminSvc.updatedCredentials["base_url"])
	require.Equal(t, "SUPER_GROK", adminSvc.updatedCredentials["subscription_tier"])
	require.Equal(t, "ACTIVE", adminSvc.updatedCredentials["entitlement_status"])
	require.Equal(t, adminSvc.updatedCredentials, updated.Credentials)
}

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
