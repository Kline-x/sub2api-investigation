package shadowsocks

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
)

// Dialer 通过 shadowsocks 节点建立 TCP 连接。
type Dialer struct {
	cfg    Config
	cipher core.Cipher
	base   net.Dialer
}

// NewDialer 构造 Dialer。参数非法时立即报错，不返回可用对象。
func NewDialer(cfg Config) (*Dialer, error) {
	if cfg.Server == "" {
		return nil, fmt.Errorf("shadowsocks: empty server address")
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("shadowsocks: empty password")
	}
	if cfg.ObfsMode != "" && cfg.ObfsMode != ObfsModeTLS {
		return nil, fmt.Errorf(
			"shadowsocks: unsupported obfs mode %q (supported: %s)", cfg.ObfsMode, ObfsModeTLS)
	}

	// PickCipher 内部会 ToUpper 并做别名映射，
	// 因此可直接传订阅里的原始写法（如 chacha20-ietf-poly1305）。
	ciph, err := core.PickCipher(cfg.Cipher, nil, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("shadowsocks: unsupported cipher %q: %w", cfg.Cipher, err)
	}

	return &Dialer{cfg: cfg, cipher: ciph}, nil
}

// DialContext 连接节点并返回一条已就绪的隧道连接，可直接交给 http.Transport。
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !strings.HasPrefix(network, "tcp") {
		return nil, fmt.Errorf("shadowsocks: unsupported network %q (only tcp)", network)
	}

	target := socks.ParseAddr(addr)
	if target == nil {
		return nil, fmt.Errorf("shadowsocks: invalid target address %q", addr)
	}

	conn, err := d.base.DialContext(ctx, "tcp", d.cfg.Server)
	if err != nil {
		return nil, fmt.Errorf("shadowsocks: dial node: %w", err)
	}

	if d.cfg.ObfsMode == ObfsModeTLS {
		conn = newTLSObfsConn(conn, d.cfg.ObfsHost)
	}

	conn = d.cipher.StreamConn(conn)

	if _, err := conn.Write(target); err != nil {
		conn.Close()
		return nil, fmt.Errorf("shadowsocks: write target address: %w", err)
	}

	return conn, nil
}
