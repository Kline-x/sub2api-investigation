package xai

import "fmt"

// OAuthUpstreamStatusError 记录 OAuth token 端点返回的上游 HTTP 状态码。
// 作为 ApplicationError 的 cause 挂载,供调用方用 errors.As 做结构化分类
// (定制:手动/导入刷新失败按上游状态码决定是否置错)。
type OAuthUpstreamStatusError struct {
	Status int
}

func (e *OAuthUpstreamStatusError) Error() string {
	return fmt.Sprintf("oauth upstream status %d", e.Status)
}
