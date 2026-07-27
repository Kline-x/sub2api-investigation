package repository

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/Wei-Shaw/sub2api/internal/pkg/shadowsocks"

	"github.com/imroc/req/v3"
)

// reqClientOptions 定义 req 客户端的构建参数
type reqClientOptions struct {
	ProxyURL    string        // 代理 URL（支持 http/https/socks5/socks5h/ss）
	Timeout     time.Duration // 请求超时时间
	Impersonate bool          // 是否模拟 Chrome 浏览器指纹
	ForceHTTP2  bool          // 是否强制使用 HTTP/2
}

// sharedReqClients 存储按配置参数缓存的 req 客户端实例
//
// 性能优化说明：
// 原实现在每次 OAuth 刷新时都创建新的 req.Client：
// 1. claude_oauth_service.go: 每次刷新创建新客户端
// 2. openai_oauth_service.go: 每次刷新创建新客户端
// 3. gemini_oauth_client.go: 每次刷新创建新客户端
//
// 新实现使用 sync.Map 缓存客户端：
// 1. 相同配置（代理+超时+模拟设置）复用同一客户端
// 2. 复用底层连接池，减少 TLS 握手开销
// 3. LoadOrStore 保证并发安全，避免重复创建
var sharedReqClients sync.Map

// getSharedReqClient 获取共享的 req 客户端实例
// 性能优化：相同配置复用同一客户端，避免重复创建
func getSharedReqClient(opts reqClientOptions) (*req.Client, error) {
	key := buildReqClientKey(opts)
	if cached, ok := sharedReqClients.Load(key); ok {
		if c, ok := cached.(*req.Client); ok {
			return c, nil
		}
	}

	client := req.C().SetTimeout(opts.Timeout)
	if opts.ForceHTTP2 {
		client = client.EnableForceHTTP2()
	}
	if opts.Impersonate {
		client = client.ImpersonateChrome()
	}
	if err := applyReqClientProxy(client, opts.ProxyURL); err != nil {
		return nil, err
	}
	client = instrumentReqClient(client)

	actual, _ := sharedReqClients.LoadOrStore(key, client)
	if c, ok := actual.(*req.Client); ok {
		return c, nil
	}
	return client, nil
}

// applyReqClientProxy 按代理 URL 为 req 客户端配置出站通道。
//
// 为什么不能对所有协议统一走 SetProxyURL：
// req 的传输层（Transport.dialConn）只识别 socks5/socks5h，其余 scheme 一律按
// HTTP 代理处理——对 https 目标发明文 CONNECT，对 http 目标直接把绝对 URI 发给
// 代理端口。ss 节点不说 HTTP，必然握手失败。因此 ss 必须改走自定义 DialContext：
// 把 ss 隧道作为底层 TCP 连接注入（client.SetDial），并且**不设置** Proxy——
// 这样 req 的 TLS 指纹握手（ImpersonateChrome）仍然在隧道之上正常进行。
//
// fail-fast 约束：ss 配置解析或 dialer 创建失败时必须返回 error，
// 绝不允许退化为"不设代理直连"（会暴露服务器真实 IP）。
func applyReqClientProxy(client *req.Client, proxyURL string) error {
	if client == nil {
		return fmt.Errorf("req client is nil")
	}

	trimmed, parsed, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return err
	}
	if trimmed == "" {
		// 空代理表示直连，保持现状
		return nil
	}

	// ss（shadowsocks，定制功能，合并上游时勿丢）
	if parsed != nil && strings.EqualFold(parsed.Scheme, "ss") {
		cfg, cfgErr := shadowsocks.ConfigFromURL(parsed)
		if cfgErr != nil {
			return cfgErr
		}
		ssDialer, dialErr := shadowsocks.NewDialer(cfg)
		if dialErr != nil {
			return dialErr
		}
		// req.T() 默认 Proxy = http.ProxyFromEnvironment。若不显式清掉，
		// 宿主机存在 HTTP(S)_PROXY 时 req 会认为"有代理"，转而用 ss 隧道去连那个
		// 环境代理并发 CONNECT —— 隧道分层错乱。账号已显式指定 ss 代理，
		// 环境变量必须让位。
		client.SetProxy(nil)
		client.SetDial(ssDialer.DialContext)
		return nil
	}

	client.SetProxyURL(trimmed)
	return nil
}

func instrumentReqClient(client *req.Client) *req.Client {
	if client == nil {
		return nil
	}
	client.GetTransport().WrapRoundTripFunc(func(rt http.RoundTripper) req.HttpRoundTripFunc {
		timed := servertiming.WrapRoundTripper(rt)
		return timed.RoundTrip
	})
	return client
}

func buildReqClientKey(opts reqClientOptions) string {
	return fmt.Sprintf("%s|%s|%t|%t",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.Impersonate,
		opts.ForceHTTP2,
	)
}

// CreatePrivacyReqClient creates an HTTP client for OpenAI privacy settings API
// This is exported for use by OpenAIPrivacyService
// Uses Chrome TLS fingerprint impersonation to bypass Cloudflare checks
func CreatePrivacyReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL:    proxyURL,
		Timeout:     30 * time.Second,
		Impersonate: true, // Enable Chrome TLS fingerprint impersonation
	})
}
