package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AccountPatrolRecordRepository persists connectivity patrol cycle history.
type AccountPatrolRecordRepository struct {
	db *sql.DB
}

func NewAccountPatrolRecordRepository(db *sql.DB) *AccountPatrolRecordRepository {
	return &AccountPatrolRecordRepository{db: db}
}

func (r *AccountPatrolRecordRepository) Create(ctx context.Context, record *service.AccountPatrolRecord) error {
	if r == nil || r.db == nil || record == nil {
		return fmt.Errorf("account patrol record repository unavailable")
	}
	failedIDs, err := json.Marshal(record.FailedAccountIDs)
	if err != nil {
		failedIDs = []byte("[]")
	}
	const q = `
		INSERT INTO account_patrol_records (
			started_at, finished_at, batch_size, success_count, failed_count,
			cursor_after, interval_minutes, concurrency, failed_account_ids, note
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id`
	return r.db.QueryRowContext(
		ctx, q,
		record.StartedAt.UTC(),
		record.FinishedAt.UTC(),
		record.BatchSize,
		record.SuccessCount,
		record.FailedCount,
		record.CursorAfter,
		record.IntervalMinutes,
		record.Concurrency,
		failedIDs,
		record.Note,
	).Scan(&record.ID)
}

func (r *AccountPatrolRecordRepository) List(ctx context.Context, page, pageSize int) ([]service.AccountPatrolRecord, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, fmt.Errorf("account patrol record repository unavailable")
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_patrol_records`).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, started_at, finished_at, batch_size, success_count, failed_count,
		       cursor_after, interval_minutes, concurrency, failed_account_ids, note
		FROM account_patrol_records
		ORDER BY finished_at DESC, id DESC
		LIMIT $1 OFFSET $2`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.AccountPatrolRecord, 0, pageSize)
	for rows.Next() {
		var item service.AccountPatrolRecord
		var failedRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.StartedAt,
			&item.FinishedAt,
			&item.BatchSize,
			&item.SuccessCount,
			&item.FailedCount,
			&item.CursorAfter,
			&item.IntervalMinutes,
			&item.Concurrency,
			&failedRaw,
			&item.Note,
		); err != nil {
			return nil, 0, err
		}
		if len(failedRaw) > 0 {
			_ = json.Unmarshal(failedRaw, &item.FailedAccountIDs)
		}
		if item.FailedAccountIDs == nil {
			item.FailedAccountIDs = []int64{}
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// DeleteOlderThan removes patrol history older than cutoff (best-effort retention).
func (r *AccountPatrolRecordRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM account_patrol_records WHERE finished_at < $1`, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
