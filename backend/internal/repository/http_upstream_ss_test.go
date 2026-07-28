package repository

import (
	"context"
	"io"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/shadowaead"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

const (
	testSSCipher   = "chacha20-ietf-poly1305"
	testSSPassword = "test-password"
)

// startFakeSSNode 启动一个最小化的 ss 节点：用真实的 go-shadowsocks2 AEAD 解密隧道、
// 读出客户端请求的目标地址，然后只截获隧道内的下一段字节（不做真实转发）。
// 这足以验证：ss 隧道确实建立、且隧道内层承载的是 TLS ClientHello（0x16），
// 即 TLS 指纹握手发生在 ss 隧道之上而不是之下。
//
// 注意：这里刻意不用 core.Cipher.StreamConn()（即 shadowsocks.Dialer 内部的用法），
// 而是绕过一层直接用 shadowaead.Cipher.Decrypter() + shadowaead.NewReader()。
// 原因：go-shadowsocks2 的 StreamConn 在 initReader/initWriter 里会读写一个
// **进程级全局** 的 salt 防重放缓存（internal.AddSalt/CheckSalt）。本测试的"客户端"
// （生产代码里真正的 shadowsocks.Dialer）和"节点"跑在同一个测试进程里，共享同一个
// 全局缓存：客户端 initWriter() 一 AddSalt，节点侧 initReader() 就会把同一个 salt
// 判成"重放"而报错——这是测试进程内自我碰撞的假阳性，并非生产代码的 bug。绕开
// initReader 直接用 Decrypter+NewReader 就不会碰这个全局缓存。
func startFakeSSNode(t *testing.T) (addr string, firstBytesCh chan []byte, targetAddrCh chan string) {
	t.Helper()
	ciph, err := core.PickCipher(testSSCipher, nil, testSSPassword)
	require.NoError(t, err)
	saltCipher, ok := ciph.(shadowaead.Cipher)
	require.True(t, ok, "cipher 未实现 shadowaead.Cipher")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	firstBytesCh = make(chan []byte, 1)
	targetAddrCh = make(chan string, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		salt := make([]byte, saltCipher.SaltSize())
		if _, err := io.ReadFull(conn, salt); err != nil {
			targetAddrCh <- "ERROR: read salt: " + err.Error()
			return
		}
		aead, err := saltCipher.Decrypter(salt)
		if err != nil {
			targetAddrCh <- "ERROR: decrypter: " + err.Error()
			return
		}
		sreader := shadowaead.NewReader(conn, aead)

		target, err := socks.ReadAddr(sreader)
		if err != nil {
			targetAddrCh <- "ERROR: " + err.Error()
			return
		}
		targetAddrCh <- target.String()

		buf := make([]byte, 5)
		n, _ := io.ReadFull(sreader, buf)
		firstBytesCh <- append([]byte(nil), buf[:n]...)
	}()

	return listener.Addr().String(), firstBytesCh, targetAddrCh
}

// TestBuildUpstreamTransportWithTLSFingerprint_SS隧道之上进行TLS指纹握手 验证 ss 代理场景下
// buildUpstreamTransportWithTLSFingerprint 正确分派：不再落入"未知协议回退到无指纹直连"分支，
// 而是把 shadowsocks 隧道作为 baseDialer 注入通用的 tlsfingerprint.NewDialer，
// 使 TLS 指纹握手在 ss 隧道之上进行。
func TestBuildUpstreamTransportWithTLSFingerprint_SS隧道之上进行TLS指纹握手(t *testing.T) {
	addr, firstBytesCh, targetAddrCh := startFakeSSNode(t)

	proxyURL := &url.URL{
		Scheme: "ss",
		User:   url.UserPassword(testSSCipher, testSSPassword),
		Host:   addr,
	}

	transport, err := buildUpstreamTransportWithTLSFingerprint(poolSettings{}, proxyURL, &tlsfingerprint.Profile{Name: "test"})
	require.NoError(t, err)
	require.NotNil(t, transport.DialTLSContext, "ss 场景应通过 DialTLSContext 启用 TLS 指纹，而不是回退到普通 DialContext")
	require.Nil(t, transport.DialContext, "ss 场景不应回退到未开启指纹的普通代理拨号")
	require.Nil(t, transport.Proxy)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, dialErr := transport.DialTLSContext(ctx, "tcp", "upstream.example:443")
	// 假节点不会回应 ServerHello，握手必然失败；这里只关心隧道分层是否正确。
	require.Error(t, dialErr)

	select {
	case target := <-targetAddrCh:
		require.Equal(t, "upstream.example:443", target, "ss 节点应收到真实目标地址，证明隧道已建立")
	case <-time.After(5 * time.Second):
		t.Fatal("ss 节点未收到目标地址，隧道未建立")
	}

	select {
	case first := <-firstBytesCh:
		require.NotEmpty(t, first)
		require.Equal(t, byte(0x16), first[0], "ss 隧道内层首字节应为 TLS ClientHello(0x16)，说明握手发生在隧道之上而非之下")
	case <-time.After(5 * time.Second):
		t.Fatal("未捕获到隧道内层字节，无法验证 TLS 握手是否发生在隧道之上")
	}
}

// TestBuildUpstreamTransportWithTLSFingerprint_SS配置非法时报错而非静默直连 验证
// shadowsocks 包 fail-fast 的设计约束在 TLS 指纹路径上同样生效：配置非法时必须
// 直接返回 error，绝不能退化为直连或忽略代理。
func TestBuildUpstreamTransportWithTLSFingerprint_SS配置非法时报错而非静默直连(t *testing.T) {
	proxyURL := &url.URL{
		Scheme: "ss",
		Host:   "127.0.0.1:1", // 缺少 userinfo（cipher/password）
	}

	transport, err := buildUpstreamTransportWithTLSFingerprint(poolSettings{}, proxyURL, &tlsfingerprint.Profile{Name: "test"})
	require.Error(t, err)
	require.Nil(t, transport)
}
