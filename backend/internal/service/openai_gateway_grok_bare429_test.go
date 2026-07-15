//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestNextGrokBare429State(t *testing.T) {
	now := time.Now()

	// 首次裸 429:从 1 开始,冷却为基础值。
	state, cooldown, escalate := nextGrokBare429State(grokBare429State{}, now)
	require.True(t, escalate)
	require.Equal(t, 1, state.count)
	require.Equal(t, grokRateLimitFallbackCooldown, cooldown)

	// 上一轮封禁未到期(并发在途请求的 429):不叠加。
	_, _, escalate = nextGrokBare429State(state, now.Add(time.Minute))
	require.False(t, escalate)

	// 封禁到期后又裸 429:连击 +1,冷却翻倍。
	state2, cooldown2, escalate := nextGrokBare429State(state, state.blockedUntil.Add(time.Second))
	require.True(t, escalate)
	require.Equal(t, 2, state2.count)
	require.Equal(t, 2*grokRateLimitFallbackCooldown, cooldown2)

	// 冷却封顶 1 小时。
	capped := grokBare429State{count: 20, blockedUntil: now.Add(-time.Second)}
	_, cooldownCap, escalate := nextGrokBare429State(capped, now)
	require.True(t, escalate)
	require.Equal(t, grokBare429EscalationCap, cooldownCap)

	// 长时间无复发:连击归零重新计。
	stale := grokBare429State{count: 5, blockedUntil: now.Add(-grokBare429StreakStaleAfter - time.Minute)}
	stateReset, cooldownReset, escalate := nextGrokBare429State(stale, now)
	require.True(t, escalate)
	require.Equal(t, 1, stateReset.count)
	require.Equal(t, grokRateLimitFallbackCooldown, cooldownReset)
}

func TestGrokSnapshotHasExplicitRateLimitReset(t *testing.T) {
	now := time.Now()

	require.False(t, grokSnapshotHasExplicitRateLimitReset(nil, now))
	require.False(t, grokSnapshotHasExplicitRateLimitReset(&xai.QuotaSnapshot{StatusCode: 429}, now))

	retryAfter := 45
	require.True(t, grokSnapshotHasExplicitRateLimitReset(&xai.QuotaSnapshot{
		StatusCode:        429,
		RetryAfterSeconds: &retryAfter,
	}, now))

	remaining := int64(0)
	resetUnix := now.Add(10 * time.Minute).Unix()
	require.True(t, grokSnapshotHasExplicitRateLimitReset(&xai.QuotaSnapshot{
		StatusCode: 429,
		Requests:   &xai.QuotaWindow{Remaining: &remaining, ResetUnix: &resetUnix},
	}, now))

	// 窗口未耗尽或 reset 已过期都不算有明确信息。
	remainingPositive := int64(3)
	require.False(t, grokSnapshotHasExplicitRateLimitReset(&xai.QuotaSnapshot{
		StatusCode: 429,
		Requests:   &xai.QuotaWindow{Remaining: &remainingPositive, ResetUnix: &resetUnix},
	}, now))
	expiredReset := now.Add(-time.Minute).Unix()
	require.False(t, grokSnapshotHasExplicitRateLimitReset(&xai.QuotaSnapshot{
		StatusCode: 429,
		Requests:   &xai.QuotaWindow{Remaining: &remaining, ResetUnix: &expiredReset},
	}, now))
}

// 连续裸 429(无 retry-after / 无窗口 reset / 非 free 耗尽文案)应递增封禁,
// 而不是每次都只封 2 分钟导致账号反复进出调度池。
func TestHandleGrokAccountUpstreamErrorBare429EscalatesCooldown(t *testing.T) {
	account := &Account{ID: 72, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	now := time.Now()

	// 模拟上一轮 2 分钟封禁刚到期后又收到裸 429。
	svc.grokBare429States.Store(account.ID, grokBare429State{count: 1, blockedUntil: now.Add(-time.Second)})

	svc.handleGrokAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, nil, []byte(`{"error":"rate_limited"}`))

	require.GreaterOrEqual(t, repo.rateLimitedCalls, 1)
	require.WithinDuration(t, now.Add(4*time.Minute), repo.lastRateLimitResetAt, 2*time.Second,
		"第二次裸 429 应封 4 分钟而不是 2 分钟")
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))

	value, ok := svc.grokBare429States.Load(account.ID)
	require.True(t, ok)
	state, ok := value.(grokBare429State)
	require.True(t, ok)
	require.Equal(t, 2, state.count)
}

// 带 Retry-After 的 429 有明确 reset 信息,不参与裸 429 连击递增。
func TestHandleGrokAccountUpstreamError429WithRetryAfterDoesNotEscalate(t *testing.T) {
	account := &Account{ID: 73, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}

	svc.handleGrokAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests,
		http.Header{"Retry-After": []string{"45"}}, []byte(`{"error":"rate_limited"}`))

	_, ok := svc.grokBare429States.Load(account.ID)
	require.False(t, ok, "有明确 reset 信息的 429 不应记入裸 429 连击")
}
