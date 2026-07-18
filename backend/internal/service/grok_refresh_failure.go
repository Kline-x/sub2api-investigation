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
