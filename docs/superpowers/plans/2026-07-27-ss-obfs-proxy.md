# sub2api 机场订阅代理（ss + obfs）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 sub2api 单一二进制内原生支持 shadowsocks（+ 必要时 simple-obfs/tls）出站代理，机场订阅节点以普通代理记录形式纳入现有代理管理。

**Architecture:** 在现有的全局唯一代理接入口 `proxyutil.ConfigureTransportProxy` 增加 `ss` 分支，返回自定义 `DialContext`；不开本地监听端口，不改宿主机路由。ss 参数复用现有 `proxies` 五元组（cipher 占 username 位），插件参数存新增 `extra` 列并以 query string 形式挂在代理 URL 上，使下游全链路无感。

**Tech Stack:** Go 1.26.5 / `github.com/shadowsocks/go-shadowsocks2`（Apache-2.0）/ `gopkg.in/yaml.v3`（已是直接依赖）/ ent + 手写 SQL 迁移 / Vue 3 + TS

**设计文档：** `docs/superpowers/specs/2026-07-27-ss-obfs-proxy-design.md`

## Global Constraints

- 后端测试必须带 `-tags unit`：`cd backend && go test -tags unit ./...`（不带则 grok 相关测试静默跳过）
- 构建用 `D:/go1.25`，`GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache"`，`GOFLAGS=-mod=mod`；首次编译会下载 toolchain，慢，用后台跑
- 提交信息用中文
- `docs/*` 被 gitignore，提交本目录文件需 `git add -f`
- 分支：`develop/xuyang/ss-obfs-proxy`（已从 `main` 拉出）
- 定制迁移编号用 `9001_` 起，**禁止**使用 `186_` 等上游编号段
- 迁移文件一经应用不可修改内容（runner 有 SHA256 校验）
- 生成文件禁止手改：`backend/cmd/server/wire_gen.go`、`backend/internal/web/dist/`；`backend/ent/` 下除 `schema/` 外均为生成产物
- `proxyurl.Parse` 的 fail-fast 语义必须保持：无效代理立即报错，**任何情况下不得回退直连**
- 仅实现 obfs 的 `tls` 模式；`http` 模式节点在导入时明确跳过并提示，不静默降级
- LSP 报 `invalid go version '1.26.5'` 是本机旧工具链噪音，忽略

---

### Task 1: Spike — 判定 obfs 是否必需

**目的：** 本任务不产出生产代码。它回答一个问题：机场节点在**不带 obfs** 时能否直接用纯 ss 连通。若能，Task 5（obfs 实现）整块删除，项目风险与工作量大幅下降。

**Files:**
- Create: `backend/tmp_spike/main.go`（临时，任务结束即删）

**Interfaces:**
- Consumes: 无
- Produces: 一个判定结论，写入本计划 Task 5 的开头。不产出代码接口。

- [ ] **Step 1: 拿到一个节点的真实参数**

从订阅里挑一个节点，记下 `server` / `port` / `cipher` / `password` / `plugin-opts.host`。

```bash
curl -sL "<订阅链接>" | head -60
```

- [ ] **Step 2: 添加依赖**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" get github.com/shadowsocks/go-shadowsocks2@latest
```

- [ ] **Step 3: 写 spike 程序（纯 ss，不带 obfs）**

创建 `backend/tmp_spike/main.go`：

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
)

func main() {
	server := os.Getenv("SS_SERVER")     // 形如 1.2.3.4:443
	cipher := os.Getenv("SS_CIPHER")     // chacha20-ietf-poly1305
	password := os.Getenv("SS_PASSWORD") // 节点密码

	ciph, err := core.PickCipher(cipher, nil, password)
	if err != nil {
		fmt.Println("PickCipher 失败:", err)
		os.Exit(1)
	}

	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		target := socks.ParseAddr(addr)
		if target == nil {
			return nil, fmt.Errorf("bad target %q", addr)
		}
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", server)
		if err != nil {
			return nil, err
		}
		c = ciph.StreamConn(c)
		if _, err := c.Write(target); err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	}

	client := &http.Client{
		Transport: &http.Transport{DialContext: dial},
		Timeout:   20 * time.Second,
	}

	resp, err := client.Get("https://api.anthropic.com/v1/models")
	if err != nil {
		fmt.Println("❌ 纯 ss 连接失败，需要 obfs:", err)
		os.Exit(2)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	fmt.Printf("✅ 纯 ss 连通，HTTP %d\n%s\n", resp.StatusCode, body)
}
```

- [ ] **Step 4: 运行**

```bash
cd backend && SS_SERVER="节点:端口" SS_CIPHER="chacha20-ietf-poly1305" SS_PASSWORD="密码" GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" run ./tmp_spike
```

预期两种结果之一：
- `✅ 纯 ss 连通，HTTP 401`（401 是正常的，说明 TLS 打通了，只是没带 API key）→ **obfs 不需要**
- `❌ 纯 ss 连接失败，需要 obfs: ...` → **obfs 必需**

- [ ] **Step 5: 记录结论并清理**

把结论写进本文件 Task 5 开头的「判定结果」一行。然后删除 spike：

```bash
rm -rf backend/tmp_spike
```

- [ ] **Step 6: 提交依赖变更**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" mod tidy
git add backend/go.mod backend/go.sum
git commit -m "chore: 引入 go-shadowsocks2 依赖(Apache-2.0)"
```

---

### Task 2: Proxy 实体新增 extra 字段

**Files:**
- Modify: `backend/ent/schema/proxy.go`
- Create: `backend/migrations/9001_custom_proxy_extra.sql`
- Modify: `backend/internal/service/proxy.go`（`Proxy` 结构体与 `URL()`）
- Test: `backend/internal/service/proxy_url_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `service.Proxy.Extra map[string]string`；`(*Proxy).URL() string` 在 `Extra` 非空时输出带 query 的 URL，query 按 key 排序

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/service/proxy_url_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/service/ -run TestProxyURL -count=1
```

预期：编译失败，`unknown field Extra in struct literal`

- [ ] **Step 3: 给 Proxy 结构体加 Extra 并改写 URL()**

修改 `backend/internal/service/proxy.go`，在 `Proxy` 结构体末尾追加字段：

```go
	ExpiryWarnDays int
	Extra          map[string]string
}
```

改写 `URL()`：

```go
func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	if len(p.Extra) > 0 {
		q := make(url.Values, len(p.Extra))
		for k, v := range p.Extra {
			q.Set(k, v)
		}
		// url.Values.Encode() 按 key 排序，保证同一节点每次生成的字符串一致，
		// 否则会击穿 httpclient 以 URL 字符串为键的 transport 缓存。
		u.RawQuery = q.Encode()
	}
	return u.String()
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/service/ -run TestProxyURL -count=1
```

预期：PASS（3 个用例）

- [ ] **Step 5: 加 ent schema 字段**

修改 `backend/ent/schema/proxy.go`，在 `Fields()` 的 `expiry_warn_days` 之后追加：

```go
		field.JSON("extra", map[string]string{}).
			Optional().
			Comment("Protocol-specific extra parameters (e.g. shadowsocks plugin opts)."),
