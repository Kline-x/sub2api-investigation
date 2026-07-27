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
