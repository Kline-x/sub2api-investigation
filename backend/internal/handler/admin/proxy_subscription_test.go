package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setupProxySubscriptionRouter 沿用本包既有的 gin 测试路由构造模式
// （见 proxy_data_handler_test.go 的 setupProxyDataRouter）。
func setupProxySubscriptionRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewProxyHandler(adminSvc)
	router.POST("/api/v1/admin/proxies/import-subscription", h.ImportSubscription)

	return router, adminSvc
}

type importSubscriptionAPIResponse struct {
	Code int                        `json:"code"`
	Data importSubscriptionResponse `json:"data"`
}

const validSSSubscription = `proxies:
  - name: "hk"
    type: ss
    server: hk.example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: pwd
`

func doImportSubscription(t *testing.T, router *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/import-subscription", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestImportSubscription_dryRun不落库(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(validSSSubscription))
	}))
	defer upstream.Close()

	router, adminSvc := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":"`+upstream.URL+`","dry_run":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	adminSvc.mu.Lock()
	created := len(adminSvc.createdProxies)
	adminSvc.mu.Unlock()
	require.Equal(t, 0, created, "dry_run 不应创建代理")

	var resp importSubscriptionAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Data.Created)
	require.Equal(t, 0, resp.Data.Updated)
}

func TestImportSubscription_实际导入创建代理并透传Extra(t *testing.T) {
	sub := `proxies:
  - name: "hk-obfs"
    type: ss
    server: hk.example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: pwd
    plugin: obfs
    plugin-opts:
      mode: tls
      host: bing.com
  - name: "unsupported"
    type: vmess
    server: xx.example.com
    port: 443
`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sub))
	}))
	defer upstream.Close()

	router, adminSvc := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":"`+upstream.URL+`","dry_run":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp importSubscriptionAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Data.Created)
	require.Equal(t, 0, resp.Data.Updated)
	require.Len(t, resp.Data.Skipped, 1)
	require.Equal(t, "unsupported", resp.Data.Skipped[0].Name)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Len(t, adminSvc.createdProxies, 1)
	created := adminSvc.createdProxies[0]
	require.Equal(t, "ss", created.Protocol)
	require.Equal(t, "hk.example.com", created.Host)
	require.Equal(t, 443, created.Port)
	require.Equal(t, "chacha20-ietf-poly1305", created.Username)
	require.Equal(t, "pwd", created.Password)
	require.Equal(t, map[string]string{"plugin": "obfs", "mode": "tls", "obfs-host": "bing.com"}, created.Extra)
}

func TestImportSubscription_已存在的代理计为Updated且不重复创建(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(validSSSubscription))
	}))
	defer upstream.Close()

	router, adminSvc := setupProxySubscriptionRouter()
	adminSvc.checkProxyExistsResult = true

	rec := doImportSubscription(t, router, `{"url":"`+upstream.URL+`","dry_run":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp importSubscriptionAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Data.Created)
	require.Equal(t, 1, resp.Data.Updated)

	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Empty(t, adminSvc.createdProxies)
}

func TestImportSubscription_拉取失败返回错误(t *testing.T) {
	router, _ := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":"http://127.0.0.1:1/nonexistent"}`)
	require.NotEqual(t, http.StatusOK, rec.Code)
}

func TestImportSubscription_拉取失败不泄露订阅URL中的密钥(t *testing.T) {
	router, _ := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":"http://127.0.0.1:1/sub?token=SECRET-TOKEN-VALUE"}`)
	require.NotEqual(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "SECRET-TOKEN-VALUE")
}

// TestImportSubscription_响应JSON字段名为小写蛇形
//
// 背景：clashsub.Skipped 的 Name/Reason 字段曾经没有 json tag，导致响应体里输出的是
// 大写的 "Name"/"Reason"，与接口契约（小写 "name"/"reason"）不符。前端按小写 key 取值会拿到
// undefined。此前的测试用同一个无 tag 的类型去反序列化断言，编码和解码用了同一套错误大小写，
// 二者互相抵消，测试通过但契约是错的——完全测不出这个问题。
//
// 这个测试直接对响应体的原始 JSON 文本 / 反序列化到 map[string]any 做断言，不经过
// importSubscriptionResponse / clashsub.Skipped 的 Go 结构体往返，因此字段 tag 一旦被去掉或
// 写错，该测试必然失败。
func TestImportSubscription_响应JSON字段名为小写蛇形(t *testing.T) {
	sub := `proxies:
  - name: "hk-obfs"
    type: ss
    server: hk.example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: pwd
    plugin: obfs
    plugin-opts:
      mode: tls
      host: bing.com
  - name: "unsupported"
    type: vmess
    server: xx.example.com
    port: 443
`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sub))
	}))
	defer upstream.Close()

	router, _ := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":"`+upstream.URL+`","dry_run":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := rec.Body.String()

	// 原始 body 文本层面：必须含小写 key，绝不能含大写 key（防止字段 tag 被误删或写错）。
	require.Contains(t, body, `"created"`)
	require.Contains(t, body, `"updated"`)
	require.Contains(t, body, `"skipped"`)
	require.Contains(t, body, `"name"`)
	require.Contains(t, body, `"reason"`)
	require.NotContains(t, body, `"Created"`)
	require.NotContains(t, body, `"Updated"`)
	require.NotContains(t, body, `"Skipped"`)
	require.NotContains(t, body, `"Name"`)
	require.NotContains(t, body, `"Reason"`)

	// 反序列化到不带 Go 字段名假设的通用 map，按小写 key 精确取值，模拟前端消费方式。
	var generic map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &generic))

	data, ok := generic["data"].(map[string]any)
	require.True(t, ok, "data 字段应为 object")

	require.Contains(t, data, "created")
	require.Contains(t, data, "updated")
	require.Contains(t, data, "skipped")
	require.EqualValues(t, 1, data["created"])
	require.EqualValues(t, 0, data["updated"])

	skippedList, ok := data["skipped"].([]any)
	require.True(t, ok, "skipped 应为数组")
	require.Len(t, skippedList, 1)

	skippedItem, ok := skippedList[0].(map[string]any)
	require.True(t, ok, "skipped[0] 应为 object")
	require.Contains(t, skippedItem, "name")
	require.Contains(t, skippedItem, "reason")
	require.Equal(t, "unsupported", skippedItem["name"])
}

func TestImportSubscription_拒绝非http协议URL(t *testing.T) {
	router, _ := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":"file:///etc/passwd"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestImportSubscription_URL为空返回400(t *testing.T) {
	router, _ := setupProxySubscriptionRouter()

	rec := doImportSubscription(t, router, `{"url":""}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
