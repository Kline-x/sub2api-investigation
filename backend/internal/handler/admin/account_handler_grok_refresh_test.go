package admin

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type accountHandlerGrokOAuthClientStub struct {
	refreshToken string
	clientID     string
}

func (s *accountHandlerGrokOAuthClientStub) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	return nil, nil
}

func (s *accountHandlerGrokOAuthClientStub) RefreshToken(_ context.Context, refreshToken, _ string, clientID string) (*xai.TokenResponse, error) {
	s.refreshToken = refreshToken
	s.clientID = clientID
	return &xai.TokenResponse{
		AccessToken:  "rotated-access-token",
		RefreshToken: "rotated-refresh-token",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
	}, nil
}

type accountHandlerGrokAdminService struct {
	*stubAdminService
	updatedCredentials map[string]any
}

func (s *accountHandlerGrokAdminService) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updatedCredentials = input.Credentials
	return &service.Account{
		ID:          id,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Credentials: input.Credentials,
	}, nil
}

func TestRefreshSingleAccount_GrokUsesGrokOAuthService(t *testing.T) {
	adminSvc := &accountHandlerGrokAdminService{stubAdminService: newStubAdminService()}
	client := &accountHandlerGrokOAuthClientStub{}
	grokOAuthSvc := service.NewGrokOAuthService(nil, client)
	defer grokOAuthSvc.Stop()

	h := &AccountHandler{
		adminService:     adminSvc,
		grokOAuthService: grokOAuthSvc,
	}
	account := &service.Account{
		ID:       3,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token": "stored-refresh-token",
			"client_id":     "stored-client-id",
			"base_url":      "https://cli-chat-proxy.grok.com/v1",
		},
	}

	updated, _, err := h.refreshSingleAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "stored-refresh-token", client.refreshToken)
	require.Equal(t, "stored-client-id", client.clientID)
	require.Equal(t, "rotated-access-token", updated.Credentials["access_token"])
	require.Equal(t, "rotated-refresh-token", updated.Credentials["refresh_token"])
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1", updated.Credentials["base_url"])
}
