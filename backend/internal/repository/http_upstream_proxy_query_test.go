package repository

import (
	"strings"
	"testing"
)

// normalizeProxyURL 曾经清空 RawQuery，导致 shadowsocks 节点的 obfs 插件参数
// （plugin/mode/obfs-host）在进入 transport 之前被抹掉，建出裸 ss 连接，
// 被要求 obfs 的服务端静默丢弃，表现为 EOF。这组用例锁住该行为。
func TestNormalizeProxyURL_保留ss插件参数(t *testing.T) {
	raw := "ss://chacha20-ietf-poly1305:pwd@node.example.com:2377?mode=tls&obfs-host=fake.example.net%3A249057&plugin=obfs"

	key, parsed, err := normalizeProxyURL(raw)
	if err != nil {
		t.Fatalf("normalizeProxyURL: %v", err)
	}
	if parsed == nil {
		t.Fatal("parsed 为 nil")
	}

	q := parsed.Query()
	if got := q.Get("plugin"); got != "obfs" {
		t.Errorf("plugin = %q, want obfs（拨号用的 URL 丢了插件参数）", got)
	}
	if got := q.Get("mode"); got != "tls" {
		t.Errorf("mode = %q, want tls", got)
	}
	if got := q.Get("obfs-host"); got != "fake.example.net:249057" {
		t.Errorf("obfs-host = %q, want fake.example.net:249057（冒号后缀不得被破坏）", got)
	}

	// 缓存键必须包含 query，否则 obfs 参数不同的节点会共用同一个连接池
	if !strings.Contains(key, "plugin=obfs") {
		t.Errorf("缓存键未包含插件参数，连接池会串用: %q", key)
	}
}

func TestNormalizeProxyURL_obfs参数不同则缓存键不同(t *testing.T) {
	a := "ss://c:p@node.example.com:2377?mode=tls&obfs-host=host-a.example.net&plugin=obfs"
	b := "ss://c:p@node.example.com:2377?mode=tls&obfs-host=host-b.example.net&plugin=obfs"

	keyA, _, err := normalizeProxyURL(a)
	if err != nil {
		t.Fatalf("normalizeProxyURL(a): %v", err)
	}
	keyB, _, err := normalizeProxyURL(b)
	if err != nil {
		t.Fatalf("normalizeProxyURL(b): %v", err)
	}
	if keyA == keyB {
		t.Fatalf("不同 obfs-host 产生了相同缓存键 %q，会导致连接池串用", keyA)
	}
}

func TestNormalizeProxyURL_仍去除路径与fragment(t *testing.T) {
	key, parsed, err := normalizeProxyURL("socks5://user:pass@1.2.3.4:1080/some/path#frag")
	if err != nil {
		t.Fatalf("normalizeProxyURL: %v", err)
	}
	if parsed.Path != "" {
		t.Errorf("Path 未被清除: %q", parsed.Path)
	}
	if parsed.Fragment != "" {
		t.Errorf("Fragment 未被清除: %q", parsed.Fragment)
	}
	if strings.Contains(key, "some/path") || strings.Contains(key, "frag") {
		t.Errorf("缓存键仍含路径或 fragment: %q", key)
	}
}
