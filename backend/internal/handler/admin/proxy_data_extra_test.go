package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestValidateDataProxy_接受ss协议 确认协议白名单放行 ss（订阅节点出站能力落地后，
// 导入侧不应再把 ss 代理当非法协议拒绝）。
func TestValidateDataProxy_接受ss协议(t *testing.T) {
	item := DataProxy{
		Protocol: "ss",
		Host:     "h.example.com",
		Port:     443,
		Username: "chacha20-ietf-poly1305",
		Password: "pwd",
	}
	if err := validateDataProxy(item); err != nil {
		t.Fatalf("ss 应被接受: %v", err)
	}
}

// TestValidateDataProxy_拒绝未知协议 确认白名单仍然拒绝不支持的协议，防止误放行。
func TestValidateDataProxy_拒绝未知协议(t *testing.T) {
	item := DataProxy{
		Protocol: "vmess",
		Host:     "h",
		Port:     443,
	}
	if err := validateDataProxy(item); err == nil {
		t.Fatal("vmess 应被拒绝")
	}
}

// TestProxyExportDataIncludesExtra 覆盖导出方向：ss 代理的 extra（cipher/plugin 等协议
// 专属参数）必须原样出现在导出 JSON 中，否则迁移到新实例后节点会因缺参数不可用。
func TestProxyExportDataIncludesExtra(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "ss-node",
			Protocol: "ss",
			Host:     "ss.example.com",
			Port:     8443,
			Username: "chacha20-ietf-poly1305",
			Password: "secret",
			Status:   service.StatusActive,
			Extra: map[string]string{
				"plugin":     "obfs-local",
				"plugin_opt": "obfs=tls;obfs-host=www.example.com",
			},
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyDataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "ss", resp.Data.Proxies[0].Protocol)
	require.Equal(t, map[string]string{
		"plugin":     "obfs-local",
		"plugin_opt": "obfs=tls;obfs-host=www.example.com",
	}, resp.Data.Proxies[0].Extra)
}

// TestProxyImportDataCreatesProxyWithExtra 覆盖导入方向：导入 payload 中携带的 extra
// 必须透传进 service.CreateProxyInput，否则新建的 ss 代理会丢失插件参数而不可用。
// 与上一个导出测试合起来验证「导出→导入」往返不丢字段，而非只验证单向。
func TestProxyImportDataCreatesProxyWithExtra(t *testing.T) {
	router, adminSvc := setupProxyDataRouter()

	payload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "ss|ss.example.com|8443|chacha20-ietf-poly1305|secret",
					"name":      "ss-node",
					"protocol":  "ss",
					"host":      "ss.example.com",
					"port":      8443,
					"username":  "chacha20-ietf-poly1305",
					"password":  "secret",
					"status":    "active",
					"extra": map[string]string{
						"plugin":     "obfs-local",
						"plugin_opt": "obfs=tls;obfs-host=www.example.com",
					},
				},
			},
			"accounts": []map[string]any{},
		},
	}

	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp proxyImportResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.ProxyCreated)
	require.Equal(t, 0, resp.Data.ProxyFailed)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Len(t, adminSvc.createdProxies, 1)
	require.Equal(t, "ss", adminSvc.createdProxies[0].Protocol)
	require.Equal(t, map[string]string{
		"plugin":     "obfs-local",
		"plugin_opt": "obfs=tls;obfs-host=www.example.com",
	}, adminSvc.createdProxies[0].Extra)
}
