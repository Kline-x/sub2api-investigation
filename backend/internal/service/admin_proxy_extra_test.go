package service

import (
	"context"
	"testing"
)

// proxyExtraRepoStub 仅实现 UpdateProxy 用到的两个方法；
// 内嵌的 nil ProxyRepository 会让其它方法调用 panic，暴露非预期的仓储访问。
type proxyExtraRepoStub struct {
	ProxyRepository
	stored  *Proxy
	updated *Proxy
}

func (s *proxyExtraRepoStub) GetByID(_ context.Context, _ int64) (*Proxy, error) {
	// 返回副本，模拟仓储每次读出独立对象
	cp := *s.stored
	if s.stored.Extra != nil {
		cp.Extra = make(map[string]string, len(s.stored.Extra))
		for k, v := range s.stored.Extra {
			cp.Extra[k] = v
		}
	}
	return &cp, nil
}

func (s *proxyExtraRepoStub) Update(_ context.Context, p *Proxy) error {
	s.updated = p
	return nil
}

// 保护性回归：UpdateProxyInput.Extra 为 nil 时不得覆盖已有 Extra。
// admin_proxy.go 中 `if input.Extra != nil` 这一行是唯一防线——
// 若被改成无条件赋值，管理员在面板编辑一次 ss 代理就会清空 obfs 参数，
// 节点静默失效（连接看似成功、实际被墙）。合并上游后此测试必须仍然通过。
func TestUpdateProxy_Extra为nil时保留原有Extra(t *testing.T) {
	repo := &proxyExtraRepoStub{stored: &Proxy{
		ID:       1,
		Name:     "node",
		Protocol: "ss",
		Host:     "1.2.3.4",
		Port:     443,
		Extra: map[string]string{
			"plugin":    "obfs",
			"mode":      "tls",
			"obfs-host": "example.com",
		},
	}}
	svc := &adminServiceImpl{proxyRepo: repo}

	got, err := svc.UpdateProxy(context.Background(), 1, &UpdateProxyInput{
		Name:  "node-renamed",
		Extra: nil,
	})
	if err != nil {
		t.Fatalf("UpdateProxy() error = %v", err)
	}
	if repo.updated == nil {
		t.Fatal("expected repo.Update to be called")
	}
	if len(got.Extra) != 3 {
		t.Fatalf("Extra 被覆盖：got %v, want 3 keys", got.Extra)
	}
	for k, want := range map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "example.com"} {
		if got.Extra[k] != want {
			t.Fatalf("Extra[%q] = %q, want %q", k, got.Extra[k], want)
		}
	}
	if got.Name != "node-renamed" {
		t.Fatalf("Name = %q, want %q", got.Name, "node-renamed")
	}
}

// 显式传入 Extra 时应当整体覆盖（订阅重新导入 / 显式编辑 obfs 参数的路径）。
func TestUpdateProxy_显式传入Extra时覆盖(t *testing.T) {
	repo := &proxyExtraRepoStub{stored: &Proxy{
		ID:       2,
		Protocol: "ss",
		Extra:    map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "old.example.com"},
	}}
	svc := &adminServiceImpl{proxyRepo: repo}

	got, err := svc.UpdateProxy(context.Background(), 2, &UpdateProxyInput{
		Extra: map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "new.example.com"},
	})
	if err != nil {
		t.Fatalf("UpdateProxy() error = %v", err)
	}
	if got.Extra["obfs-host"] != "new.example.com" {
		t.Fatalf("Extra[obfs-host] = %q, want %q", got.Extra["obfs-host"], "new.example.com")
	}
}

// 传入空（非 nil）map 表示显式清空，应当生效——否则将无法把一个 ss 代理改回普通节点。
func TestUpdateProxy_传入空map表示显式清空(t *testing.T) {
	repo := &proxyExtraRepoStub{stored: &Proxy{
		ID:       3,
		Protocol: "ss",
		Extra:    map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "example.com"},
	}}
	svc := &adminServiceImpl{proxyRepo: repo}

	got, err := svc.UpdateProxy(context.Background(), 3, &UpdateProxyInput{
		Extra: map[string]string{},
	})
	if err != nil {
		t.Fatalf("UpdateProxy() error = %v", err)
	}
	if len(got.Extra) != 0 {
		t.Fatalf("Extra = %v, want empty", got.Extra)
	}
}
