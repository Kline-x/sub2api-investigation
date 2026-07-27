package shadowsocks

import (
	"context"
	"strings"
	"testing"
)

func TestNewDialer_不支持的cipher报错(t *testing.T) {
	_, err := NewDialer(Config{
		Server: "h:443", Cipher: "rc4-md5", Password: "p",
	})
	if err == nil {
		t.Fatal("期望报错，实际成功")
	}
	if !strings.Contains(err.Error(), "cipher") {
		t.Errorf("错误信息应提到 cipher: %v", err)
	}
}

func TestNewDialer_合法配置(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("dialer 为 nil")
	}
}

// 保护性分支：调用方可能绕过 ConfigFromURL 直接构造 Config。
// ObfsHost 为空会让 makeClientHello 生成畸形 SNI，服务端多半静默拒连，
// 症状极难诊断，因此必须在构造入口 fail-fast。
func TestNewDialer_obfs为tls但ObfsHost为空时报错(t *testing.T) {
	_, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
		ObfsMode: ObfsModeTLS, ObfsHost: "",
	})
	if err == nil {
		t.Fatal("期望报错，实际成功")
	}
	if !strings.Contains(err.Error(), "ObfsHost") {
		t.Errorf("错误信息应提到 ObfsHost: %v", err)
	}
}

func TestNewDialer_obfs为tls且ObfsHost非空时通过(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
		ObfsMode: ObfsModeTLS, ObfsHost: "example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("dialer 为 nil")
	}
}

func TestNewDialer_不支持的obfs模式报错(t *testing.T) {
	_, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
		ObfsMode: "http", ObfsHost: "example.com",
	})
	if err == nil {
		t.Fatal("期望报错，实际成功")
	}
	if !strings.Contains(err.Error(), "obfs mode") {
		t.Errorf("错误信息应提到 obfs mode: %v", err)
	}
}

func TestDialContext_拒绝非tcp网络(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	_, err = d.DialContext(context.Background(), "udp", "example.com:443")
	if err == nil {
		t.Fatal("期望拒绝 udp，实际成功")
	}
	if !strings.Contains(err.Error(), "network") {
		t.Errorf("错误信息应提到 network: %v", err)
	}
}

func TestDialContext_拒绝非法目标地址(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	_, err = d.DialContext(context.Background(), "tcp", "没有端口的地址")
	if err == nil {
		t.Fatal("期望拒绝非法地址，实际成功")
	}
}
