package tlsfingerprint

import (
	"context"
	"net"
	"testing"
)

// ss 场景下，proxyURL 分派逻辑在 backend/internal/repository/http_upstream.go
// 的 buildUpstreamTransportWithTLSFingerprint 中完成（而非本包内）。
// 本测试只验证被复用的通用构造 NewDialer(profile, baseDialer) 确实会调用传入的
// baseDialer——这是 ss 隧道能够注入到 TLS 指纹握手之下的前提。
func TestNewDialer_接受自定义baseDialer(t *testing.T) {
	called := false
	base := func(ctx context.Context, network, addr string) (net.Conn, error) {
		called = true
		return nil, context.Canceled
	}

	d := NewDialer(nil, base)
	if d == nil {
		t.Fatal("NewDialer 返回 nil")
	}

	_, _ = d.DialTLSContext(context.Background(), "tcp", "example.com:443")
	if !called {
		t.Error("baseDialer 未被调用——ss 场景依赖它注入 ss 隧道")
	}
}
