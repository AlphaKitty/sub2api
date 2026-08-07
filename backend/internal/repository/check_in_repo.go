package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/checkinrecord"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// checkInRepository 签到记录的 ent 实现。
// 硬删除实体：不使用软删除 Mixin，记录一经创建不可变（审计型流水）。
type checkInRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

// NewCheckInRepository 创建签到记录仓储。
// *sql.DB 直接满足 sqlExecutor 接口（ExecContext/QueryContext）。
func NewCheckInRepository(client *dbent.Client, sqlDB *sql.DB) service.CheckInRecordRepository {
	return &checkInRepository{client: client, sql: sqlDB}
}

// GetByDate 查询用户某天的签到记录；不存在返回 ErrCheckInNotFound。
func (r *checkInRepository) GetByDate(ctx context.Context, userID int64, date time.Time) (*service.CheckInRecord, error) {
	rec, err := clientFromContext(ctx, r.client).CheckInRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckInDateEQ(date),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrCheckInNotFound
		}
		return nil, fmt.Errorf("get check-in record: %w", err)
	}
	return checkInEntityToService(rec), nil
}

// ListRecent 返回用户最近 limit 条签到记录（按签到日期倒序）。
func (r *checkInRepository) ListRecent(ctx context.Context, userID int64, limit int) ([]service.CheckInRecord, error) {
	recs, err := clientFromContext(ctx, r.client).CheckInRecord.Query().
		Where(checkinrecord.UserIDEQ(userID)).
		Order(dbent.Desc(checkinrecord.FieldCheckInDate)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recent check-in records: %w", err)
	}
	out := make([]service.CheckInRecord, 0, len(recs))
	for i := range recs {
		out = append(out, *checkInEntityToService(recs[i]))
	}
	return out, nil
}

// RecordCheckIn 在单个事务内：插入签到记录 + 用户余额原子累加 + 支付审计流水。
func (r *checkInRepository) RecordCheckIn(ctx context.Context, userID int64, date time.Time, streak int, reward float64) (float64, error) {
	if userID <= 0 {
		return 0, service.ErrUserNotFound
	}
	var balanceAfter float64
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		// 1. 插入签到记录（唯一索引 (user_id, check_in_date) 兜底并发幂等）
		if _, err := txClient.CheckInRecord.Create().
			SetUserID(userID).
			SetCheckInDate(date).
			SetStreakDays(streak).
			SetRewardAmount(reward).
			Save(txCtx); err != nil {
			if dbent.IsConstraintError(err) {
				return service.ErrCheckInAlreadyCheckedIn.WithCause(err)
			}
			return fmt.Errorf("insert check-in record: %w", err)
		}

		// 2. 余额原子累加并返回新值（原生 SQL 避免"读-改-写"竞态）
		rows, err := txClient.QueryContext(txCtx,
			"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL RETURNING balance",
			reward, userID,
		)
		if err != nil {
			return fmt.Errorf("add check-in balance: %w", err)
		}
		if !rows.Next() {
			rowsErr := rows.Err()
			_ = rows.Close()
			if rowsErr != nil {
				return rowsErr
			}
			return service.ErrUserNotFound
		}
		if err := rows.Scan(&balanceAfter); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan check-in balance: %w", err)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		// 必须立即关闭 rows 释放连接，后续才能在同一事务连接上执行 ent 写操作
		if err := rows.Close(); err != nil {
			return err
		}

		// 3. 支付审计流水（order_id 必填，用 checkin:{userID}:{date} 填充）
		detail := fmt.Sprintf(`{"user_id":%d,"check_in_date":%q,"streak_days":%d,"reward_amount":%s}`,
			userID, date.Format("2006-01-02"), streak, formatFloat(reward))
		if _, err := txClient.PaymentAuditLog.Create().
			SetOrderID(fmt.Sprintf("checkin:%d:%s", userID, date.Format("2006-01-02"))).
			SetAction("checkin_reward").
			SetDetail(detail).
			SetOperator("system").
			Save(txCtx); err != nil {
			return fmt.Errorf("write check-in audit log: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return balanceAfter, nil
}

// SumRewardsSince 统计用户自 since（含）以来的累计签到奖励金额。
func (r *checkInRepository) SumRewardsSince(ctx context.Context, userID int64, since time.Time) (float64, error) {
	client := clientFromContext(ctx, r.client)
	var result []struct {
		Sum float64 `json:"sum"`
	}
	err := client.CheckInRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckInDateGTE(since),
		).
		Aggregate(dbent.As(dbent.Sum(checkinrecord.FieldRewardAmount), "sum")).
		Scan(ctx, &result)
	if err != nil {
		return 0, fmt.Errorf("sum check-in rewards: %w", err)
	}
	if len(result) == 0 {
		return 0, nil
	}
	return result[0].Sum, nil
}

// withTx 在事务中执行 fn；已处于外部事务时复用当前事务 client。
func (r *checkInRepository) withTx(ctx context.Context, fn func(txCtx context.Context, txClient *dbent.Client) error) error {
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return fmt.Errorf("begin check-in transaction: %w", err)
	}

	var txClient *dbent.Client
	txCtx := ctx
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
		txCtx = dbent.NewTxContext(ctx, tx)
	} else {
		// 已处于外部事务中（ErrTxStarted）：复用当前事务 client，由调用方负责提交/回滚。
		if existingTx := dbent.TxFromContext(ctx); existingTx != nil {
			txClient = existingTx.Client()
		} else {
			txClient = r.client
		}
	}

	if err := fn(txCtx, txClient); err != nil {
		return err
	}
	if err == nil {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit check-in transaction: %w", err)
		}
	}
	return nil
}

// checkInEntityToService 将 ent 实体转为 service 层结构。
// 查询/写入统一使用应用全局时区（timezone 包设置 time.Local）。
func checkInEntityToService(rec *dbent.CheckInRecord) *service.CheckInRecord {
	return &service.CheckInRecord{
		ID:           rec.ID,
		UserID:       rec.UserID,
		CheckInDate:  rec.CheckInDate.In(time.Local),
		StreakDays:   rec.StreakDays,
		RewardAmount: rec.RewardAmount,
		CreatedAt:    rec.CreatedAt,
	}
}

// formatFloat 格式化 decimal 金额（保留 8 位精度）。
func formatFloat(v float64) string {
	return fmt.Sprintf("%.8f", v)
}
