package service

import "context"

// TempUnschedEntryErrorThreshold 账号真正重新进入临时不可调度的累计次数阈值。
// 达到后自动 SetError 永久停用(定制)。
const TempUnschedEntryErrorThreshold = 3

// TempUnschedEntryCounterCache 追踪账号「重新进入」临时不可调度的次数。
// 仅在 temp 从空/已过期 → 生效 时递增；窗口延长不计。
type TempUnschedEntryCounterCache interface {
	// IncrementTempUnschedEntryCount 原子递增并返回当前值。
	IncrementTempUnschedEntryCount(ctx context.Context, accountID int64) (int64, error)
	// ResetTempUnschedEntryCount 清零(测试成功/恢复正常/已置错后调用)。
	ResetTempUnschedEntryCount(ctx context.Context, accountID int64) error
}
