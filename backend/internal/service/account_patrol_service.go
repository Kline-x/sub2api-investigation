package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	accountPatrolDefaultIntervalMinutes = 30
	accountPatrolMinIntervalMinutes     = 5
	accountPatrolMaxIntervalMinutes     = 24 * 60
	accountPatrolDefaultBatchSize       = 20
	accountPatrolMinBatchSize           = 1
	accountPatrolMaxBatchSize           = 100
	accountPatrolDefaultConcurrency     = 4
	accountPatrolMinConcurrency         = 1
	accountPatrolMaxConcurrency         = 20
	accountPatrolCycleInterval          = time.Minute
	accountPatrolRunTimeout             = 8 * time.Minute
	accountPatrolLeaderLockKey          = "account:patrol:leader"
	accountPatrolLeaderLockTTL          = 2 * time.Minute
)

// AccountPatrolSettings controls the global account connectivity patrol runner.
type AccountPatrolSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"`
	BatchSize       int  `json:"batch_size"`
	Concurrency     int  `json:"concurrency"`
}

func defaultAccountPatrolSettings() *AccountPatrolSettings {
	return &AccountPatrolSettings{
		Enabled:         false,
		IntervalMinutes: accountPatrolDefaultIntervalMinutes,
		BatchSize:       accountPatrolDefaultBatchSize,
		Concurrency:     accountPatrolDefaultConcurrency,
	}
}

func normalizeAccountPatrolSettings(in *AccountPatrolSettings) *AccountPatrolSettings {
	out := defaultAccountPatrolSettings()
	if in == nil {
		return out
	}
	out.Enabled = in.Enabled
	if in.IntervalMinutes > 0 {
		out.IntervalMinutes = in.IntervalMinutes
	}
	if out.IntervalMinutes < accountPatrolMinIntervalMinutes {
		out.IntervalMinutes = accountPatrolMinIntervalMinutes
	}
	if out.IntervalMinutes > accountPatrolMaxIntervalMinutes {
		out.IntervalMinutes = accountPatrolMaxIntervalMinutes
	}
	if in.BatchSize > 0 {
		out.BatchSize = in.BatchSize
	}
	if out.BatchSize < accountPatrolMinBatchSize {
		out.BatchSize = accountPatrolMinBatchSize
	}
	if out.BatchSize > accountPatrolMaxBatchSize {
		out.BatchSize = accountPatrolMaxBatchSize
	}
	if in.Concurrency > 0 {
		out.Concurrency = in.Concurrency
	}
	if out.Concurrency < accountPatrolMinConcurrency {
		out.Concurrency = accountPatrolMinConcurrency
	}
	if out.Concurrency > accountPatrolMaxConcurrency {
		out.Concurrency = accountPatrolMaxConcurrency
	}
	return out
}

// GetAccountPatrolSettings returns defaults when the setting is absent.
func (s *SettingService) GetAccountPatrolSettings(ctx context.Context) (*AccountPatrolSettings, error) {
	defaults := defaultAccountPatrolSettings()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAccountPatrolSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return nil, fmt.Errorf("get account patrol settings: %w", err)
	}
	if value == "" {
		return defaults, nil
	}
	var settings AccountPatrolSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return defaults, nil
	}
	return normalizeAccountPatrolSettings(&settings), nil
}

// SetAccountPatrolSettings persists normalized patrol settings.
func (s *SettingService) SetAccountPatrolSettings(ctx context.Context, settings *AccountPatrolSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	normalized := normalizeAccountPatrolSettings(settings)
	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal account patrol settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyAccountPatrolSettings, string(data))
}

type accountPatrolIDLister interface {
	ListPatrolAccountIDs(ctx context.Context, afterID int64, limit int) ([]int64, error)
}

// AccountPatrolService periodically batch-tests accounts when enabled.
// Failures are marked error by AccountTestService; successes recover runtime state.
type AccountPatrolService struct {
	accountRepo        AccountRepository
	accountTestService *AccountTestService
	rateLimitService   *RateLimitService
	settingService     *SettingService
	lockCache          LeaderLockCache
	db                 *sql.DB

	parentCtx    context.Context
	parentCancel context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	started      bool
	stopped      bool
	cursor       int64
	lastCycleAt  time.Time
	instanceID   string
	now          func() time.Time
}

func NewAccountPatrolService(
	accountRepo AccountRepository,
	accountTestService *AccountTestService,
	rateLimitService *RateLimitService,
	settingService *SettingService,
) *AccountPatrolService {
	ctx, cancel := context.WithCancel(context.Background())
	return &AccountPatrolService{
		accountRepo:        accountRepo,
		accountTestService: accountTestService,
		rateLimitService:   rateLimitService,
		settingService:     settingService,
		parentCtx:          ctx,
		parentCancel:       cancel,
		now:                time.Now,
		instanceID:         uuid.NewString(),
	}
}

