//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type tempEntryCounterStub struct {
	count      int64
	incrCalls  int
	resetCalls int
	incrErr    error
	resetErr   error
}

func (s *tempEntryCounterStub) IncrementTempUnschedEntryCount(ctx context.Context, accountID int64) (int64, error) {
	s.incrCalls++
	if s.incrErr != nil {
		return 0, s.incrErr
	}
	s.count++
	return s.count, nil
}

func (s *tempEntryCounterStub) ResetTempUnschedEntryCount(ctx context.Context, accountID int64) error {
	s.resetCalls++
	if s.resetErr != nil {
		return s.resetErr
	}
	s.count = 0
	return nil
}

func TestAccountRepository_RecordTempUnschedReentry_EscalatesToErrorAtThreshold(t *testing.T) {
	counter := &tempEntryCounterStub{}
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	repo.tempUnschedEntryCounter = counter

	var setErrorCalls int
	var lastErrorMsg string
	repo.setErrorForTest = func(ctx context.Context, id int64, errorMsg string) error {
		setErrorCalls++
		lastErrorMsg = errorMsg
		require.Equal(t, int64(42), id)
		return nil
	}

	// 1st / 2nd re-entry: only increment
	for i := 1; i <= 2; i++ {
		repo.recordTempUnschedReentry(context.Background(), 42)
		require.Equal(t, i, counter.incrCalls)
		require.Equal(t, 0, counter.resetCalls)
		require.Equal(t, 0, setErrorCalls)
	}

	// 3rd re-entry: SetError + ClearTemp + Reset
	repo.recordTempUnschedReentry(context.Background(), 42)
	require.Equal(t, 3, counter.incrCalls)
	require.Equal(t, 1, setErrorCalls)
	require.Equal(t, "temporary unschedulable entered 3 times", lastErrorMsg)
	require.Equal(t, 1, counter.resetCalls)
	// ClearTempUnschedulable 走 sql.Exec
	require.NotEmpty(t, exec.execQueries)
	joined := ""
	for _, q := range exec.execQueries {
		joined += q + "\n"
	}
	require.Contains(t, joined, "temp_unschedulable_until")
}

func TestAccountRepository_RecordTempUnschedReentry_NilCounterIsNoop(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	repo.recordTempUnschedReentry(context.Background(), 42)
	require.Empty(t, exec.execQueries)
}

func TestAccountRepository_RecordTempUnschedReentry_IncrementErrorIsNoop(t *testing.T) {
	counter := &tempEntryCounterStub{incrErr: errors.New("redis down")}
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	repo.tempUnschedEntryCounter = counter

	repo.recordTempUnschedReentry(context.Background(), 42)
	require.Equal(t, 1, counter.incrCalls)
	require.Equal(t, 0, counter.resetCalls)
	require.Empty(t, exec.execQueries)
}

func TestTempUnschedEntryErrorThresholdConstant(t *testing.T) {
	require.Equal(t, 3, service.TempUnschedEntryErrorThreshold)
}
