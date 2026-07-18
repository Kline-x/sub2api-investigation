package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	tempUnschedEntryCounterPrefix     = "temp_unsched_entry:account:"
	tempUnschedEntryCounterTTLSeconds = 86400 // 24 小时兜底
)

// tempUnschedEntryCounterIncrScript 原子 INCR；首写时设置 TTL。
var tempUnschedEntryCounterIncrScript = redis.NewScript(`
	local key = KEYS[1]
	local ttl = tonumber(ARGV[1])

	local count = redis.call('INCR', key)
	if count == 1 then
		redis.call('EXPIRE', key, ttl)
	end

	return count
`)

type tempUnschedEntryCounterCache struct {
	rdb *redis.Client
}

// NewTempUnschedEntryCounterCache 创建临时不可调度 re-entry 计数器。
func NewTempUnschedEntryCounterCache(rdb *redis.Client) service.TempUnschedEntryCounterCache {
	return &tempUnschedEntryCounterCache{rdb: rdb}
}

// IncrementTempUnschedEntryCount 原子递增计数并返回当前值。
func (c *tempUnschedEntryCounterCache) IncrementTempUnschedEntryCount(ctx context.Context, accountID int64) (int64, error) {
	key := fmt.Sprintf("%s%d", tempUnschedEntryCounterPrefix, accountID)
	result, err := tempUnschedEntryCounterIncrScript.Run(ctx, c.rdb, []string{key}, tempUnschedEntryCounterTTLSeconds).Int64()
	if err != nil {
		return 0, fmt.Errorf("increment temp unsched entry count: %w", err)
	}
	return result, nil
}

// ResetTempUnschedEntryCount 清零计数器。
func (c *tempUnschedEntryCounterCache) ResetTempUnschedEntryCount(ctx context.Context, accountID int64) error {
	key := fmt.Sprintf("%s%d", tempUnschedEntryCounterPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}
