// Package shadowsocks 提供 shadowsocks 出站拨号能力。
//
// 设计约束：所有参数错误在构造 Dialer 时立即报错（fail-fast），
// 绝不返回一个"退化为直连"的 dialer——静默直连会产生 IP 关联风险。
package shadowsocks

import (
	"fmt"
	"net/url"
	"strings"
)

// ObfsModeTLS 是当前唯一支持的 simple-obfs 模式。
const ObfsModeTLS = "tls"

// Config 描述一个 shadowsocks 节点。
type Config struct {
	Server   string // host:port
	Cipher   string // 如 chacha20-ietf-poly1305
	Password string
	ObfsMode string // "" 表示不启用 obfs；当前仅支持 "tls"
	ObfsHost string // obfs 伪装的 SNI/Host
}

// ConfigFromURL 从代理 URL 解析节点参数。
//
// URL 形如：
//
//	ss://cipher:password@host:port?plugin=obfs&mode=tls&obfs-host=example.com
//
// cipher 占据 userinfo 的 username 位，这与 shadowsocks 分享链接的标准格式一致。
func ConfigFromURL(u *url.URL) (Config, error) {
	if u == nil {
		return Config{}, fmt.Errorf("shadowsocks: nil proxy URL")
	}

	host := u.Hostname()
	if host == "" {
		return Config{}, fmt.Errorf("shadowsocks: proxy URL missing host")
	}
	port := u.Port()
	if port == "" {
		return Config{}, fmt.Errorf("shadowsocks: proxy URL missing port")
	}

	if u.User == nil {
		return Config{}, fmt.Errorf("shadowsocks: proxy URL missing cipher and password")
	}
	cipher := u.User.Username()
	if cipher == "" {
		return Config{}, fmt.Errorf("shadowsocks: proxy URL missing cipher")
	}
	password, ok := u.User.Password()
	if !ok || password == "" {
		return Config{}, fmt.Errorf("shadowsocks: proxy URL missing password")
	}

	cfg := Config{
		Server:   u.Host,
		Cipher:   cipher,
		Password: password,
	}

	q := u.Query()
	switch plugin := strings.TrimSpace(q.Get("plugin")); plugin {
	case "":
		// 无插件
	case "obfs":
		mode := strings.TrimSpace(q.Get("mode"))
		if mode != ObfsModeTLS {
			return Config{}, fmt.Errorf(
				"shadowsocks: unsupported obfs mode %q (supported: %s)", mode, ObfsModeTLS)
		}
		cfg.ObfsMode = mode
		cfg.ObfsHost = strings.TrimSpace(q.Get("obfs-host"))
		if cfg.ObfsHost == "" {
			return Config{}, fmt.Errorf("shadowsocks: obfs enabled but obfs-host is empty")
		}
	default:
		return Config{}, fmt.Errorf(
			"shadowsocks: unsupported plugin %q (supported: obfs)", plugin)
	}

	return cfg, nil
}
