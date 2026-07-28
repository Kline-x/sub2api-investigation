package repository

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// TestApplyReqClientProxy_SS走自定义Dial而非CONNECT 验证 req 客户端路径下的 ss 分派：
// req 的传输层只识别 socks5/socks5h，其余 scheme 一律按 HTTP 代理处理（发明文 CONNECT），
// 因此 ss 必须改走 client.SetDial 注入的隧道 DialContext，并且不得设置 Proxy。
//
// 断言首字节为 0x16(TLS ClientHello) 而不是 "CONNEC"，正是在证明"没有走 CONNECT"。
func TestApplyReqClientProxy_SS走自定义Dial而非CONNECT(t *testing.T) {
	addr, firstBytesCh, targetAddrCh := startFakeSSNode(t)

	proxyURL := (&url.URL{
		Scheme: "ss",
		User:   url.UserPassword(testSSCipher, testSSPassword),
		Host:   addr,
	}).String()

	// ImpersonateChrome 是 OAuth 客户端的真实用法：需要确认 TLS 指纹握手仍在隧道之上进行
	client := req.C().SetTimeout(5 * time.Second).ImpersonateChrome()
	require.NoError(t, applyReqClientProxy(client, proxyURL))

	transport := client.GetTransport()
	require.NotNil(t, transport.DialContext, "ss 场景必须注入自定义 DialContext")
	require.Nil(t, transport.Proxy, "ss 场景不得设置 Proxy（含 ProxyFromEnvironment），否则会退化为 CONNECT")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 假节点不会回应 ServerHello，请求必然失败；这里只关心隧道分层是否正确。
	_, err := client.R().SetContext(ctx).Get("https://upstream.example/whatever")
	require.Error(t, err)

	select {
	case target := <-targetAddrCh:
		require.Equal(t, "upstream.example:443", target, "ss 节点应收到真实目标地址，证明隧道已建立")
	case <-time.After(5 * time.Second):
		t.Fatal("ss 节点未收到目标地址，隧道未建立")
	}

	select {
	case first := <-firstBytesCh:
		require.NotEmpty(t, first)
		require.Equal(t, byte(0x16), first[0],
			"ss 隧道内层首字节应为 TLS ClientHello(0x16)；若为 'C'(0x43) 说明退化成了明文 CONNECT")
	case <-time.After(5 * time.Second):
		t.Fatal("未捕获到隧道内层字节")
	}
}

// TestApplyReqClientProxy_SS配置非法时报错而非静默直连 验证 fail-fast：
// 配置错误绝不能退化为"不设代理直连"，那会暴露服务器真实 IP。
func TestApplyReqClientProxy_SS配置非法时报错而非静默直连(t *testing.T) {
	cases := map[string]string{
		"缺少cipher与password": "ss://127.0.0.1:1234",
		"obfs缺少obfs-host":   "ss://chacha20-ietf-poly1305:pwd@127.0.0.1:1234?plugin=obfs&mode=tls",
		"不支持的cipher":        "ss://rc4-md5:pwd@127.0.0.1:1234",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			client := req.C()
			require.Error(t, applyReqClientProxy(client, raw))
		})
	}

	// 同样的错误必须从共享客户端池冒泡出来，而不是被吞掉后返回一个直连客户端
	_, err := getSharedReqClient(reqClientOptions{
		ProxyURL: "ss://127.0.0.1:1234",
		Timeout:  time.Second,
	})
	require.Error(t, err)
}

// TestApplyReqClientProxy_非ss协议保持SetProxyURL 守住回归：
// http/socks5 仍走 req 原生代理路径，不受 ss 改造影响。
func TestApplyReqClientProxy_非ss协议保持SetProxyURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8080", "socks5://127.0.0.1:1080"} {
		client := req.C()
		require.NoError(t, applyReqClientProxy(client, raw))
		transport := client.GetTransport()
		require.NotNil(t, transport.Proxy, "非 ss 协议应通过 Proxy 配置：%s", raw)
		require.Nil(t, transport.DialContext, "非 ss 协议不应注入自定义 DialContext：%s", raw)
	}
}

// TestApplyReqClientProxy_空代理表示直连
func TestApplyReqClientProxy_空代理表示直连(t *testing.T) {
	client := req.C()
	require.NoError(t, applyReqClientProxy(client, "   "))
	require.Nil(t, client.GetTransport().DialContext)
}

// TestApplyReqClientProxy_不支持的协议报错
func TestApplyReqClientProxy_不支持的协议报错(t *testing.T) {
	client := req.C()
	require.Error(t, applyReqClientProxy(client, "ftp://127.0.0.1:21"))
}
