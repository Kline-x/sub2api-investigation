package shadowsocks

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

const testObfsHost = "b1333d1.default.microsoft.fi:249057"

// doHandshake 完成首包写入（即 ClientHello 伪装），返回服务端侧收到的原始字节。
func doHandshake(t *testing.T, obfsConn net.Conn, server net.Conn, first []byte) []byte {
	t.Helper()
	helloCh := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 65536)
		n, err := server.Read(buf)
		if err != nil {
			helloCh <- nil
			return
		}
		helloCh <- append([]byte(nil), buf[:n]...)
	}()

	_ = obfsConn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := obfsConn.Write(first); err != nil {
		t.Fatalf("首包写入失败: %v", err)
	}
	hello := <-helloCh
	if hello == nil {
		t.Fatal("服务端未读到 ClientHello")
	}
	return hello
}

func TestTLSObfs_首包伪装成ClientHello并携带载荷(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	obfsConn := newTLSObfsConn(client, testObfsHost)
	payload := []byte("0123456789abcdef0123456789abcdef") // 32 字节，模拟 ss salt

	hello := doHandshake(t, obfsConn, server, payload)

	// 记录层：handshake(0x16) + 版本 0x0301 + 长度自洽
	if hello[0] != recordTypeHandshake || hello[1] != 0x03 || hello[2] != 0x01 {
		t.Fatalf("记录头 = % x, want 16 03 01", hello[:3])
	}
	recLen := int(binary.BigEndian.Uint16(hello[3:5]))
	if recLen != len(hello)-5 {
		t.Fatalf("记录长度字段 = %d, 实际握手体 = %d", recLen, len(hello)-5)
	}

	// 握手层：ClientHello(0x01) + 3 字节长度自洽 + client_version 0x0303
	if hello[5] != 0x01 {
		t.Fatalf("握手类型 = 0x%02x, want 0x01(ClientHello)", hello[5])
	}
	hsLen := int(hello[6])<<16 | int(hello[7])<<8 | int(hello[8])
	if hsLen != len(hello)-9 {
		t.Fatalf("握手长度字段 = %d, 实际 = %d", hsLen, len(hello)-9)
	}
	if hello[9] != 0x03 || hello[10] != 0x03 {
		t.Fatalf("client_version = % x, want 03 03", hello[9:11])
	}

	// 固定布局：session_id 32 字节、cipher_suites 56 字节，
	// 使 session ticket 扩展的载荷恒定落在第 142 字节。
	if hello[43] != 32 {
		t.Fatalf("session_id 长度 = %d, want 32", hello[43])
	}
	if got := binary.BigEndian.Uint16(hello[76:78]); got != 56 {
		t.Fatalf("cipher_suites 长度 = %d, want 56", got)
	}
	if hello[134] != 0x01 || hello[135] != 0x00 {
		t.Fatalf("compression_methods = % x, want 01 00", hello[134:136])
	}
	if got := binary.BigEndian.Uint16(hello[136:138]); int(got) != len(hello)-138 {
		t.Fatalf("扩展长度字段 = %d, 实际 = %d", got, len(hello)-138)
	}

	exts := parseExtensions(t, hello[138:])

	ticket, ok := exts[0x0023]
	if !ok {
		t.Fatal("缺少 session ticket 扩展(0x0023)")
	}
	if !bytes.Equal(ticket, payload) {
		t.Fatalf("session ticket 载荷 = %q, want %q", ticket, payload)
	}
	// 载荷偏移必须稳定在 142（138 + 扩展类型 2 + 扩展长度 2）
	if idx := bytes.Index(hello, payload); idx != 142 {
		t.Fatalf("载荷偏移 = %d, want 142", idx)
	}

	name, ok := exts[0x0000]
	if !ok {
		t.Fatal("缺少 server_name 扩展(0x0000)")
	}
	// ServerNameList: 2 字节列表长度 + 1 字节类型 + 2 字节名字长度 + 名字
	if len(name) < 5 {
		t.Fatalf("server_name 扩展过短: % x", name)
	}
	gotHost := string(name[5:])
	// obfs-host 里的冒号是主机名的一部分，绝不能被当端口切掉
	if gotHost != testObfsHost {
		t.Fatalf("SNI = %q, want %q", gotHost, testObfsHost)
	}
}

