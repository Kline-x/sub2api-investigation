//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestBuildAccountQuotaSummaryAggregatesStatusAndPlatforms(t *testing.T) {
	now := time.Now().UTC()
	rateLimitReset := now.Add(time.Hour)
	grokUsage := 100.0
	accounts := []Account{
		{
			ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			RateLimitResetAt: &rateLimitReset,
			Extra: map[string]any{
				grokBillingExtraKey:                 &xai.BillingSummary{UsagePercent: &grokUsage},
				grokFreeUsageExhaustedUntilExtraKey: now.Add(24 * time.Hour).Format(time.RFC3339),
			},
		},
		{
			ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			Extra: map[string]any{"passive_usage_7d_utilization": 0.5},
		},
		{ID: 3, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusError, Schedulable: true},
	}

	summary := buildAccountQuotaSummary(accounts, now)

	require.Equal(t, 3, summary.Status.Total)
	require.Equal(t, 1, summary.Status.Schedulable)
	require.Equal(t, 1, summary.Status.RateLimited)
	require.Equal(t, 1, summary.Status.QuotaExhausted)
	require.Equal(t, 1, summary.Status.Error)
	require.Equal(t, 1, summary.Status.QuotaUnknown)
	require.Len(t, summary.Platforms, 2)
	require.Equal(t, PlatformAnthropic, summary.Platforms[0].Platform)
	require.Equal(t, 2, summary.Platforms[0].Total)
	require.Equal(t, 1, summary.Platforms[0].QuotaKnown)
	require.InDelta(t, 50, *summary.Platforms[0].AverageUtilizationPercent, 0.001)
	require.Equal(t, PlatformGrok, summary.Platforms[1].Platform)
	require.Equal(t, 1, summary.Platforms[1].QuotaExhausted)
	require.InDelta(t, 100, *summary.Platforms[1].AverageUtilizationPercent, 0.001)
}
