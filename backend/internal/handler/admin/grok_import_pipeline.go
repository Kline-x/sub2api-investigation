package admin

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 定制:导入 grok OAuth 账号后的后台流水——刷新令牌 → grok-4.5 测试。
// 取代原 scheduleGrokImportProbe 配额探测(测试成功路径本身会持久化配额快照)。
const (
	grokImportPipelineConcurrency = 3
	grokImportPipelineTimeout     = 90 * time.Second
)

type grokImportPipelineDeps struct {
	adminService          service.AdminService
	grokOAuth             service.GrokOAuthTokenService
	tester                backgroundAccountTester
	tokenCacheInvalidator service.TokenCacheInvalidator
}

type grokImportPipelineTask struct {
	deps      grokImportPipelineDeps
	accountID int64
}

type grokImportPipelineScheduler struct {
	mu          sync.Mutex
	queue       []grokImportPipelineTask
	concurrency int
	workers     int
	timeout     time.Duration
}

var defaultGrokImportPipelineScheduler = &grokImportPipelineScheduler{
	concurrency: grokImportPipelineConcurrency,
	timeout:     grokImportPipelineTimeout,
}

func (s *grokImportPipelineScheduler) schedule(deps grokImportPipelineDeps, account *service.Account) bool {
	if s == nil || account == nil || account.ID <= 0 {
		return false
	}
	if account.Platform != service.PlatformGrok || account.Type != service.AccountTypeOAuth {
		return false
	}
	if deps.adminService == nil || deps.grokOAuth == nil {
		return false
	}

	s.mu.Lock()
	s.queue = append(s.queue, grokImportPipelineTask{deps: deps, accountID: account.ID})
	if s.workers < s.concurrency {
		s.workers++
		go s.worker()
	}
	s.mu.Unlock()
	return true
}

func (s *grokImportPipelineScheduler) worker() {
	for {
		task, ok := s.nextTask()
		if !ok {
			return
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("grok_import_pipeline_panic", "account_id", task.accountID, "recovery_type", panicType(recovered))
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
			defer cancel()
			runGrokImportPipeline(ctx, task.deps, task.accountID)
		}()
	}
}

func (s *grokImportPipelineScheduler) nextTask() (grokImportPipelineTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		s.workers--
		return grokImportPipelineTask{}, false
	}
	task := s.queue[0]
	s.queue[0] = grokImportPipelineTask{}
	s.queue = s.queue[1:]
	if len(s.queue) == 0 {
		s.queue = nil
	}
	return task, true
}

// runGrokImportPipeline 执行单账号流水:刷新令牌 → 成功则空 model 测试(grok 默认 grok-4.5)。
// 刷新永久失败(invalid_grant/上游4xx非429)→ SetAccountError;测试失败的置错在
// service 层 testGrokAccountConnection 内统一生效。
func runGrokImportPipeline(ctx context.Context, deps grokImportPipelineDeps, accountID int64) {
	account, err := deps.adminService.GetAccount(ctx, accountID)
	if err != nil || account == nil {
		slog.Warn("grok_import_pipeline_load_failed", "account_id", accountID, "error", err)
		return
	}

	tokenInfo, err := deps.grokOAuth.RefreshAccountToken(ctx, account)
	if err != nil {
		if service.IsGrokRefreshPermanentFailure(err) {
			if setErr := deps.adminService.SetAccountError(ctx, accountID, "Grok import refresh failed: "+err.Error()); setErr != nil {
				slog.Warn("grok_import_pipeline_set_error_failed", "account_id", accountID, "error", setErr)
			}
		}
		slog.Warn("grok_import_pipeline_refresh_failed", "account_id", accountID, "error", err)
		return
	}

	newCredentials := service.MergeCredentials(account.Credentials, deps.grokOAuth.BuildAccountCredentials(tokenInfo))
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		newCredentials["base_url"] = baseURL
	}
	updated, err := deps.adminService.UpdateAccount(ctx, accountID, &service.UpdateAccountInput{Credentials: newCredentials})
	if err != nil {
		slog.Warn("grok_import_pipeline_update_failed", "account_id", accountID, "error", err)
		return
	}
	if deps.tokenCacheInvalidator != nil {
		if invalidateErr := deps.tokenCacheInvalidator.InvalidateToken(ctx, updated); invalidateErr != nil {
			slog.Warn("grok_import_pipeline_invalidate_failed", "account_id", accountID, "error", invalidateErr)
		}
	}

	if deps.tester == nil {
		return
	}
	result, testErr := deps.tester.RunTestBackground(ctx, accountID, "")
	if testErr != nil {
		slog.Warn("grok_import_pipeline_test_failed", "account_id", accountID, "error", testErr)
		return
	}
	if result != nil {
		slog.Info("grok_import_pipeline_done", "account_id", accountID, "status", result.Status, "latency_ms", result.LatencyMs)
	}
}

func (h *AccountHandler) scheduleGrokImportPipeline(account *service.Account) bool {
	if h == nil {
		return false
	}
	return defaultGrokImportPipelineScheduler.schedule(grokImportPipelineDeps{
		adminService:          h.adminService,
		grokOAuth:             h.grokOAuthService,
		tester:                h.accountTester,
		tokenCacheInvalidator: h.tokenCacheInvalidator,
	}, account)
}