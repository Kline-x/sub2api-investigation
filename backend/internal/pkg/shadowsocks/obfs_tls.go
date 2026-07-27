package shadowsocks

import "net"

// newTLSObfsConn 是 simple-obfs(tls) 封装的占位实现。
//
// 真实节点实测确认：不带 obfs 的裸 ss 连接会被服务端直接丢弃（EOF），
// 因此调用方（DialContext）必须保留这个封装点；这里先原样返回 conn
// 只是为了让本任务（Task 4：ss dialer 本体）可以独立编译和测试。
// 真正的 simple-obfs(tls) 握手封装由后续任务实现并替换本文件。
func newTLSObfsConn(conn net.Conn, host string) net.Conn {
	return conn
}
