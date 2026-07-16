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
