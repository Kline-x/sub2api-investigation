package shadowsocks

import (
	"net/url"
	"strings"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestConfigFromURL_基本节点(t *testing.T) {
	cfg, err := ConfigFromURL(mustParse(t, "ss://chacha20-ietf-poly1305:pwd@node.example.com:443"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server != "node.example.com:443" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Cipher != "chacha20-ietf-poly1305" {
		t.Errorf("Cipher = %q", cfg.Cipher)
	}
	if cfg.Password != "pwd" {
		t.Errorf("Password = %q", cfg.Password)
	}
	if cfg.ObfsMode != "" {
		t.Errorf("ObfsMode 应为空, got %q", cfg.ObfsMode)
	}
}

func TestConfigFromURL_带obfs插件(t *testing.T) {
	cfg, err := ConfigFromURL(mustParse(t,
		"ss://chacha20-ietf-poly1305:pwd@n.example.com:443?plugin=obfs&mode=tls&obfs-host=bing.com"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ObfsMode != "tls" {
		t.Errorf("ObfsMode = %q, want tls", cfg.ObfsMode)
	}
	if cfg.ObfsHost != "bing.com" {
		t.Errorf("ObfsHost = %q, want bing.com", cfg.ObfsHost)
	}
}

func TestConfigFromURL_错误场景(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"缺少cipher", "ss://:pwd@h:443", "cipher"},
		{"缺少密码", "ss://chacha20-ietf-poly1305@h:443", "password"},
		{"缺少端口", "ss://chacha20-ietf-poly1305:pwd@h", "port"},
		{"不支持的插件", "ss://c:p@h:443?plugin=v2ray-plugin", "plugin"},
		{"不支持的obfs模式", "ss://c:p@h:443?plugin=obfs&mode=http", "mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigFromURL(mustParse(t, tc.raw))
			if err == nil {
				t.Fatal("期望报错，实际成功")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("错误信息 %q 未包含 %q", err.Error(), tc.wantErr)
			}
		})
	}
}