```

- [ ] **Step 6: 写迁移文件**

创建 `backend/migrations/9001_custom_proxy_extra.sql`：

```sql
-- 定制迁移：为 proxies 增加协议扩展参数列（shadowsocks 插件参数等）
-- 编号使用 9001_ 高位段，与上游迁移编号永久隔离，避免合并上游时撞号。
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS extra JSONB;
```

- [ ] **Step 7: 重新生成 ent 代码**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" generate ./ent
```

- [ ] **Step 8: 全量编译 + 测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" build ./... && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/service/ ./internal/repository/ -count=1
```

预期：编译通过，测试 PASS

- [ ] **Step 9: 提交**

```bash
git add backend/ent backend/migrations/9001_custom_proxy_extra.sql backend/internal/service/proxy.go backend/internal/service/proxy_url_test.go
git commit -m "feat: 代理实体新增 extra 扩展参数字段

- proxies 表加 JSONB extra 列(定制迁移 9001_)
- Proxy.URL() 将 extra 拼为 query string,按 key 排序保证稳定"
```

---

### Task 3: repository 层读写 extra

**Files:**
- Modify: `backend/internal/repository/proxy_repo.go`
- Test: `backend/internal/repository/proxy_repo_extra_test.go`

**Interfaces:**
- Consumes: `service.Proxy.Extra`（Task 2）
- Produces: `proxyRepo.Create` / `Update` / 各 List 方法在 entity ↔ domain 转换时保留 `Extra`

- [ ] **Step 1: 定位转换函数**

```bash
grep -n "func.*toServiceProxy\|func.*toProxy\|ExpiryWarnDays" backend/internal/repository/proxy_repo.go
```

记下 entity → `service.Proxy` 的转换函数名，以及 Create/Update 里设置字段的位置。

- [ ] **Step 2: 写失败测试**

创建 `backend/internal/repository/proxy_repo_extra_test.go`。参照同目录已有的 `proxy_repo_integration_test.go` 的建库/建 client 方式（复用其 helper，不要另起一套）：

```go
//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestProxyRepo_Extra往返(t *testing.T) {
	ctx := context.Background()
	repo, cleanup := newTestProxyRepo(t) // 复用本包已有 helper
	defer cleanup()

	want := map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "bing.com"}
	p := &service.Proxy{
		Name: "ss-node", Protocol: "ss", Host: "h.example.com", Port: 443,
		Username: "chacha20-ietf-poly1305", Password: "secret",
		Status: service.StatusActive, Extra: want,
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.Extra) != len(want) {
		t.Fatalf("Extra 长度不符: got %v, want %v", got.Extra, want)
	}
	for k, v := range want {
		if got.Extra[k] != v {
			t.Errorf("Extra[%q] = %q, want %q", k, got.Extra[k], v)
		}
	}
}
```

> 若本包 helper 名称与 `newTestProxyRepo` 不同，改用实际存在的那个，并保持 build tag 与同目录其他集成测试一致。

- [ ] **Step 3: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags "unit integration" ./internal/repository/ -run TestProxyRepo_Extra -count=1
```

预期：FAIL（Extra 为空）

- [ ] **Step 4: 在转换与写入路径补上 Extra**

在 Step 1 定位到的三处各加一行：

entity → domain 转换函数内，与 `ExpiryWarnDays` 并列：

```go
		Extra: e.Extra,
```

`Create` 内 builder 链上追加：

```go
		SetNillableExtra(nilIfEmpty(p.Extra)).
```

若 ent 未生成 `SetNillableExtra`，直接用条件写法：

```go
	if len(p.Extra) > 0 {
		builder = builder.SetExtra(p.Extra)
	}
```

`Update` 内同样处理（`Update` 需支持清空：`len == 0` 时调用 `ClearExtra()`）。

- [ ] **Step 5: 运行确认通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags "unit integration" ./internal/repository/ -run TestProxyRepo_Extra -count=1
```

预期：PASS

- [ ] **Step 6: 提交**

```bash
git add backend/internal/repository/
git commit -m "feat: 代理仓储层读写 extra 字段"
```

---

### Task 4: shadowsocks dialer（不含 obfs）

**Files:**
- Create: `backend/internal/pkg/shadowsocks/config.go`
- Create: `backend/internal/pkg/shadowsocks/dialer.go`
- Test: `backend/internal/pkg/shadowsocks/config_test.go`
- Test: `backend/internal/pkg/shadowsocks/dialer_test.go`

**Interfaces:**
- Consumes: `github.com/shadowsocks/go-shadowsocks2/core`、`.../socks`
- Produces:
  - `shadowsocks.Config{Server, Cipher, Password, ObfsMode, ObfsHost string}`
  - `shadowsocks.ConfigFromURL(u *url.URL) (Config, error)`
  - `shadowsocks.NewDialer(cfg Config) (*Dialer, error)`
  - `(*Dialer).DialContext(ctx context.Context, network, addr string) (net.Conn, error)`

- [ ] **Step 1: 写 config 失败测试**

创建 `backend/internal/pkg/shadowsocks/config_test.go`：

```go
package shadowsocks