func (s *AccountPatrolService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

// ProvideAccountPatrolService starts the process-wide patrol runner.
func ProvideAccountPatrolService(
	accountRepo AccountRepository,
	accountTestService *AccountTestService,
	rateLimitService *RateLimitService,
	settingService *SettingService,
	lockCache LeaderLockCache,
	db *sql.DB,
) *AccountPatrolService {
	svc := NewAccountPatrolService(accountRepo, accountTestService, rateLimitService, settingService)
	svc.SetLeaderLock(lockCache, db)
	svc.Start()
	return svc
}

func (s *AccountPatrolService) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()
	go s.runLoop()
}

func (s *AccountPatrolService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.parentCancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *AccountPatrolService) GetSettings(ctx context.Context) (*AccountPatrolSettings, error) {
	if s == nil || s.settingService == nil {
		return defaultAccountPatrolSettings(), nil
	}
	return s.settingService.GetAccountPatrolSettings(ctx)
}

func (s *AccountPatrolService) UpdateSettings(ctx context.Context, settings *AccountPatrolSettings) error {
	if s == nil || s.settingService == nil {
		return fmt.Errorf("account patrol service unavailable")
	}
	return s.settingService.SetAccountPatrolSettings(ctx, settings)
}

func (s *AccountPatrolService) runLoop() {
	defer s.wg.Done()
	_ = s.RunDue(s.parentCtx)
	ticker := time.NewTicker(accountPatrolCycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.parentCtx.Done():
			return
		case <-ticker.C:
			_ = s.RunDue(s.parentCtx)
		}
	}
}

// RunDue executes one patrol cycle when enabled and due.
func (s *AccountPatrolService) RunDue(ctx context.Context) error {
	if s == nil || s.accountTestService == nil || s.accountRepo == nil || s.settingService == nil {
		return nil
	}
	settings, err := s.GetSettings(ctx)
	if err != nil || settings == nil || !settings.Enabled {
		return err
	}

	now := s.now()
	s.mu.Lock()
	if !s.lastCycleAt.IsZero() && now.Sub(s.lastCycleAt) < time.Duration(settings.IntervalMinutes)*time.Minute {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	release, acquired := s.tryAcquireLeader(ctx)
	if !acquired {
		return nil
	}
	if release != nil {
		defer release()
	}

	runCtx, cancel := context.WithTimeout(ctx, accountPatrolRunTimeout)
	defer cancel()

	ids, err := s.nextBatch(runCtx, settings.BatchSize)
	if err != nil {
		slog.Warn("account_patrol_list_failed", "error", err)
		return err
	}
	if len(ids) == 0 {
		s.mu.Lock()
		s.lastCycleAt = now
		s.mu.Unlock()
		return nil
	}

	success, failed := s.testBatch(runCtx, ids, settings.Concurrency)
	s.mu.Lock()
	if len(ids) > 0 {
		s.cursor = ids[len(ids)-1]
	}
	s.lastCycleAt = now
	s.mu.Unlock()

	slog.Info("account_patrol_cycle",
		"batch", len(ids),
		"success", success,
		"failed", failed,
		"cursor", s.cursor,
	)
	return nil
}

func (s *AccountPatrolService) tryAcquireLeader(ctx context.Context) (release func(), ok bool) {
	if s == nil {
		return nil, true
	}
	if s.lockCache == nil && s.db == nil {
		return nil, true
	}
	return tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, accountPatrolLeaderLockKey, s.instanceID, accountPatrolLeaderLockTTL)
}

func (s *AccountPatrolService) nextBatch(ctx context.Context, batchSize int) ([]int64, error) {
	lister, ok := s.accountRepo.(accountPatrolIDLister)
	if !ok {
		return nil, fmt.Errorf("account repository does not support patrol listing")
	}
	s.mu.Lock()
	afterID := s.cursor
	s.mu.Unlock()

	ids, err := lister.ListPatrolAccountIDs(ctx, afterID, batchSize)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		return ids, nil
	}
	// wrap around
	if afterID == 0 {
		return nil, nil
	}
	return lister.ListPatrolAccountIDs(ctx, 0, batchSize)
}

func (s *AccountPatrolService) testBatch(ctx context.Context, ids []int64, concurrency int) (success, failed int) {
	if concurrency < 1 {
		concurrency = accountPatrolDefaultConcurrency
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	var mu sync.Mutex
	for _, id := range ids {
		accountID := id
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			result, testErr := s.accountTestService.RunTestBackground(gctx, accountID, "")
			ok := testErr == nil && result != nil && result.Status == "success"
			if ok && s.rateLimitService != nil {
				if _, recoverErr := s.rateLimitService.RecoverAccountAfterSuccessfulTest(gctx, accountID); recoverErr != nil {
					slog.Warn("account_patrol_recover_failed", "account_id", accountID, "error", recoverErr)
				}
			}
			mu.Lock()
			if ok {
				success++
			} else {
				failed++
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return success, failed
}
