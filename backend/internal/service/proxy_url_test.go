package service

import "testing"

func TestProxyURL_无Extra时不带query(t *testing.T) {
	p := &Proxy{Protocol: "socks5", Host: "1.2.3.4", Port: 1080, Username: "u", Password: "p"}
	got := p.URL()
	want := "socks5://u:p@1.2.3.4:1080"
	if got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestProxyURL_ss节点带Extra(t *testing.T) {
	p := &Proxy{
		Protocol: "ss",
		Host:     "node.example.com",
		Port:     443,
		Username: "chacha20-ietf-poly1305",
		Password: "secret",
		Extra:    map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "bing.com"},
	}
	got := p.URL()
	want := "ss://chacha20-ietf-poly1305:secret@node.example.com:443?mode=tls&obfs-host=bing.com&plugin=obfs"
	if got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestProxyURL_query顺序稳定(t *testing.T) {
	p := &Proxy{
		Protocol: "ss", Host: "h", Port: 1, Username: "c", Password: "p",
		Extra: map[string]string{"z": "1", "a": "2", "m": "3"},
	}
	first := p.URL()
	for i := 0; i < 50; i++ {
		if got := p.URL(); got != first {
			t.Fatalf("第 %d 次调用结果不同: %q vs %q", i, got, first)
		}
	}
}