func parseExtensions(t *testing.T, b []byte) map[uint16][]byte {
	t.Helper()
	out := map[uint16][]byte{}
	for len(b) > 0 {
		if len(b) < 4 {
			t.Fatalf("扩展块尾部残留 % x", b)
		}
		typ := binary.BigEndian.Uint16(b[:2])
		n := int(binary.BigEndian.Uint16(b[2:4]))
		if len(b) < 4+n {
			t.Fatalf("扩展 0x%04x 声明长度 %d 超出剩余字节 %d", typ, n, len(b)-4)
		}
		out[typ] = b[4 : 4+n]
		b = b[4+n:]
	}
	return out
}

func TestTLSObfs_数据帧往返(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	obfsConn := newTLSObfsConn(client, testObfsHost)
	payload := []byte("hello shadowsocks")

	// 先把首包（ClientHello 伪装）走掉，后续写入才是 application data 记录
	doHandshake(t, obfsConn, server, []byte("salt"))

	done := make(chan error, 1)
	go func() {
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(server, hdr); err != nil {
			done <- fmt.Errorf("读记录头: %w", err)
			return
		}
		if hdr[0] != 0x17 || hdr[1] != 0x03 || hdr[2] != 0x03 {
			done <- fmt.Errorf("记录头 = % x, want 17 03 03", hdr[:3])
			return
		}
		length := int(binary.BigEndian.Uint16(hdr[3:5]))
		body := make([]byte, length)
		if _, err := io.ReadFull(server, body); err != nil {
			done <- fmt.Errorf("读记录体: %w", err)
			return
		}
		if !bytes.Equal(body, payload) {
			done <- fmt.Errorf("记录体 = %q, want %q", body, payload)
			return
		}
		done <- nil
	}()

	_ = obfsConn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	n, err := obfsConn.Write(payload)
	if err != nil {
		t.Fatalf("obfs write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write 返回 %d, want %d", n, len(payload))
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTLSObfs_大载荷分帧(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	obfsConn := newTLSObfsConn(client, testObfsHost)
	payload := bytes.Repeat([]byte("x"), maxTLSRecordPayload+1234)

	doHandshake(t, obfsConn, server, []byte("salt"))

	type result struct {
		lengths []int
		body    []byte
		err     error
	}
	resCh := make(chan result, 1)
	go func() {
		var res result
		remaining := len(payload)
		for remaining > 0 {
			hdr := make([]byte, 5)
			if _, err := io.ReadFull(server, hdr); err != nil {
				res.err = err
				resCh <- res
				return
			}
			if hdr[0] != 0x17 {
				res.err = fmt.Errorf("记录类型 = 0x%02x, want 0x17", hdr[0])
				resCh <- res
				return
			}
			length := int(binary.BigEndian.Uint16(hdr[3:5]))
			if length > maxTLSRecordPayload {
				res.err = fmt.Errorf("记录长度 %d 超过上限 %d", length, maxTLSRecordPayload)
				resCh <- res
				return
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(server, body); err != nil {
				res.err = err
				resCh <- res
				return
			}
			res.lengths = append(res.lengths, length)
			res.body = append(res.body, body...)
			remaining -= length
		}
		resCh <- res
	}()

	_ = obfsConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	n, err := obfsConn.Write(payload)
	if err != nil {
		t.Fatalf("obfs write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write 返回 %d, want %d", n, len(payload))
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("服务端侧: %v", res.err)
	}
	if len(res.lengths) != 2 {
		t.Fatalf("应拆成 2 帧，实际 %d 帧: %v", len(res.lengths), res.lengths)
	}
	if res.lengths[0] != maxTLSRecordPayload || res.lengths[1] != 1234 {
		t.Fatalf("分帧长度 = %v, want [%d 1234]", res.lengths, maxTLSRecordPayload)
	}
	if !bytes.Equal(res.body, payload) {
		t.Fatal("分帧重组后的内容与原载荷不一致")
	}
}

// 下面的字节取自真实节点 b1333d1.t8.glados-config.net:2377 的抓包（2026-07-27）。
// 关键点：ChangeCipherSpec 之后的第一条记录虽然仍标着 0x16，但载荷已经是真实数据。
var realServerHelloRecord = append([]byte{0x16, 0x03, 0x01, 0x00, 0x5b}, []byte{
	0x02, 0x00, 0x00, 0x57, 0x03, 0x03, 0x6a, 0x67, 0x18, 0x18, 0xc6, 0x41, 0x03, 0x98, 0x8e, 0x90,
	0x1d, 0x76, 0xdd, 0x4c, 0x28, 0xcf, 0xf4, 0x63, 0xf3, 0x0d, 0xf6, 0xa5, 0xc0, 0x6b, 0xff, 0x50,
	0x17, 0x14, 0x36, 0x32, 0x21, 0xa3, 0x20, 0x7c, 0xa3, 0xc9, 0x19, 0x5b, 0x74, 0x61, 0xc2, 0x11,
	0x7d, 0x69, 0xb0, 0x05, 0x9d, 0xff, 0x8a, 0xdc, 0xb4, 0x4a, 0x71, 0x6c, 0xfb, 0xc2, 0x8a, 0x2c,
	0xd2, 0x50, 0x43, 0xab, 0x33, 0xb3, 0xac, 0xcc, 0xa8, 0x00, 0x00, 0x0f, 0xff, 0x01, 0x00, 0x01,
	0x00, 0x00, 0x17, 0x00, 0x00, 0x00, 0x0b, 0x00, 0x02, 0x01, 0x00,
}...)

var realChangeCipherSpecRecord = []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01}

func TestTLSObfs_跳过服务端伪造握手后读出真实载荷(t *testing.T) {
	// 实测的握手响应总长度：ServerHello 96 字节 + ChangeCipherSpec 6 字节 = 102
	if got := len(realServerHelloRecord) + len(realChangeCipherSpecRecord); got != 102 {
		t.Fatalf("抓包 fixture 长度 = %d, want 102", got)
	}

	salt := bytes.Repeat([]byte{0xAB}, 32) // CCS 之后第一条记录（伪装成 0x16）里的 ss salt
	rest := []byte("real shadowsocks payload")

	var stream []byte
	stream = append(stream, realServerHelloRecord...)
	stream = append(stream, realChangeCipherSpecRecord...)
	// 伪装成 handshake 的数据记录 —— 载荷必须被交给上层，不能当握手丢弃
	stream = append(stream, 0x16, 0x03, 0x03, 0x00, byte(len(salt)))
	stream = append(stream, salt...)
	// 常规 application data 记录
	stream = append(stream, 0x17, 0x03, 0x03, 0x00, byte(len(rest)))
	stream = append(stream, rest...)

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_, _ = server.Write(stream)
		server.Close()
	}()

	obfsConn := newTLSObfsConn(client, testObfsHost)
	_ = obfsConn.SetReadDeadline(time.Now().Add(3 * time.Second))

	want := append(append([]byte(nil), salt...), rest...)
	got := make([]byte, len(want))
	if _, err := io.ReadFull(obfsConn, got); err != nil {
		t.Fatalf("读取真实载荷失败: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("读出的载荷 = % x, want % x", got, want)
	}
}

func TestTLSObfs_读取跨记录边界的分段载荷(t *testing.T) {
	// 一条 300 字节记录，上层用 128 字节的小缓冲分多次读，
	// 验证 remain 记账正确、不会把下一条记录头当成载荷。
	big := bytes.Repeat([]byte("ab"), 150)
	tail := []byte("tail")

	var stream []byte
	stream = append(stream, realServerHelloRecord...)
	stream = append(stream, realChangeCipherSpecRecord...)
	stream = append(stream, 0x17, 0x03, 0x03, byte(len(big)>>8), byte(len(big)))
	stream = append(stream, big...)
	stream = append(stream, 0x17, 0x03, 0x03, 0x00, byte(len(tail)))
	stream = append(stream, tail...)

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_, _ = server.Write(stream)
		server.Close()
	}()

	obfsConn := newTLSObfsConn(client, testObfsHost)
	_ = obfsConn.SetReadDeadline(time.Now().Add(3 * time.Second))

	var got []byte
	buf := make([]byte, 128)
	for len(got) < len(big)+len(tail) {
		n, err := obfsConn.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			t.Fatalf("读到 %d 字节后出错: %v", len(got), err)
		}
	}
	want := append(append([]byte(nil), big...), tail...)
	if !bytes.Equal(got, want) {
		t.Fatalf("读出内容与预期不符（长度 %d vs %d）", len(got), len(want))
	}
}

func TestTLSObfs_握手响应异常时报错(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		// 不是握手记录，直接给一条 alert
		_, _ = server.Write([]byte{0x15, 0x03, 0x03, 0x00, 0x02, 0x02, 0x28})
		server.Close()
	}()

	obfsConn := newTLSObfsConn(client, testObfsHost)
	_ = obfsConn.SetReadDeadline(time.Now().Add(3 * time.Second))

	buf := make([]byte, 64)
	if _, err := obfsConn.Read(buf); err == nil {
		t.Fatal("服务端返回 alert 记录时应报错，实际成功返回")
	}
}
