package service

import (
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

type AccountQuotaStatusSummary struct {
	Total          int `json:"total"`
	Schedulable    int `json:"schedulable"`
	RateLimited    int `json:"rate_limited"`
	QuotaExhausted int `json:"quota_exhausted"`
	Error          int `json:"error"`
	QuotaUnknown   int `json:"quota_unknown"`
}

type AccountPlatformQuotaSummary struct {
	Platform                  string   `json:"platform"`
	Total                     int      `json:"total"`
	QuotaKnown                int      `json:"quota_known"`
	AverageUtilizationPercent *float64 `json:"average_utilization_percent"`
	QuotaExhausted            int      `json:"quota_exhausted"`
	utilizationPercentTotal   float64
}

type AccountQuotaSummary struct {
	Status      AccountQuotaStatusSummary     `json:"status"`
	Platforms   []AccountPlatformQuotaSummary `json:"platforms"`
	CollectedAt time.Time                     `json:"collected_at"`
}

func buildAccountQuotaSummary(accounts []Account, now time.Time) *AccountQuotaSummary {
	result := &AccountQuotaSummary{CollectedAt: now.UTC()}
	byPlatform := make(map[string]*AccountPlatformQuotaSummary)
	for i := range accounts {
		account := &accounts[i]
		result.Status.Total++
		if account.IsSchedulable() {
			result.Status.Schedulable++
		}
		if account.RateLimitResetAt != nil && account.RateLimitResetAt.After(now) {
			result.Status.RateLimited++
		}
		if account.Status == StatusError {
			result.Status.Error++
		}

		platform := byPlatform[account.Platform]
		if platform == nil {
			platform = &AccountPlatformQuotaSummary{Platform: account.Platform}
			byPlatform[account.Platform] = platform
		}
		platform.Total++
		utilization, known, exhausted := persistedAccountQuotaUtilization(account, now)
		if known {
			platform.QuotaKnown++
			platform.utilizationPercentTotal += utilization
		} else {
			result.Status.QuotaUnknown++
		}
		if exhausted {
			result.Status.QuotaExhausted++
			platform.QuotaExhausted++
		}
	}

	for _, platform := range byPlatform {
		if platform.QuotaKnown > 0 {
			average := platform.utilizationPercentTotal / float64(platform.QuotaKnown)
			platform.AverageUtilizationPercent = &average
		}
		result.Platforms = append(result.Platforms, *platform)
	}
	sort.Slice(result.Platforms, func(i, j int) bool {
		return result.Platforms[i].Platform < result.Platforms[j].Platform
	})
	return result
}

func persistedAccountQuotaUtilization(account *Account, now time.Time) (float64, bool, bool) {
	if account == nil {
		return 0, false, false
	}
	if marker, ok := account.Extra[grokFreeUsageExhaustedUntilExtraKey].(string); ok {
		if until, err := time.Parse(time.RFC3339, strings.TrimSpace(marker)); err == nil && until.After(now) {
			return 100, true, true
		}
	}
	if account.Platform == PlatformGrok {
		if billing, err := grokBillingSnapshotFromExtra(account.Extra); err == nil && billing != nil {
			if billing.UsagePercent != nil {
				value := clampQuotaPercent(*billing.UsagePercent)
				return value, true, value >= 100
			}
			if billing.UsedPercent != nil {
				value := clampQuotaPercent(*billing.UsedPercent)
				return value, true, value >= 100
			}
		}
		if snapshot, err := grokQuotaSnapshotFromExtra(account.Extra); err == nil && snapshot != nil {
			return grokSnapshotUtilization(snapshot)
		}
	}

	maxUtilization := -1.0
	for _, key := range []string{"session_window_utilization", "passive_usage_7d_utilization", "passive_usage_7d_oi_utilization"} {
		if _, exists := account.Extra[key]; !exists {
			continue
		}
		value := parseExtraFloat64(account.Extra[key])
		if value >= 0 && value <= 1 {
			value *= 100
		}
		if value > maxUtilization {
			maxUtilization = value
		}
	}
	if maxUtilization >= 0 {
		value := clampQuotaPercent(maxUtilization)
		return value, true, value >= 100
	}
	return 0, false, false
}

func grokSnapshotUtilization(snapshot *xai.QuotaSnapshot) (float64, bool, bool) {
	maxUtilization := -1.0
	exhausted := false
	for _, window := range []*xai.QuotaWindow{snapshot.Requests, snapshot.Tokens} {
		if window == nil || window.Limit == nil || window.Remaining == nil || *window.Limit <= 0 {
			continue
		}
		value := (float64(*window.Limit-*window.Remaining) / float64(*window.Limit)) * 100
		if value > maxUtilization {
			maxUtilization = value
		}
		if *window.Remaining <= 0 {
			exhausted = true
		}
	}
	if maxUtilization < 0 {
		return 0, false, false
	}
	return clampQuotaPercent(maxUtilization), true, exhausted
}

func clampQuotaPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
