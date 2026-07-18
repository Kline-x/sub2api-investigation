//go:build unit

package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func makeFakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	seg := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	return seg([]byte("{\"alg\":\"none\"}")) + "." + seg(payload) + ".sig"
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

	require.Equal(t, "field@example.com", item.Name)
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
			{"refresh_token": "rt-only"},
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