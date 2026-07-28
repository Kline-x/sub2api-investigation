package clashsub

import "testing"

const sample = `
proxies:
  - name: "香港 01"
    type: ss
    server: hk1.example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: pwd1
    plugin: obfs
    plugin-opts:
      mode: tls
      host: bing.com
  - name: "日本 01"
    type: ss
    server: jp1.example.com
    port: 8443
    cipher: aes-128-gcm
    password: pwd2
  - name: "vmess 节点"
    type: vmess
    server: v.example.com
    port: 443
    uuid: xxxx
  - name: "http obfs 节点"
    type: ss
    server: h.example.com
    port: 443
    cipher: aes-128-gcm
    password: pwd3
    plugin: obfs
    plugin-opts:
      mode: http
      host: bing.com
`

func TestParse_提取ss节点(t *testing.T) {
	nodes, skipped, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes 数量 = %d, want 2: %+v", len(nodes), nodes)
	}

	n := nodes[0]
	if n.Name != "香港 01" || n.Server != "hk1.example.com" || n.Port != 443 {
		t.Errorf("节点0 基本字段不符: %+v", n)
	}
	if n.Cipher != "chacha20-ietf-poly1305" || n.Password != "pwd1" {
		t.Errorf("节点0 加密字段不符: %+v", n)
	}
	if n.Plugin != "obfs" || n.ObfsMode != "tls" || n.ObfsHost != "bing.com" {
		t.Errorf("节点0 插件字段不符: %+v", n)
	}

	if nodes[1].Plugin != "" {
		t.Errorf("节点1 不应有插件: %+v", nodes[1])
	}

	if len(skipped) != 2 {
		t.Fatalf("skipped 数量 = %d, want 2: %+v", len(skipped), skipped)
	}
}

func TestParse_跳过原因可读(t *testing.T) {
	_, skipped, err := Parse([]byte(sample))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	byName := map[string]string{}
	for _, s := range skipped {
		byName[s.Name] = s.Reason
	}
	if byName["vmess 节点"] == "" {
		t.Error("vmess 节点应给出跳过原因")
	}
	if byName["http obfs 节点"] == "" {
		t.Error("http obfs 节点应给出跳过原因")
	}
}

func TestParse_非法YAML报错(t *testing.T) {
	if _, _, err := Parse([]byte("::: not yaml :::")); err == nil {
		t.Fatal("期望报错，实际成功")
	}
}

func TestParse_空订阅返回空列表(t *testing.T) {
	nodes, skipped, err := Parse([]byte("proxies: []\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(nodes) != 0 || len(skipped) != 0 {
		t.Errorf("空订阅应返回空列表, got nodes=%v skipped=%v", nodes, skipped)
	}
}
