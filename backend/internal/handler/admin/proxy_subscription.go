package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/clashsub"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	subscriptionFetchTimeout = 30 * time.Second
	subscriptionMaxBytes     = 8 << 20 // 8 MiB，防御超大响应
)

// importSubscriptionRequest 是 POST /admin/proxies/import-subscription 的请求体。
type importSubscriptionRequest struct {
	URL    string `json:"url"`
	DryRun bool   `json:"dry_run"`
}

// importSubscriptionResponse 汇报本次订阅导入的结果。
type importSubscriptionResponse struct {
	Created int                `json:"created"`
	Updated int                `json:"updated"`
	Skipped []clashsub.Skipped `json:"skipped"`
}

// ImportSubscription 拉取 Clash 格式订阅，提取其中受支持的 ss 节点并导入为代理记录。
// dry_run=true 时只统计将要创建/更新的数量，不写库。
// POST /api/v1/admin/proxies/import-subscription
func (h *ProxyHandler) ImportSubscription(c *gin.Context) {
	ctx := c.Request.Context()

	var req importSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		response.BadRequest(c, "subscription url is required")
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		response.BadRequest(c, "subscription url must be http or https")
		return
	}

	data, err := fetchSubscription(ctx, req.URL)
	if err != nil {
		// 订阅链接本身通常携带机场分配的密钥（query 参数或路径），不能把 err 原样透传给客户端
		// 或写进日志：net/http 的错误里经常内嵌完整请求 URL。
		response.BadRequest(c, "fetch subscription failed: "+sanitizeFetchErr(err))
		return
	}

	nodes, skipped, err := clashsub.Parse(data)
	if err != nil {
		response.BadRequest(c, fmt.Sprintf("parse subscription failed: %v", err))
		return
	}

	resp := importSubscriptionResponse{Skipped: skipped}

	for _, n := range nodes {
		// 去重口径与既有 BatchCreate/导入逻辑一致：host + port + username(cipher) + password。
		exists, err := h.adminService.CheckProxyExists(ctx, n.Server, n.Port, n.Cipher, n.Password)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if exists {
			resp.Updated++
			continue
		}
		resp.Created++

		if req.DryRun {
			continue
		}

		extra := map[string]string{}
		if n.Plugin == "obfs" {
			extra["plugin"] = "obfs"
			extra["mode"] = n.ObfsMode
			extra["obfs-host"] = n.ObfsHost
		}

		if _, err := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:     n.Name,
			Protocol: "ss",
			Host:     n.Server,
			Port:     n.Port,
			Username: n.Cipher,
			Password: n.Password,
			Extra:    extra,
		}); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	response.Success(c, resp)
}

// fetchSubscription 拉取订阅内容，带超时与响应体大小上限护栏。
func fetchSubscription(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// 部分机场按 User-Agent 返回不同格式，显式要求 Clash 格式。
	req.Header.Set("User-Agent", "clash-verge/v1.0.0")

	client := &http.Client{Timeout: subscriptionFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, subscriptionMaxBytes))
}

// sanitizeFetchErr 从拉取失败的 error 中剥离订阅 URL（net/http 的 *url.Error 会把完整请求
// URL 内嵌在 Error() 文本里，而订阅 URL 常携带机场分配的密钥），只保留底层网络错误描述。
func sanitizeFetchErr(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
