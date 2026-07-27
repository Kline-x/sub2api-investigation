package shadowsocks

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// simple-obfs(tls) 伪装层。
//
// 该模式把 shadowsocks 的字节流塞进 TLS 1.2 的记录层里：首个请求伪装成
// ClientHello（载荷藏在 session ticket 扩展中），之后的每次写入都封装成
// application data 记录。所有格式均按 RFC 5246（TLS 1.2）的公开定义构造，
// 未引用任何 GPL 实现的源码。
//
// 服务端的响应结构由真实机场节点实测确认（见 Read 上方注释）。

const (
	// maxTLSRecordPayload 是单条 TLS 记录的最大载荷（RFC 5246 §6.2.1）。
	maxTLSRecordPayload = 16384

	recordTypeChangeCipherSpec = 0x14
	recordTypeAlert            = 0x15
	recordTypeHandshake        = 0x16
	recordTypeApplicationData  = 0x17

	// clientHelloFixedOverhead 是 ClientHello 里除 session ticket 载荷和 SNI 主机名
	// 之外的固定字节数（记录头 5 + 握手头 4 + 版本 2 + random 32 + session_id 33 +
	// cipher_suites 58 + compression 2 + 扩展长度字段 2 + 各扩展头部与固定内容）。
	// 只用于给首包载荷设一个安全上限，精确值不必与实际完全一致，偏大即可。
	clientHelloFixedOverhead = 512
)

// maxClientHelloPayload 是能塞进首个 ClientHello 的最大载荷。
// 超出部分会退回普通 application data 记录发送。
const maxClientHelloPayload = maxTLSRecordPayload - clientHelloFixedOverhead

type tlsObfsConn struct {
	net.Conn
	host          string
	firstRequest  bool
	firstResponse bool
	remain        int // 当前记录尚未读完的载荷字节数
}

func newTLSObfsConn(conn net.Conn, host string) net.Conn {
	return &tlsObfsConn{
		Conn:          conn,
		host:          host,
		firstRequest:  true,
		firstResponse: true,
	}
}

func (c *tlsObfsConn) Write(b []byte) (int, error) {
	written := 0
	if c.firstRequest {
		c.firstRequest = false

		embed := b
		if len(embed) > maxClientHelloPayload {
			embed = embed[:maxClientHelloPayload]
		}
		hello, err := makeClientHello(embed, c.host)
		if err != nil {
			return 0, err
		}
		if _, err := c.Conn.Write(hello); err != nil {
			return 0, err
		}
		written = len(embed)
		b = b[len(embed):]
	}

	for len(b) > 0 {
		chunk := b
		if len(chunk) > maxTLSRecordPayload {
			chunk = chunk[:maxTLSRecordPayload]
		}
		record := make([]byte, 5+len(chunk))
		record[0] = recordTypeApplicationData
		record[1], record[2] = 0x03, 0x03 // TLS 1.2
		binary.BigEndian.PutUint16(record[3:5], uint16(len(chunk)))
		copy(record[5:], chunk)
		if _, err := c.Conn.Write(record); err != nil {
			return written, err
		}
		written += len(chunk)
		b = b[len(chunk):]
	}
	return written, nil
}

// Read 剥掉 TLS 记录层伪装，把真实载荷交给上层。
//
// 首次读取前必须跳过服务端伪造的握手响应。实测节点
// b1333d1.t8.glados-config.net:2377（2026-07-27）返回的原始字节为：
//
//	16 03 01 00 5b  02 00 00 57 03 03 ...  ServerHello 记录（5 + 91 = 96 字节）
//	14 03 03 00 01  01                     ChangeCipherSpec 记录（5 + 1 = 6 字节）
//	16 03 03 00 20  <32 字节>              ← 这条已经是真实数据（ss salt）
//	17 03 03 05 66  <1382 字节>            后续数据
//
// 因此需要跳过的握手字节数实测为 96 + 6 = 102。关键观察是：**CCS 之后的第一条
// 记录虽然仍标着 0x16（伪装成 Finished），但它的载荷已经是真实数据**——那 32 字节
// 正好是 chacha20-ietf-poly1305 的 salt 长度，把它当握手丢掉会导致内层
// "message authentication failed"（本实现迭代中已实测踩到）。
//
// 由于 ServerHello 的长度取决于服务端伪造的扩展内容，写死 102 不可靠；
// 这里改为按记录边界跳过——丢弃记录直到丢完第一条 ChangeCipherSpec，
// 之后所有记录一律按「5 字节头 + 载荷」解包，不再看记录类型。
func (c *tlsObfsConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	// 上一条记录还有剩余载荷，先读完
	if c.remain > 0 {
		n := c.remain
		if n > len(b) {
			n = len(b)
		}
		read, err := c.Conn.Read(b[:n])
		c.remain -= read
		return read, err
	}

	if c.firstResponse {
		c.firstResponse = false
		if err := c.skipHandshakeResponse(); err != nil {
			return 0, err
		}
	}

	for {
		hdr, err := c.readRecordHeader()
		if err != nil {
			return 0, err
		}
		length := int(binary.BigEndian.Uint16(hdr[3:5]))
		if length > maxTLSRecordPayload {
			return 0, fmt.Errorf("shadowsocks: oversized TLS record %d", length)
		}
		if length == 0 {
			continue // 空记录，继续读下一条，避免返回 (0, nil) 造成空转
		}

		n := length
		if n > len(b) {
			n = len(b)
		}
		read, err := c.Conn.Read(b[:n])
		c.remain = length - read
		return read, err
	}
}

