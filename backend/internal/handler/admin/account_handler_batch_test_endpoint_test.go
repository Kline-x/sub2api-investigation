//go:build unit

package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type batchTestTesterStub struct {
	mu       sync.Mutex
	results  map[int64]*service.ScheduledTestResult
	calls    []int64
	modelIDs map[int64]string
}

func (s *batchTestTesterStub) RunTestBackground(_ context.Context, accountID int64, modelID string) (*service.ScheduledTestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, accountID)
	if s.modelIDs == nil {
		s.modelIDs = make(map[int64]string)
	}
	s.modelIDs[accountID] = modelID
	if r, ok := s.results[accountID]; ok {
		return r, nil
	}
	return &service.ScheduledTestResult{Status: "success", LatencyMs: 5}, nil
}

type batchTestAdminService struct {
	*stubAdminService
	accounts map[int64]*service.Account
}

func (s *batchTestAdminService) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	out := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		if acc, ok := s.accounts[id]; ok {
			out = append(out, acc)
		}
	}
	return out, nil
}

func newBatchTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/batch-test", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, rec
}

func TestBatchTestMixedResults(t *testing.T) {
	adminSvc := &batchTestAdminService{
		stubAdminService: newStubAdminService(),
		accounts: map[int64]*service.Account{
			1: {ID: 1, Name: "ok", Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			2: {ID: 2, Name: "bad", Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
		},
	}
	tester := &batchTestTesterStub{results: map[int64]*service.ScheduledTestResult{
		2: {Status: "failed", ErrorMessage: "Grok Responses API returned 403: blocked"},
	}}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.accountTester = tester

	// id=3 不存在 → 计为 failed
	c, rec := newBatchTestContext(t, `{"account_ids":[1,2,3]}`)
	handler.BatchTest(c)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Equal(t, int64(3), gjson.Get(body, "data.total").Int())
	require.Equal(t, int64(1), gjson.Get(body, "data.success").Int())
	require.Equal(t, int64(2), gjson.Get(body, "data.failed").Int())
	require.Equal(t, int64(3), gjson.Get(body, "data.results.#").Int())

	sort.Slice(tester.calls, func(i, j int) bool { return tester.calls[i] < tester.calls[j] })
	require.Equal(t, []int64{1, 2}, tester.calls)
}

func TestBatchTestEmptyIDsRejected(t *testing.T) {
	handler := NewAccountHandler(newStubAdminService(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.accountTester = &batchTestTesterStub{}

	c, rec := newBatchTestContext(t, `{"account_ids":[]}`)
	handler.BatchTest(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBatchTestModelsByPlatform(t *testing.T) {
	adminSvc := &batchTestAdminService{
		stubAdminService: newStubAdminService(),
		accounts: map[int64]*service.Account{
			10: {ID: 10, Name: "g1", Platform: service.PlatformGrok, Type: service.AccountTypeOAuth},
			11: {ID: 11, Name: "o1", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth},
		},
	}
	tester := &batchTestTesterStub{}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.accountTester = tester

	c, rec := newBatchTestContext(t, `{"account_ids":[10,11],"models_by_platform":{"grok":"grok-4.5","openai":"gpt-5.4"}}`)
	handler.BatchTest(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "grok-4.5", tester.modelIDs[10])
	require.Equal(t, "gpt-5.4", tester.modelIDs[11])
}