import (
	"net/url"
	"strings"
	"testing"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func TestConfigFromURL_基本节点(t *testing.T) {
	cfg, err := ConfigFromURL(mustParse(t, "ss://chacha20-ietf-poly1305:pwd@node.example.com:443"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server != "node.example.com:443" {
		t.Errorf("Server = %q", cfg.Server)
	}
	if cfg.Cipher != "chacha20-ietf-poly1305" {
		t.Errorf("Cipher = %q", cfg.Cipher)
	}
	if cfg.Password != "pwd" {
		t.Errorf("Password = %q", cfg.Password)
	}
	if cfg.ObfsMode != "" {
		t.Errorf("ObfsMode 应为空, got %q", cfg.ObfsMode)
	}
}

func TestConfigFromURL_带obfs插件(t *testing.T) {
	cfg, err := ConfigFromURL(mustParse(t,
		"ss://chacha20-ietf-poly1305:pwd@n.example.com:443?plugin=obfs&mode=tls&obfs-host=bing.com"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ObfsMode != "tls" {
		t.Errorf("ObfsMode = %q, want tls", cfg.ObfsMode)
	}
	if cfg.ObfsHost != "bing.com" {
		t.Errorf("ObfsHost = %q, want bing.com", cfg.ObfsHost)
	}
}

func TestConfigFromURL_错误场景(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"缺少cipher", "ss://:pwd@h:443", "cipher"},
		{"缺少密码", "ss://chacha20-ietf-poly1305@h:443", "password"},
		{"缺少端口", "ss://chacha20-ietf-poly1305:pwd@h", "port"},
		{"不支持的插件", "ss://c:p@h:443?plugin=v2ray-plugin", "plugin"},
		{"不支持的obfs模式", "ss://c:p@h:443?plugin=obfs&mode=http", "mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigFromURL(mustParse(t, tc.raw))
			if err == nil {
				t.Fatal("期望报错，实际成功")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("错误信息 %q 未包含 %q", err.Error(), tc.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/shadowsocks/ -count=1
```

预期：编译失败，`undefined: ConfigFromURL`

- [ ] **Step 3: 实现 config.go**

创建 `backend/internal/pkg/shadowsocks/config.go`：

```go
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
```

- [ ] **Step 4: 运行 config 测试确认通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/shadowsocks/ -count=1
```

预期：PASS

- [ ] **Step 5: 写 dialer 失败测试**

创建 `backend/internal/pkg/shadowsocks/dialer_test.go`：

```go
package shadowsocks

import (
	"context"
	"strings"
	"testing"
)

func TestNewDialer_不支持的cipher报错(t *testing.T) {
	_, err := NewDialer(Config{
		Server: "h:443", Cipher: "rc4-md5", Password: "p",
	})
	if err == nil {
		t.Fatal("期望报错，实际成功")
	}
	if !strings.Contains(err.Error(), "cipher") {
		t.Errorf("错误信息应提到 cipher: %v", err)
	}
}

func TestNewDialer_合法配置(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("dialer 为 nil")
	}
}

func TestDialContext_拒绝非tcp网络(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	_, err = d.DialContext(context.Background(), "udp", "example.com:443")
	if err == nil {
		t.Fatal("期望拒绝 udp，实际成功")
	}
	if !strings.Contains(err.Error(), "network") {
		t.Errorf("错误信息应提到 network: %v", err)
	}
}

func TestDialContext_拒绝非法目标地址(t *testing.T) {
	d, err := NewDialer(Config{
		Server: "h:443", Cipher: "chacha20-ietf-poly1305", Password: "p",
	})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}
	_, err = d.DialContext(context.Background(), "tcp", "没有端口的地址")
	if err == nil {
		t.Fatal("期望拒绝非法地址，实际成功")
	}
}
```

- [ ] **Step 6: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/shadowsocks/ -run TestNewDialer -count=1
```

预期：编译失败，`undefined: NewDialer`

- [ ] **Step 7: 实现 dialer.go**

创建 `backend/internal/pkg/shadowsocks/dialer.go`：

```go
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
```

> 注：`newTLSObfsConn` 在 Task 5 实现。若 Task 1 判定不需要 obfs，则删除上面 `if d.cfg.ObfsMode == ObfsModeTLS` 整个分支，并在 `ConfigFromURL` 中把 `plugin=obfs` 也归入「不支持的插件」错误分支。

- [ ] **Step 8: 临时桩让本任务可独立通过**

若 Task 5 尚未实现，先在 `dialer.go` 同目录加一个最小桩，使编译通过（Task 5 会替换它）：

创建 `backend/internal/pkg/shadowsocks/obfs_tls.go`：

```go
package shadowsocks

import "net"

// newTLSObfsConn 在 Task 5 实现真正的 simple-obfs(tls) 封装。
// 当前为占位，直接返回原连接——仅供编译与非 obfs 路径的测试使用。
func newTLSObfsConn(conn net.Conn, host string) net.Conn {
	return conn
}
```

- [ ] **Step 9: 运行全部测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/shadowsocks/ -count=1 -v
```

预期：全部 PASS

- [ ] **Step 10: 提交**

```bash
git add backend/internal/pkg/shadowsocks/
git commit -m "feat: 新增 shadowsocks 出站 dialer

- ConfigFromURL 从代理 URL 解析节点参数,参数非法 fail-fast
- DialContext 建立 ss 隧道,可直接交给 http.Transport
- obfs 封装暂为占位,待后续任务实现"
```

---

### Task 5: simple-obfs (tls) 实现

> **判定结果（由 Task 1 填写）：** ☐ 需要 obfs　☐ 不需要 obfs（本任务整体跳过，并按 Task 4 Step 7 的注记删除 obfs 分支）

**Files:**
- Modify: `backend/internal/pkg/shadowsocks/obfs_tls.go`（替换 Task 4 的占位实现）
- Test: `backend/internal/pkg/shadowsocks/obfs_tls_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `newTLSObfsConn(conn net.Conn, host string) net.Conn` —— 返回的 `net.Conn` 对上层透明，内部完成 obfs 封装

**实现须知（重要）：** simple-obfs 的 TLS 模式分两部分：

1. **数据帧**：标准 TLS 1.2 application data 记录 —— `0x17 0x03 0x03` + 2 字节大端长度 + 载荷，单帧上限 16384 字节。这部分是 RFC 5246 公开格式，可直接实现
2. **握手帧**：首个 Write 需伪装成 ClientHello；首个 Read 需精确跳过服务端的握手响应字节数

第 2 部分的确切字节布局**必须以真实服务端行为为准**，不得凭记忆编写。Step 1 用真实节点抓取基准数据，后续测试以抓到的数据为 fixture。

- [ ] **Step 1: 抓取真实握手基准**

写一个临时程序连到真实节点，dump 首个 Write 之后服务端返回的原始字节：

创建 `backend/tmp_obfs/main.go`：

```go
package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	server := os.Getenv("SS_SERVER")
	conn, err := net.DialTimeout("tcp", server, 10*time.Second)
	if err != nil {
		fmt.Println("dial:", err)
		os.Exit(1)
	}
	defer conn.Close()

	// 发一个最小 TLS ClientHello 探针，观察服务端响应结构
	hello := []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00}
	if _, err := conn.Write(hello); err != nil {
		fmt.Println("write:", err)
		os.Exit(1)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	fmt.Printf("读到 %d 字节, err=%v\n", n, err)
	fmt.Println(hex.Dump(buf[:n]))
}
```

运行：

```bash
cd backend && SS_SERVER="节点:端口" GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" run ./tmp_obfs
```

把 hex dump 保存下来。据此确定：服务端响应的记录结构、总字节数、以及客户端应跳过多少字节才能读到真实载荷。

- [ ] **Step 2: 把基准数据写成 fixture 测试**

创建 `backend/internal/pkg/shadowsocks/obfs_tls_test.go`，用 `net.Pipe()` 造一个假服务端，回放 Step 1 抓到的响应字节，断言 `newTLSObfsConn` 能正确剥离伪装读出载荷。测试骨架：

```go
package shadowsocks

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// 数据帧格式是 RFC 5246 标准，可独立于握手先行验证。
func TestTLSObfs_数据帧往返(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	obfsConn := newTLSObfsConn(client, "bing.com")
	payload := []byte("hello shadowsocks")

	done := make(chan struct{})
	go func() {
		defer close(done)
		// 服务端侧：读掉客户端首包(握手伪装)，再读一个 application data 记录
		buf := make([]byte, 4096)
		if _, err := server.Read(buf); err != nil {
			t.Errorf("server read handshake: %v", err)
			return
		}
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(server, hdr); err != nil {
			t.Errorf("server read record header: %v", err)
			return
		}
		if hdr[0] != 0x17 || hdr[1] != 0x03 || hdr[2] != 0x03 {
			t.Errorf("record header = % x, want 17 03 03", hdr[:3])
			return
		}
		length := int(hdr[3])<<8 | int(hdr[4])
		body := make([]byte, length)
		if _, err := io.ReadFull(server, body); err != nil {
			t.Errorf("server read record body: %v", err)
			return
		}
		if !bytes.Equal(body, payload) {
			t.Errorf("body = %q, want %q", body, payload)
		}
	}()

	client.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if _, err := obfsConn.Write(payload); err != nil {
		t.Fatalf("obfs write: %v", err)
	}
	<-done
}

func TestTLSObfs_大载荷分帧(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	obfsConn := newTLSObfsConn(client, "bing.com")
	payload := bytes.Repeat([]byte("x"), maxTLSRecordPayload+1234)

	type record struct{ n int }
	records := make(chan record, 8)
	errCh := make(chan error, 1)

	go func() {
		defer close(records)
		// 先读掉首包（握手伪装）
		buf := make([]byte, 65536)
		if _, err := server.Read(buf); err != nil {
			errCh <- err
			return
		}
		remaining := len(payload)
		for remaining > 0 {
			hdr := make([]byte, 5)
			if _, err := io.ReadFull(server, hdr); err != nil {
				errCh <- err
				return
			}
			length := int(hdr[3])<<8 | int(hdr[4])
			if length > maxTLSRecordPayload {
				errCh <- fmt.Errorf("记录长度 %d 超过上限 %d", length, maxTLSRecordPayload)
				return
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(server, body); err != nil {
				errCh <- err
				return
			}
			records <- record{n: length}
			remaining -= length
		}
	}()

	client.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := obfsConn.Write(payload); err != nil {
		t.Fatalf("obfs write: %v", err)
	}

	count, total := 0, 0
	for r := range records {
		count++
		total += r.n
	}
	select {
	case err := <-errCh:
		t.Fatalf("server side: %v", err)
	default:
	}

	if count < 2 {
		t.Errorf("超长载荷应被拆成多帧，实际 %d 帧", count)
	}
	if total != len(payload) {
		t.Errorf("分帧后总长度 = %d, want %d", total, len(payload))
	}
}
```

测试文件的 import 需包含 `bytes`、`fmt`、`io`、`net`、`testing`、`time`。

- [ ] **Step 3: 实现 obfs_tls.go**

替换 Task 4 的占位文件。**记录帧部分是 RFC 5246 公开格式，下面给出完整实现；只有 `makeClientHello` 与 `serverHandshakeSkipBytes` 两处需要按 Step 1 的抓包结论落笔。**

```go
package shadowsocks

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// maxTLSRecordPayload 是单条 TLS 记录的最大载荷（RFC 5246 §6.2.1）。
const maxTLSRecordPayload = 16384

// serverHandshakeSkipBytes 是首次读取时需要丢弃的服务端握手响应字节数。
// !!! 该值必须由 Task 5 Step 1 的真实抓包确定，不得凭空填写 !!!
const serverHandshakeSkipBytes = 0 // TODO(Step 1): 按抓包结果填入

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
	if c.firstRequest {
		c.firstRequest = false
		hello, err := makeClientHello(b, c.host)
		if err != nil {
			return 0, err
		}
		if _, err := c.Conn.Write(hello); err != nil {
			return 0, err
		}
		return len(b), nil
	}

	total := len(b)
	for len(b) > 0 {
		chunk := b
		if len(chunk) > maxTLSRecordPayload {
			chunk = chunk[:maxTLSRecordPayload]
		}
		hdr := [5]byte{0x17, 0x03, 0x03}
		binary.BigEndian.PutUint16(hdr[3:], uint16(len(chunk)))
		if _, err := c.Conn.Write(append(hdr[:], chunk...)); err != nil {
			return total - len(b), err
		}
		b = b[len(chunk):]
	}
	return total, nil
}

func (c *tlsObfsConn) Read(b []byte) (int, error) {
	// 上一条记录还有剩余载荷，先读完
	if c.remain > 0 {
		n := c.remain
		if n > len(b) {
			n = len(b)
		}
		read, err := io.ReadFull(c.Conn, b[:n])
		c.remain -= read
		return read, err
	}

	if c.firstResponse {
		c.firstResponse = false
		if serverHandshakeSkipBytes > 0 {
			if _, err := io.CopyN(io.Discard, c.Conn, serverHandshakeSkipBytes); err != nil {
				return 0, fmt.Errorf("shadowsocks: skip obfs handshake: %w", err)
			}
		}
	}

	hdr := make([]byte, 5)
	if _, err := io.ReadFull(c.Conn, hdr); err != nil {
		return 0, err
	}
	if hdr[0] != 0x17 {
		return 0, fmt.Errorf("shadowsocks: unexpected TLS record type 0x%02x", hdr[0])
	}
	length := int(binary.BigEndian.Uint16(hdr[3:5]))
	if length > maxTLSRecordPayload {
		return 0, fmt.Errorf("shadowsocks: oversized TLS record %d", length)
	}

	n := length
	if n > len(b) {
		n = len(b)
	}
	read, err := io.ReadFull(c.Conn, b[:n])
	c.remain = length - read
	return read, err
}
```

`makeClientHello(payload []byte, host string) ([]byte, error)` 需构造一个含 SNI = `host` 且把 `payload` 嵌入其中的 TLS 1.2 ClientHello。其确切布局与 `serverHandshakeSkipBytes` 的取值**互相耦合**，必须一起由 Step 1 的抓包验证确定：先写一版、跑 Step 5 的端到端验证、根据实际服务端响应调整，直到 HTTPS 请求成功。

**禁止**从 mihomo / simple-obfs 等 GPL 项目复制源码——本仓库以 LGPL-3.0 分发，且已在设计阶段因许可证问题否决这些依赖。实现须基于 RFC 5246 的公开记录格式与 Step 1 观测到的服务端行为。

- [ ] **Step 4: 运行单元测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/shadowsocks/ -count=1 -v
```

预期：全部 PASS（含补全后的分帧用例）

- [ ] **Step 5: 端到端验证（真实节点）**

创建 `backend/tmp_obfs_e2e/main.go`——走完整的 `NewDialer` 路径，带 obfs 配置：

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/shadowsocks"
)

func main() {
	d, err := shadowsocks.NewDialer(shadowsocks.Config{
		Server:   os.Getenv("SS_SERVER"),
		Cipher:   os.Getenv("SS_CIPHER"),
		Password: os.Getenv("SS_PASSWORD"),
		ObfsMode: shadowsocks.ObfsModeTLS,
		ObfsHost: os.Getenv("SS_OBFS_HOST"),
	})
	if err != nil {
		fmt.Println("NewDialer 失败:", err)
		os.Exit(1)
	}

	client := &http.Client{
		Transport: &http.Transport{DialContext: d.DialContext},
		Timeout:   20 * time.Second,
	}

	resp, err := client.Get("https://api.anthropic.com/v1/models")
	if err != nil {
		fmt.Println("❌ ss+obfs 连接失败:", err)
		os.Exit(2)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	fmt.Printf("✅ ss+obfs 连通，HTTP %d\n%s\n", resp.StatusCode, body)
}
```

运行：

```bash
cd backend && SS_SERVER="节点:端口" SS_CIPHER="chacha20-ietf-poly1305" SS_PASSWORD="密码" SS_OBFS_HOST="伪装域名" GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" run ./tmp_obfs_e2e
```

预期：`✅ ss+obfs 连通，HTTP 401`（401 说明 TLS 已打通，只是没带 API key）。

若失败，回到 Step 3 调整 `makeClientHello` 与 `serverHandshakeSkipBytes`，重跑本步，直到通过。

- [ ] **Step 6: 清理临时程序并提交**

```bash
rm -rf backend/tmp_obfs backend/tmp_obfs_e2e
git add backend/internal/pkg/shadowsocks/
git commit -m "feat: 实现 simple-obfs(tls) 伪装层

基于 RFC 5246 记录格式自实现,未引入 GPL 依赖。
经真实机场节点端到端验证连通。"
```

---

### Task 6: 接入 proxyurl 白名单与 proxyutil 拨号

**Files:**
- Modify: `backend/internal/pkg/proxyurl/parse.go`
- Modify: `backend/internal/pkg/proxyutil/dialer.go`
- Test: `backend/internal/pkg/proxyurl/parse_test.go`（追加用例）
- Test: `backend/internal/pkg/proxyutil/dialer_test.go`（追加用例）

**Interfaces:**
- Consumes: `shadowsocks.ConfigFromURL`、`shadowsocks.NewDialer`（Task 4）
- Produces: `proxyurl.Parse` 接受 `ss://` scheme；`proxyutil.ConfigureTransportProxy` 为 ss 设置 `transport.DialContext`

- [ ] **Step 1: 写 proxyurl 失败测试**

在 `backend/internal/pkg/proxyurl/parse_test.go` 追加：

```go
func TestParse_接受ss协议(t *testing.T) {
	trimmed, parsed, err := Parse("ss://chacha20-ietf-poly1305:pwd@node.example.com:443?plugin=obfs&mode=tls&obfs-host=bing.com")
	if err != nil {
		t.Fatalf("ss 应被接受: %v", err)
	}
	if parsed.Scheme != "ss" {
		t.Errorf("Scheme = %q, want ss", parsed.Scheme)
	}
	if parsed.Query().Get("mode") != "tls" {
		t.Errorf("query 未保留: %q", trimmed)
	}
}

func TestParse_ss不被升级为socks5h(t *testing.T) {
	_, parsed, err := Parse("ss://c:p@h:443")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Scheme != "ss" {
		t.Errorf("ss 不应被改写, got %q", parsed.Scheme)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/proxyurl/ -run TestParse_接受ss -count=1
```

预期：FAIL，`unsupported proxy scheme "ss"`

- [ ] **Step 3: 白名单加 ss**

修改 `backend/internal/pkg/proxyurl/parse.go` 的 `allowedSchemes`：

```go
var allowedSchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
	// ss: shadowsocks（定制功能，合并上游时勿丢）
	"ss": true,
}
```

同时更新该文件顶部的包注释与 `Parse` 的文档注释，把 `ss` 列入允许协议。

- [ ] **Step 4: 运行确认通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/proxyurl/ -count=1
```

预期：PASS（含原有全部用例）

- [ ] **Step 5: 写 proxyutil 失败测试**

在 `backend/internal/pkg/proxyutil/dialer_test.go` 追加：

```go
func TestConfigureTransportProxy_ss设置DialContext(t *testing.T) {
	u, err := url.Parse("ss://chacha20-ietf-poly1305:pwd@node.example.com:443")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	tr := &http.Transport{}
	if err := ConfigureTransportProxy(tr, u); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext 未被设置")
	}
	if tr.Proxy != nil {
		t.Error("ss 不应设置 Transport.Proxy")
	}
}

func TestConfigureTransportProxy_ss参数非法时报错(t *testing.T) {
	u, err := url.Parse("ss://badcipher:pwd@node.example.com:443")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	tr := &http.Transport{}
	err = ConfigureTransportProxy(tr, u)
	if err == nil {
		t.Fatal("期望报错，实际成功")
	}
	if tr.DialContext != nil {
		t.Error("失败时不得设置 DialContext（禁止退化为直连）")
	}
}
```

- [ ] **Step 6: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/proxyutil/ -run TestConfigureTransportProxy_ss -count=1
```

预期：FAIL，`unsupported proxy scheme: ss`

- [ ] **Step 7: 加 ss 分支**

修改 `backend/internal/pkg/proxyutil/dialer.go`，在 `socks5` case 之后、`default` 之前插入：

```go
	case "ss":
		cfg, err := shadowsocks.ConfigFromURL(proxyURL)
		if err != nil {
			return err
		}
		dialer, err := shadowsocks.NewDialer(cfg)
		if err != nil {
			return err
		}
		transport.DialContext = dialer.DialContext
		return nil
```

并在 import 块加入：

```go
	"github.com/Wei-Shaw/sub2api/internal/pkg/shadowsocks"
```

同时更新文件顶部包注释，把 ss 列入支持协议。

- [ ] **Step 8: 运行确认通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/proxyurl/ ./internal/pkg/proxyutil/ ./internal/pkg/shadowsocks/ -count=1
```

预期：全部 PASS

- [ ] **Step 9: 提交**

```bash
git add backend/internal/pkg/proxyurl/ backend/internal/pkg/proxyutil/
git commit -m "feat: 代理链路接入 ss 协议

- proxyurl 白名单加 ss(定制,合并上游勿丢)
- proxyutil 为 ss 设置自定义 DialContext,参数非法时不设置(禁止退化直连)"
```

---

### Task 7: TLS 指纹路径适配 ss

**Files:**
- Modify: `backend/internal/pkg/tlsfingerprint/dialer.go`
- Test: `backend/internal/pkg/tlsfingerprint/dialer_ss_test.go`

**Interfaces:**
- Consumes: `(*shadowsocks.Dialer).DialContext`（Task 4）、`tlsfingerprint.NewDialer(profile, baseDialer)`（已存在）
- Produces: ss 代理场景下 TLS 指纹功能可用，**不新增 XXXProxyDialer 类型**

- [ ] **Step 1: 定位协议分派点**

```bash
grep -rn "NewSOCKS5ProxyDialer\|NewHTTPProxyDialer" backend/internal --include=*.go | grep -v _test
```

记下调用方按 scheme 分派的位置——ss 分支要加在那里。

- [ ] **Step 2: 写失败测试**

创建 `backend/internal/pkg/tlsfingerprint/dialer_ss_test.go`：

```go
package tlsfingerprint

import (
	"context"
	"net"
	"testing"
)

func TestNewDialer_接受自定义baseDialer(t *testing.T) {
	called := false
	base := func(ctx context.Context, network, addr string) (net.Conn, error) {
		called = true
		return nil, context.Canceled
	}

	d := NewDialer(nil, base)
	if d == nil {
		t.Fatal("NewDialer 返回 nil")
	}

	_, _ = d.DialTLSContext(context.Background(), "tcp", "example.com:443")
	if !called {
		t.Error("baseDialer 未被调用——ss 场景依赖它注入 ss 隧道")
	}
}
```

> 若 `NewDialer` 的 profile 参数不接受 nil，改传该包已有的默认 profile 构造函数。

- [ ] **Step 3: 运行确认失败或通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/tlsfingerprint/ -run TestNewDialer_接受自定义 -count=1
```

若 PASS，说明通用构造已可用，直接进入 Step 4 补分派逻辑；若 FAIL，先修 `NewDialer` 使其正确使用传入的 baseDialer。

- [ ] **Step 4: 在分派点加 ss 分支**

在 Step 1 定位到的分派位置，参照已有 socks5/http 分支的写法加入：

```go
	case "ss":
		cfg, err := shadowsocks.ConfigFromURL(proxyURL)
		if err != nil {
			return nil, err
		}
		ssDialer, err := shadowsocks.NewDialer(cfg)
		if err != nil {
			return nil, err
		}
		// 复用通用构造：ss 隧道作为 baseDialer，TLS 指纹握手在隧道之上进行
		return NewDialer(profile, ssDialer.DialContext), nil
```

- [ ] **Step 5: 运行测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/tlsfingerprint/ -count=1
```

预期：PASS

- [ ] **Step 6: 提交**

```bash
git add backend/internal/pkg/tlsfingerprint/
git commit -m "feat: TLS 指纹拨号支持 ss 代理

复用已有 NewDialer(profile, baseDialer) 通用构造,ss 隧道作为 baseDialer,
不新增按协议写死的 XXXProxyDialer。"
```

---

### Task 8: Clash 订阅解析

**Files:**
- Create: `backend/internal/pkg/clashsub/parse.go`
- Test: `backend/internal/pkg/clashsub/parse_test.go`

**Interfaces:**
- Consumes: `gopkg.in/yaml.v3`
- Produces:
  - `clashsub.Node{Name, Server string; Port int; Cipher, Password, Plugin, ObfsMode, ObfsHost string}`
  - `clashsub.Skipped{Name, Reason string}`
  - `clashsub.Parse(data []byte) (nodes []Node, skipped []Skipped, err error)`

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/pkg/clashsub/parse_test.go`：

```go
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
```

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/clashsub/ -count=1
```

预期：编译失败，`undefined: Parse`

- [ ] **Step 3: 实现 parse.go**

创建 `backend/internal/pkg/clashsub/parse.go`：

```go
// Package clashsub 解析 Clash 格式订阅，提取受支持的 shadowsocks 节点。
//
// 当前仅支持 type=ss，且插件为空或 obfs(tls)。
// 其余节点不会被静默丢弃，而是收集到 Skipped 列表并附带原因。
package clashsub

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node 是一个可用的 shadowsocks 节点。
type Node struct {
	Name     string
	Server   string
	Port     int
	Cipher   string
	Password string
	Plugin   string // "" 或 "obfs"
	ObfsMode string // Plugin=="obfs" 时为 "tls"
	ObfsHost string
}

// Skipped 记录被跳过的节点及原因。
type Skipped struct {
	Name   string
	Reason string
}

type clashFile struct {
	Proxies []clashProxy `yaml:"proxies"`
}

type clashProxy struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`
	Server     string         `yaml:"server"`
	Port       int            `yaml:"port"`
	Cipher     string         `yaml:"cipher"`
	Password   string         `yaml:"password"`
	Plugin     string         `yaml:"plugin"`
	PluginOpts map[string]any `yaml:"plugin-opts"`
}

// Parse 解析订阅内容。
func Parse(data []byte) ([]Node, []Skipped, error) {
	var f clashFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, nil, fmt.Errorf("clashsub: parse yaml: %w", err)
	}

	nodes := make([]Node, 0, len(f.Proxies))
	skipped := make([]Skipped, 0)

	for _, p := range f.Proxies {
		if p.Type != "ss" {
			skipped = append(skipped, Skipped{
				Name:   p.Name,
				Reason: fmt.Sprintf("不支持的节点类型 %q（仅支持 ss）", p.Type),
			})
			continue
		}
		if p.Server == "" || p.Port == 0 {
			skipped = append(skipped, Skipped{Name: p.Name, Reason: "缺少 server 或 port"})
			continue
		}
		if p.Cipher == "" || p.Password == "" {
			skipped = append(skipped, Skipped{Name: p.Name, Reason: "缺少 cipher 或 password"})
			continue
		}

		n := Node{
			Name:     p.Name,
			Server:   p.Server,
			Port:     p.Port,
			Cipher:   p.Cipher,
			Password: p.Password,
		}

		switch plugin := strings.TrimSpace(p.Plugin); plugin {
		case "":
			// 无插件，直接可用
		case "obfs":
			mode, _ := p.PluginOpts["mode"].(string)
			if mode != "tls" {
				skipped = append(skipped, Skipped{
					Name:   p.Name,
					Reason: fmt.Sprintf("不支持的 obfs 模式 %q（仅支持 tls）", mode),
				})
				continue
			}
			host, _ := p.PluginOpts["host"].(string)
			if host == "" {
				skipped = append(skipped, Skipped{Name: p.Name, Reason: "obfs 缺少 host"})
				continue
			}
			n.Plugin = "obfs"
			n.ObfsMode = mode
			n.ObfsHost = host
		default:
			skipped = append(skipped, Skipped{
				Name:   p.Name,
				Reason: fmt.Sprintf("不支持的插件 %q（仅支持 obfs）", plugin),
			})
			continue
		}

		nodes = append(nodes, n)
	}

	return nodes, skipped, nil
}
```

- [ ] **Step 4: 运行确认通过**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/pkg/clashsub/ -count=1 -v
```

预期：全部 PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/pkg/clashsub/
git commit -m "feat: 新增 Clash 订阅解析包

提取 type=ss 节点(支持无插件与 obfs/tls);
其余节点收集到 skipped 列表并附原因,不静默丢弃。"
```

---

### Task 9: 订阅导入 API

**Files:**
- Create: `backend/internal/handler/admin/proxy_subscription.go`
- Modify: `backend/internal/server/routes/admin.go`
- Test: `backend/internal/handler/admin/proxy_subscription_test.go`

**Interfaces:**
- Consumes: `clashsub.Parse`（Task 8）、既有的代理创建与去重逻辑（`buildProxyKey`）
- Produces: `POST /admin/proxies/import-subscription`，请求体 `{"url": "...", "dry_run": bool}`，响应 `{"created": n, "updated": n, "skipped": [{name, reason}]}`

- [ ] **Step 1: 阅读既有导入实现**

```bash
grep -n "func (h \*ProxyHandler)" backend/internal/handler/admin/proxy_data.go
grep -n "proxies" backend/internal/server/routes/admin.go
```

记下 `ImportData` 的签名、`buildProxyKey` 的用法、以及代理路由分组的注册位置。

- [ ] **Step 2: 写失败测试**

创建 `backend/internal/handler/admin/proxy_subscription_test.go`。参照同目录 `account_data_handler_test.go` 的 gin 测试上下文构造方式：

```go
package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportSubscription_dryRun不落库(t *testing.T) {
	sub := `proxies:
  - name: "hk"
    type: ss
    server: hk.example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: pwd
`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sub))
	}))
	defer upstream.Close()

	h, stub := newTestProxyHandler(t) // 复用本包已有 stub 构造
	rec := httptest.NewRecorder()
	c := newTestGinContext(rec, http.MethodPost, "/admin/proxies/import-subscription",
		strings.NewReader(`{"url":"`+upstream.URL+`","dry_run":true}`))

	h.ImportSubscription(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := stub.CreatedCount(); got != 0 {
		t.Errorf("dry_run 不应创建代理，实际创建 %d 个", got)
	}
	if !strings.Contains(rec.Body.String(), `"created":1`) {
		t.Errorf("应报告将创建 1 个，实际 body = %s", rec.Body.String())
	}
}

func TestImportSubscription_拉取失败返回错误(t *testing.T) {
	h, _ := newTestProxyHandler(t)
	rec := httptest.NewRecorder()
	c := newTestGinContext(rec, http.MethodPost, "/admin/proxies/import-subscription",
		strings.NewReader(`{"url":"http://127.0.0.1:1/nonexistent"}`))

	h.ImportSubscription(c)

	if rec.Code == http.StatusOK {
		t.Fatalf("期望非 200，实际 200: %s", rec.Body.String())
	}
}
```

> 若本包 helper 名称不同（如 `newProxyHandlerStub`），改用实际存在的那个。

- [ ] **Step 3: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/handler/admin/ -run TestImportSubscription -count=1
```

预期：编译失败，`h.ImportSubscription undefined`

- [ ] **Step 4: 实现 handler**

创建 `backend/internal/handler/admin/proxy_subscription.go`：

```go
package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/clashsub"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	subscriptionFetchTimeout = 30 * time.Second
	subscriptionMaxBytes     = 8 << 20 // 8 MiB，防御超大响应
)

type importSubscriptionRequest struct {
	URL    string `json:"url"`
	DryRun bool   `json:"dry_run"`
}

type importSubscriptionResponse struct {
	Created int                `json:"created"`
	Updated int                `json:"updated"`
	Skipped []clashsub.Skipped `json:"skipped"`
}

// ImportSubscription 拉取 Clash 订阅并把其中的 ss 节点导入为代理记录。
func (h *ProxyHandler) ImportSubscription(c *gin.Context) {
	ctx := c.Request.Context()

	var req importSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		response.BadRequest(c, "subscription url is required")
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		response.BadRequest(c, "subscription url must be http or https")
		return
	}

	data, err := fetchSubscription(ctx, req.URL)
	if err != nil {
		response.BadRequest(c, fmt.Sprintf("fetch subscription failed: %v", err))
		return
	}

	nodes, skipped, err := clashsub.Parse(data)
	if err != nil {
		response.BadRequest(c, fmt.Sprintf("parse subscription failed: %v", err))
		return
	}

	resp := importSubscriptionResponse{Skipped: skipped}

	for _, n := range nodes {
		extra := map[string]string{}
		if n.Plugin == "obfs" {
			extra["plugin"] = "obfs"
			extra["mode"] = n.ObfsMode
			extra["obfs-host"] = n.ObfsHost
		}

		// 去重口径与既有导入一致：host + port + username(cipher) + password
		exists, err := h.adminService.CheckProxyExists(ctx, n.Server, n.Port, n.Cipher, n.Password)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if exists {
			resp.Updated++
			continue
		}
		resp.Created++

		if req.DryRun {
			continue
		}

		if _, err := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:     n.Name,
			Protocol: "ss",
			Host:     n.Server,
			Port:     n.Port,
			Username: n.Cipher,
			Password: n.Password,
			Extra:    extra,
		}); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	response.Success(c, resp)
}

func fetchSubscription(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// 部分机场按 User-Agent 返回不同格式，显式要求 Clash 格式
	req.Header.Set("User-Agent", "clash-verge/v1.0.0")

	client := &http.Client{Timeout: subscriptionFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned HTTP %s", strconv.Itoa(resp.StatusCode))
	}

	return io.ReadAll(io.LimitReader(resp.Body, subscriptionMaxBytes))
}
```

同时给 `CreateProxyInput` / `UpdateProxyInput` 加 `Extra` 字段（`backend/internal/service/admin_service.go:444` 与 `:457`），在两个结构体的 `ExpiryWarnDays` 之后各追加一行：

```go
	Extra          map[string]string
```

并在 `backend/internal/service/admin_proxy.go` 的 `CreateProxy`（约 72 行）构造 `Proxy` 时补一行：

```go
		ExpiryWarnDays: input.ExpiryWarnDays,
		Extra:          input.Extra,
	}
```

`UpdateProxy` 内同样透传 `Extra`。

> 注意：`backend/internal/service/proxy_service.go` 里那个独立的 `ProxyService` / `CreateProxyRequest` 是**另一条路径**，admin handler 并不使用它。为保持两条路径一致，也给 `CreateProxyRequest` 加同名字段并在 `ProxyService.Create` 中透传，避免日后有人用错入口导致 Extra 丢失。

- [ ] **Step 5: 注册路由**

在 `backend/internal/server/routes/admin.go` 的代理路由分组内追加（紧邻已有的 import/export 路由）：

```go
		proxies.POST("/import-subscription", proxyHandler.ImportSubscription)
```

- [ ] **Step 6: 运行测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/handler/admin/ ./internal/service/ -count=1
```

预期：PASS

- [ ] **Step 7: 提交**

```bash
git add backend/internal/handler/admin/proxy_subscription.go backend/internal/handler/admin/proxy_subscription_test.go backend/internal/server/routes/admin.go backend/internal/service/admin_service.go backend/internal/service/admin_proxy.go backend/internal/service/proxy_service.go
git commit -m "feat: 新增订阅导入接口

POST /admin/proxies/import-subscription
拉取 Clash 订阅,提取 ss 节点导入为代理记录;
支持 dry_run 预览;不支持的节点返回跳过原因。"
```

---

### Task 10: 导入导出携带 extra

**Files:**
- Modify: `backend/internal/handler/admin/proxy_data.go`
- Modify: `backend/internal/handler/admin/account_data.go`（`DataProxy` 结构体与协议校验）
- Test: `backend/internal/handler/admin/proxy_data_extra_test.go`

**Interfaces:**
- Consumes: `service.Proxy.Extra`（Task 2）
- Produces: `DataProxy` 增加 `Extra map[string]string` 字段；协议校验放行 `ss`

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/handler/admin/proxy_data_extra_test.go`：

```go
package admin

import "testing"

func TestValidateProxyItem_接受ss协议(t *testing.T) {
	item := DataProxy{
		Protocol: "ss", Host: "h.example.com", Port: 443,
		Username: "chacha20-ietf-poly1305", Password: "pwd",
	}
	if err := validateProxyItem(&item); err != nil {
		t.Fatalf("ss 应被接受: %v", err)
	}
}

func TestValidateProxyItem_拒绝未知协议(t *testing.T) {
	item := DataProxy{
		Protocol: "vmess", Host: "h", Port: 443,
	}
	if err := validateProxyItem(&item); err == nil {
		t.Fatal("vmess 应被拒绝")
	}
}
```

> 若校验函数名不是 `validateProxyItem`，用 `grep -n "proxy protocol is invalid" backend/internal/handler/admin/account_data.go` 定位真实函数名并替换。

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/handler/admin/ -run TestValidateProxyItem -count=1
```

预期：FAIL，`proxy protocol is invalid: ss`

- [ ] **Step 3: 放行 ss 并给 DataProxy 加 Extra**

修改 `backend/internal/handler/admin/account_data.go` 的协议校验（约 688 行）：

```go
	switch item.Protocol {
	case "http", "https", "socks5", "socks5h", "ss":
```

在 `DataProxy` 结构体加字段：

```go
	Extra map[string]string `json:"extra,omitempty"`
```

- [ ] **Step 4: 导出与导入路径带上 Extra**

在 `backend/internal/handler/admin/proxy_data.go` 与 `account_data.go` 中，所有构造 `DataProxy` 的位置补 `Extra: p.Extra,`；所有从 `DataProxy` 构造 `service.CreateProxyInput` 的位置补 `Extra: item.Extra,`。

用以下命令确认没有遗漏：

```bash
grep -n "DataProxy{" backend/internal/handler/admin/*.go
grep -n "CreateProxyInput{" backend/internal/handler/admin/*.go
```

- [ ] **Step 5: 运行测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./internal/handler/admin/ -count=1
```

预期：PASS（含既有导入导出用例）

- [ ] **Step 6: 提交**

```bash
git add backend/internal/handler/admin/
git commit -m "feat: 代理导入导出携带 extra 并放行 ss 协议"
```

---

### Task 11: 前端代理表单适配 ss

**Files:**
- Modify: `frontend/src/types/index.ts`（Proxy 类型加 `extra`）
- Modify: `frontend/src/views/admin/ProxiesView.vue`（协议下拉、表单标签、订阅导入入口）
- Modify: `frontend/src/api/admin/proxies.ts`（新增订阅导入 API）
- Modify: `frontend/src/i18n/locales/zh/admin/resources.ts`
- Modify: `frontend/src/i18n/locales/en/admin/resources.ts`

**Interfaces:**
- Consumes: 后端 `POST /admin/proxies/import-subscription`（Task 9）、代理对象新增的 `extra` 字段
- Produces: 协议下拉含 `ss`；选中 ss 时用户名标签变为「加密方式」；新增「从订阅导入」入口

- [ ] **Step 1: 确认协议选项与表单位置**

```bash
grep -n "socks5" frontend/src/views/admin/ProxiesView.vue frontend/src/types/index.ts frontend/src/i18n/locales/zh/admin/resources.ts
```

记下协议选项数组、表单里用户名字段的 label 绑定、以及工具栏按钮区的行号。

- [ ] **Step 2: TS 类型加 extra**

在 `frontend/src/types/index.ts` 的 Proxy 接口中追加：

```ts
  extra?: Record<string, string>
```

- [ ] **Step 3: 协议下拉加 ss**

在协议选项数组中追加：

```ts
{ label: 'Shadowsocks (ss)', value: 'ss' },
```

- [ ] **Step 4: 按协议切换字段标签**

在代理表单组件中，用计算属性替换写死的「用户名/密码」标签：

```ts
const isShadowsocks = computed(() => form.protocol === 'ss')
const usernameLabel = computed(() =>
  isShadowsocks.value ? t('proxy.cipher') : t('proxy.username')
)
```

模板里把用户名字段的 label 换成 `usernameLabel`。并在 i18n 文件补 `proxy.cipher`（中文「加密方式」、英文 `Cipher`）。

- [ ] **Step 5: 加「从订阅导入」入口**

在代理列表页工具栏加按钮，点击弹出输入框收订阅 URL，先以 `dry_run: true` 调用接口展示预览（将创建 N 个、跳过列表及原因），用户确认后再以 `dry_run: false` 正式导入。

- [ ] **Step 6: 类型检查与测试**

```bash
cd frontend && npx vue-tsc --noEmit && npx vitest run
```

预期：类型检查无错误，测试全绿

- [ ] **Step 7: 提交**

```bash
git add frontend/src/
git commit -m "feat: 前端代理表单支持 ss 协议与订阅导入

- 协议下拉新增 ss;选中时用户名标签改为「加密方式」
- 代理列表页新增「从订阅导入」,先 dry_run 预览再确认导入"
```

---

### Task 12: 全量验证与文档登记

**Files:**
- Modify: `CUSTOM_CHANGES.md`
- Modify: `E:\ai-md\claude\plan\sub2api机场订阅代理支持.md`（同步状态）

**Interfaces:**
- Consumes: 前序全部任务
- Produces: 可发布状态

- [ ] **Step 1: 后端全量测试**

```bash
cd backend && GOMODCACHE="E:/code/AI/codex/sub2api-investigation-old/.gomodcache" GOFLAGS=-mod=mod "D:/go1.25/bin/go.exe" test -tags unit ./... -count=1
```

预期：全绿。若 `TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig` 超时，属已知偶发（上游同样存在），可忽略。

- [ ] **Step 2: 前端全量验证**

```bash
cd frontend && npx vue-tsc --noEmit && npx vitest run
```

预期：全绿

- [ ] **Step 3: 真实链路验收**

在管理台：
1. 「从订阅导入」粘贴订阅链接 → 确认预览里节点数与跳过原因合理 → 导入
2. 代理列表能看到 ss 节点，点「测试延迟」应返回正常延迟（验证探测链路自动生效）
3. 把一个上游账号绑定到该 ss 代理，用该账号发一次真实请求，确认成功

- [ ] **Step 4: 在 CUSTOM_CHANGES 登记**

在「持续维护的定制功能清单」表格追加：

```markdown
| **shadowsocks 出站代理**（ss + simple-obfs/tls；订阅导入） | `pkg/shadowsocks/`、`pkg/clashsub/`、`pkg/proxyurl/parse.go`（白名单含 ss）、`pkg/proxyutil/dialer.go`、`pkg/tlsfingerprint/dialer.go`、`migrations/9001_custom_proxy_extra.sql`、`handler/admin/proxy_subscription.go`、前端代理表单 |
```

并在文件顶部按版本追加一条发布说明条目。

- [ ] **Step 5: 同步 plan 活文档**

更新 `E:\ai-md\claude\plan\sub2api机场订阅代理支持.md` 的「当前状态 / 迭代历史 / 下一步」三段。

- [ ] **Step 6: 提交并推送**

```bash
git add CUSTOM_CHANGES.md
git commit -m "docs: 登记 shadowsocks 出站代理定制功能"
git push -u origin develop/xuyang/ss-obfs-proxy
```

---

## 附：合并上游时的保护清单

合并上游后务必确认以下各处仍然存在（按易丢失程度排序）：

1. `backend/internal/pkg/proxyurl/parse.go` 的 `allowedSchemes` 里的 `"ss": true` —— **最易被冲掉**
2. `backend/internal/pkg/proxyutil/dialer.go` 的 `case "ss"` 分支
3. `backend/internal/handler/admin/account_data.go` 协议校验 switch 里的 `"ss"`
4. `backend/migrations/9001_custom_proxy_extra.sql` 文件本身
5. `backend/ent/schema/proxy.go` 的 `extra` 字段（丢失会导致 ent 生成代码不含该字段，编译报错，属显性失败）
6. `Proxy.URL()` 中的 query 拼接逻辑