// skipHandshakeResponse 丢弃服务端伪造的握手响应：ServerHello 记录（0x16）
// 加上紧随其后的 ChangeCipherSpec 记录（0x14）。丢完 CCS 即返回，
// 因为 CCS 之后的第一条记录已经携带真实载荷。
func (c *tlsObfsConn) skipHandshakeResponse() error {
	for {
		hdr, err := c.readRecordHeader()
		if err != nil {
			return fmt.Errorf("shadowsocks: read obfs handshake record: %w", err)
		}
		length := int64(binary.BigEndian.Uint16(hdr[3:5]))
		switch hdr[0] {
		case recordTypeHandshake, recordTypeChangeCipherSpec:
			if _, err := io.CopyN(io.Discard, c.Conn, length); err != nil {
				return fmt.Errorf("shadowsocks: skip obfs handshake record: %w", err)
			}
			if hdr[0] == recordTypeChangeCipherSpec {
				return nil
			}
		case recordTypeAlert:
			return fmt.Errorf("shadowsocks: obfs server sent TLS alert during handshake")
		default:
			return fmt.Errorf(
				"shadowsocks: unexpected TLS record type 0x%02x in obfs handshake", hdr[0])
		}
	}
}

func (c *tlsObfsConn) readRecordHeader() ([]byte, error) {
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(c.Conn, hdr); err != nil {
		return nil, err
	}
	return hdr, nil
}

