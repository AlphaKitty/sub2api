package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	checkInClaimKeyPrefix = "checkin:claim:"
	// checkInClaimTTL 占用标记的存活时间：签到跨天即失效，48h 冗余覆盖时区偏移
	checkInClaimTTL = 48 * time.Hour
)

// CheckInCache 签到幂等的 Redis 快路径（非权威）。
// 正确性由 check_in_records 的唯一索引兜底；Redis 仅拦截重复点击，
// 避免并发请求全部打到 DB 上做无谓的冲突回滚。
type CheckInCache struct {
	rdb *redis.Client
}

// NewCheckInCache 创建签到 Redis 快路径缓存。
func NewCheckInCache(rdb *redis.Client) service.CheckInDedupCache {
	return &CheckInCache{rdb: rdb}
}

// Claim 尝试占用用户当日签到位；已占用返回 false。
func (c *CheckInCache) Claim(ctx context.Context, userID int64, dayKey string) (bool, error) {
	key := checkInClaimKey(userID, dayKey)
	ok, err := c.rdb.SetNX(ctx, key, "1", checkInClaimTTL).Result()
	if err != nil {
		return false, fmt.Errorf("check-in claim setnx: %w", err)
	}
	return ok, nil
}

// Release 释放用户当日签到位（仅当签到事务失败时调用）。
func (c *CheckInCache) Release(ctx context.Context, userID int64, dayKey string) error {
	if err := c.rdb.Del(ctx, checkInClaimKey(userID, dayKey)).Err(); err != nil {
		return fmt.Errorf("check-in claim release: %w", err)
	}
	return nil
}

func checkInClaimKey(userID int64, dayKey string) string {
	return fmt.Sprintf("%s%d:%s", checkInClaimKeyPrefix, userID, dayKey)
}