// makeClientHello 构造一个 TLS 1.2 ClientHello 记录，SNI 填 host，
// payload 放进 session ticket 扩展（RFC 5077 定义的扩展类型 0x0023）。
//
// 字段布局是对真实机场节点抓包实测反推得到的固定形态，使得 payload 恒定
// 落在记录起始后的第 142 字节：
//
//	0   16 03 01 LL LL              记录头（handshake，版本写 TLS 1.0，与真实客户端一致）
//	5   01 LL LL LL                 ClientHello 握手头
//	9   03 03                       client_version = TLS 1.2
//	11  <32B random>                前 4 字节为大端 unix 时间戳
//	43  20 <32B session_id>
//	76  00 38 <56B cipher_suites>   28 个套件
//	134 01 00                       compression_methods = {null}
//	136 LL LL                       extensions 长度
//	138 00 23 LL LL <payload>       session ticket 扩展 —— 载荷在此
//	    00 00 ...                   server_name(SNI) = host
//	    00 17 / 00 16               extended_master_secret / encrypt_then_mac
//	    00 0b / 00 0a / 00 0d ...   ec_point_formats / supported_groups / signature_algorithms
//
// host 原样写入 SNI（包括机场下发的形如 "x.default.microsoft.fi:249057" 这种
// 带冒号的字符串——服务端按原字符串比对，不能当端口切开）。
func makeClientHello(payload []byte, host string) ([]byte, error) {
	if len(payload) > maxClientHelloPayload {
		return nil, fmt.Errorf("shadowsocks: obfs first payload too large (%d bytes)", len(payload))
	}
	if host == "" {
		return nil, fmt.Errorf("shadowsocks: obfs host is empty")
	}
	// host 最终会连同 SNI 扩展头（5 字节）、session ticket 载荷及其他固定
	// 扩展一起塞进同一条 TLS 记录，记录长度字段只有 2 字节（RFC 5246 §6.2.1）。
	// 这里贴着 maxTLSRecordPayload 卡一个宽松但正确的上限，避免像
	// 0xFFFF-5 那样的上界在 payload/其他扩展占用空间后仍能让记录总长溢出
	// 2 字节字段而不报错。
	if len(host) > maxTLSRecordPayload {
		return nil, fmt.Errorf("shadowsocks: obfs host too long (%d bytes)", len(host))
	}

	random := make([]byte, 32)
	if _, err := rand.Read(random[4:]); err != nil {
		return nil, fmt.Errorf("shadowsocks: obfs random: %w", err)
	}
	binary.BigEndian.PutUint32(random[:4], uint32(time.Now().Unix()))

	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		return nil, fmt.Errorf("shadowsocks: obfs session id: %w", err)
	}

	ext := make([]byte, 0, 128+len(payload)+len(host))
	ext = appendExtension(ext, 0x0023, payload)   // session_ticket，携带真实载荷
	ext = appendExtension(ext, 0x0000, sni(host)) // server_name
	ext = appendExtension(ext, 0x0017, nil)       // extended_master_secret
	ext = appendExtension(ext, 0x0016, nil)       // encrypt_then_mac
	// ec_point_formats: uncompressed / ansiX962_compressed_prime / ansiX962_compressed_char2
	ext = appendExtension(ext, 0x000b, []byte{0x03, 0x01, 0x00, 0x02})
	// supported_groups: x25519, secp256r1, secp384r1, secp521r1
	ext = appendExtension(ext, 0x000a, []byte{
		0x00, 0x08, 0x00, 0x1d, 0x00, 0x17, 0x00, 0x18, 0x00, 0x19,
	})
	// signature_algorithms
	ext = appendExtension(ext, 0x000d, []byte{
		0x00, 0x14,
		0x06, 0x01, 0x06, 0x03, 0x05, 0x01, 0x05, 0x03,
		0x04, 0x01, 0x04, 0x03, 0x03, 0x01, 0x03, 0x03,
		0x02, 0x01, 0x02, 0x03,
	})

	body := make([]byte, 0, 134+len(ext))
	body = append(body, 0x03, 0x03) // client_version = TLS 1.2
	body = append(body, random...)
	body = append(body, byte(len(sessionID)))
	body = append(body, sessionID...)
	body = append(body, byte(len(tlsCipherSuites)>>8), byte(len(tlsCipherSuites)))
	body = append(body, tlsCipherSuites...)
	body = append(body, 0x01, 0x00) // compression_methods = {null}
	body = append(body, byte(len(ext)>>8), byte(len(ext)))
	body = append(body, ext...)

	handshake := make([]byte, 0, 4+len(body))
	handshake = append(handshake, 0x01) // ClientHello
	handshake = append(handshake, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	handshake = append(handshake, body...)

	record := make([]byte, 0, 5+len(handshake))
	record = append(record, recordTypeHandshake, 0x03, 0x01)
	record = append(record, byte(len(handshake)>>8), byte(len(handshake)))
	record = append(record, handshake...)
	return record, nil
}

// appendExtension 追加一个 TLS 扩展：2 字节类型 + 2 字节长度 + 内容。
func appendExtension(dst []byte, extType uint16, data []byte) []byte {
	dst = append(dst, byte(extType>>8), byte(extType))
	dst = append(dst, byte(len(data)>>8), byte(len(data)))
	return append(dst, data...)
}

// sni 构造 server_name 扩展的内容：ServerNameList（RFC 6066 §3）。
func sni(host string) []byte {
	out := make([]byte, 0, 5+len(host))
	out = append(out, byte((len(host)+3)>>8), byte(len(host)+3)) // server_name_list 长度
	out = append(out, 0x00)                                      // name_type = host_name
	out = append(out, byte(len(host)>>8), byte(len(host)))
	return append(out, host...)
}

// tlsCipherSuites 是 ClientHello 里声明的 28 个套件（56 字节），
// 取自常见浏览器的 TLS 1.2 套件列表，只为让指纹看起来正常；
// 由于握手是伪造的，这些套件永远不会真正被协商。
var tlsCipherSuites = []byte{
	0xc0, 0x2c, 0xc0, 0x30, 0x00, 0x9f, 0xcc, 0xa9,
	0xcc, 0xa8, 0xcc, 0xaa, 0xc0, 0x2b, 0xc0, 0x2f,
	0x00, 0x9e, 0xc0, 0x24, 0xc0, 0x28, 0x00, 0x6b,
	0xc0, 0x23, 0xc0, 0x27, 0x00, 0x67, 0xc0, 0x0a,
	0xc0, 0x14, 0x00, 0x39, 0xc0, 0x09, 0xc0, 0x13,
	0x00, 0x33, 0x00, 0x9d, 0x00, 0x9c, 0x00, 0x3d,
	0x00, 0x3c, 0x00, 0x35, 0x00, 0x2f, 0x00, 0xff,
}
